// build.go (v25.6): StartInDir(goal, outDir) lets the UI pick the output folder
// (default = root); run() writes a design manifest (manifest.go) into
// <outDir>/design-manifest/ right after planning and generates into <outDir>.
// Prior:
// build.go (v25.5): Start() now also wires the structured self-healing observer
// (agent.HealObserver → "heal" SSE) and a broker AgentSink (→ "agent_structured"
// SSE) so the Studio can render repair attempts live in a dedicated panel.
// Prior:
// build.go (v23.10 fix): 0-entity plans now fail with an actionable message
// (Ollama down) instead of silently "completing" empty; build_complete now
// carries output_path + entity count for the UI.
// build.go (v20.1): + BuildService.Clean (clean start: wipe .gen + generated dirs).
// Patched in v18 (B2): the build pipeline no longer references the github
// client. Opening a PR is now strictly an out-of-engine action (gen-ferry).
// The PROnSuccess gate and CreateBuildPR call site are removed.
//
// Patched in v18.1: convert compiler.SchemaIR → []schema.Entity in the
// evolve flow. The previous code assigned ir.Entities (of type
// []compiler.EntityIR) directly to planner.Plan.Entities (of type
// []schema.Entity), which never compiled. The planner type asserts
// schema shape, not the relational IR shape.
package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/agent"
	"github.com/hpdkhoa/gen-system/generator/internal/api/sse"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	planner_v2 "github.com/hpdkhoa/gen-system/generator/internal/planner_v2"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

type JobStatus string

const (
	JobPending JobStatus = "pending"
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
)

type BuildJob struct {
	ID          string    `json:"id"`
	Goal        string    `json:"goal"`
	Status      JobStatus `json:"status"`
	Error       string    `json:"error,omitempty"`
	StartedAt   int64     `json:"started_at"`
	DoneAt      int64     `json:"done_at,omitempty"`
	OutputPath  string    `json:"output_path,omitempty"`  // v23.10: where the generated app landed
	ManifestDir string    `json:"manifest_dir,omitempty"` // v25.6: design-manifest/ folder
	Entities    int       `json:"entities,omitempty"`     // v23.10: entity count, for the UI summary
}

type BuildService struct {
	broker       *sse.Broker
	snapSvc      *SnapshotService
	root         string
	projectsBase string // v25.6: output-folder selection boundary
	mu           sync.Mutex
	job          *BuildJob
	pending      *PendingContinue // v25.8: continuation awaiting Approve/Reject
}

func NewBuildService(broker *sse.Broker, snapSvc *SnapshotService, root string) *BuildService {
	return &BuildService{broker: broker, snapSvc: snapSvc, root: root}
}

// SetProjectsBase records the security boundary for output-folder selection
// (v25.6). The generate UI may only target directories inside this base, and
// never the server root itself (a build overwrites its target). Empty base
// disables the picker (only the default root is usable).
func (s *BuildService) SetProjectsBase(base string) {
	if abs, err := filepath.Abs(base); err == nil {
		s.projectsBase = abs
	} else {
		s.projectsBase = base
	}
}

// ProjectsBase returns the configured output boundary (absolute), or "".
func (s *BuildService) ProjectsBase() string { return s.projectsBase }

// Root returns the server root (absolute where possible).
func (s *BuildService) Root() string {
	if abs, err := filepath.Abs(s.root); err == nil {
		return abs
	}
	return s.root
}

// ValidateOutputDir resolves req to an absolute, symlink-cleaned path and
// confirms it is inside projectsBase and is not the server root. The directory
// must already exist (create it first via the mkdir endpoint). Mirrors
// ProjectManager.validateRoot, with the extra root-exclusion rule because a
// build overwrites its target. Returns the safe absolute path or an error.
func (s *BuildService) ValidateOutputDir(req string) (string, error) {
	if strings.TrimSpace(req) == "" {
		return "", fmt.Errorf("output directory is required")
	}
	if s.projectsBase == "" {
		return "", fmt.Errorf("output-folder selection is disabled (no --projects-base configured)")
	}
	abs, err := filepath.Abs(req)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("path does not exist or is unreadable: %v", err)
	}
	base := s.projectsBase
	if b, err := filepath.EvalSymlinks(base); err == nil {
		base = b
	}
	rel, err := filepath.Rel(base, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output directory must be inside the projects base (%s)", s.projectsBase)
	}
	fi, err := os.Stat(resolved)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("output directory must be an existing directory")
	}
	// Reject the server root. A build overwrites its target and must never
	// clobber the currently-served project.
	root := s.root
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	} else if a, err := filepath.Abs(root); err == nil {
		root = a
	}
	if resolved == root {
		return "", fmt.Errorf("cannot generate into the current root (%s): a build overwrites its target; pick or create another folder", s.root)
	}
	return resolved, nil
}

// CleanResult reports what a clean-start removed.
type CleanResult struct {
	Removed []string `json:"removed"`
	Kept    []string `json:"kept"`
}

// Clean resets the project to a pre-build state. v20.1: removes engine
// state (.gen) and the generated layer directories (backend, frontend,
// mobile, db) under root. Refuses while a build/evolve is running.
//
// It deliberately does not touch source the user brought themselves
// (anything outside those generated dirs), and reports exactly what it
// removed vs. what was absent so the UI can show a clear summary.
func (s *BuildService) Clean() (CleanResult, error) {
	s.mu.Lock()
	running := s.job != nil && s.job.Status == JobRunning
	s.mu.Unlock()
	if running {
		return CleanResult{}, fmt.Errorf("a build is in progress; wait for it to finish")
	}

	targets := []string{".gen", "backend", "frontend", "mobile", "db"}
	var res CleanResult
	for _, t := range targets {
		p := filepath.Join(s.root, t)
		if _, err := os.Stat(p); err != nil {
			res.Kept = append(res.Kept, t+" (absent)")
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			return res, fmt.Errorf("removing %s: %w", t, err)
		}
		res.Removed = append(res.Removed, t)
	}
	s.mu.Lock()
	s.job = nil
	s.mu.Unlock()
	if s.broker != nil {
		s.broker.Publish("agent_event", map[string]string{
			"phase": "clean", "dot": "amber",
			"text": "Clean start: removed " + strings.Join(res.Removed, ", "),
		})
	}
	return res, nil
}

func (s *BuildService) Current() *BuildJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job == nil {
		return nil
	}
	c := *s.job
	return &c
}

func (s *BuildService) Start(goal string) (*BuildJob, error) {
	return s.StartInDir(goal, "")
}

// StartInDir is Start with an explicit output directory. outDir must already be
// validated by the caller (handler) to live inside the projects base and not be
// the server root. Empty outDir falls back to s.root (backward-compatible).
func (s *BuildService) StartInDir(goal, outDir string) (*BuildJob, error) {
	s.mu.Lock()
	if s.job != nil && s.job.Status == JobRunning {
		s.mu.Unlock()
		return nil, fmt.Errorf("build already running")
	}
	target := outDir
	if strings.TrimSpace(target) == "" {
		target = s.root
	}
	job := &BuildJob{
		ID:        fmt.Sprintf("build-%d", time.Now().UnixMilli()),
		Goal:      goal,
		Status:    JobRunning,
		StartedAt: time.Now().UnixMilli(),
	}
	s.job = job
	s.mu.Unlock()

	// Wire SSE into agent event hook before starting goroutine
	agent.EventHook = func(phase, msg string) {
		s.broker.Publish("agent_event", map[string]string{
			"phase": phase, "dot": dotFor(phase), "text": msg,
		})
	}

	// v25.5: also wire the structured self-healing observer and a broker sink so
	// the Studio can render repair attempts in a dedicated panel (in addition to the
	// free-text agent_event log). HealObserver carries the per-attempt fields;
	// the FuncSink republishes every structured AgentEvent (lifecycle + heal) as
	// an "agent_structured" SSE event the Studio's panel consumes. Both are
	// package-level hooks. understand-server never links agent, so the archtest
	// boundary is unaffected.
	agent.HealObserver = func(ev agent.HealEvent) {
		s.broker.Publish("heal", map[string]interface{}{
			"task_id": ev.TaskID, "func": ev.Func,
			"attempt": ev.Attempt, "max": ev.Max,
			"phase": ev.Phase, "strategy": ev.Strategy, "detail": ev.Detail,
		})
	}
	agent.ResetAgentSinks()
	agent.AddAgentSink(agent.FuncSink{Fn: func(ev agent.AgentEvent) {
		s.broker.Publish("agent_structured", map[string]interface{}{
			"kind": string(ev.Kind), "ts": ev.TS, "phase": ev.Phase,
			"message": ev.Message, "task_id": ev.TaskID, "func": ev.Func,
			"attempt": ev.Attempt, "max": ev.Max, "strategy": ev.Strategy,
		})
	}})

	go s.run(job, goal, target)
	return job, nil
}

func (s *BuildService) run(job *BuildJob, goal, target string) {
	s.broker.Publish("build_started", job)

	// Auto-snapshot before build
	if s.snapSvc != nil {
		_, _ = s.snapSvc.Create("Pre-build auto-snap", "auto-saved before build", "agent", "default", true)
	}

	emit := func(dot, text string) {
		s.broker.Publish("agent_event", map[string]string{"phase": dot, "dot": dot, "text": text})
	}

	emit("cyan", fmt.Sprintf("Planner V2 starting, goal: \"%s\"", goal))

	// v23.10 fix: stream planner step progress to the UI. The planner makes
	// several model calls (the slowest part of a build); without this the UI got
	// no events during planning and looked stuck. Note model cold-load: the first
	// call after `ollama serve` loads the model into RAM (an 11GB model can take
	// 30-90s), so step 1 may pause noticeably the first time.
	emit("amber", "Calling the local model. If this is the first build after starting Ollama, it loads the model into memory first (can take 30-90s for a large model).")
	planner_v2.ProgressHook = func(dot, msg string) { emit(dot, msg) }
	defer func() { planner_v2.ProgressHook = nil }()

	plan, err := planner_v2.BuildPlanV2(goal)
	if err != nil {
		s.fail(job, "planning failed: "+err.Error())
		return
	}

	emit("green", fmt.Sprintf("Plan ready: %d entities, %d tasks", len(plan.Entities), len(plan.Tasks)))

	// v23.10 fix: an empty-entity plan means the planner could not turn the goal
	// into a data model. Almost always the cause is that the local model (Ollama) is not
	// running or returned nothing usable. Generating from it "succeeds" but
	// produces no usable app, which looks like the UI silently stalling. Fail
	// loudly with an actionable message instead.
	if len(plan.Entities) == 0 {
		s.fail(job, "the planner produced 0 entities. The local model (Ollama) is likely not running or returned no usable schema. "+ // v23.35: hint the qwen planner model
			"Start it (`ollama serve` + `ollama pull qwen3:14b`) and set GEN_LOGIC=ollama, or run `gen apply schema.json` with an explicit schema.")
		return
	}

	// v25.6: write the design manifest (the whole pre-code design: idea,
	// features, schema, task graph) into <target>/design-manifest/ before
	// generation runs, so it survives even if generation later fails. Side
	// artifact: non-fatal, the build proceeds by default.
	if mp, err := WriteDesignManifest(target, goal, plan); err != nil {
		emit("amber", "design manifest could not be written: "+err.Error())
	} else {
		emit("green", "Design manifest written: "+mp.HTML)
		s.broker.Publish("design_manifest", mp)
	}

	emit("cyan", "agent.RunFromPlan() → executing task graph")

	if err := agent.RunFromPlan(plan, target); err != nil {
		s.fail(job, err.Error())
		return
	}

	// Resolve where the generated app actually landed so the UI can point the
	// user at it (RunFromPlan writes under target; the backend lands in ./backend
	// or ./output/backend depending on the plan path).
	outPath := target
	if _, err := os.Stat(filepath.Join(target, "output", "backend")); err == nil {
		outPath = filepath.Join(target, "output")
	}
	absOut, _ := filepath.Abs(outPath)

	s.mu.Lock()
	job.Status = JobDone
	job.DoneAt = time.Now().UnixMilli()
	job.OutputPath = absOut
	job.ManifestDir = filepath.Join(target, "design-manifest")
	job.Entities = len(plan.Entities)
	s.mu.Unlock()

	s.broker.Publish("build_complete", job)
	emit("green", fmt.Sprintf("Build complete: %d entities generated from \"%s\"", len(plan.Entities), goal))
	emit("cyan", "Output written to: "+absOut)

	// Note: opening a GitHub PR is no longer part of the build pipeline.
	// To push a build to GitHub, run gen-ferry against the working tree.

	// Snapshot after successful build
	if s.snapSvc != nil {
		_, _ = s.snapSvc.Create(
			fmt.Sprintf("Build: %s", goal),
			fmt.Sprintf("Generated from goal: \"%s\"", goal),
			"agent", "default", false,
		)
	}
}

// ─────────────────────────────────────────────
// Evolve (Phase 2.3): marker-based frontend evolution
//
// Loads current IR from .gen/ir.json, builds evolve_frontend tasks for
// all known entities, and runs them through the agent loop.
// EvolveForm/EvolveList use gen-system markers to preserve custom code
// outside the generated block, so they are safe to run on an existing codebase.
//
// change is the natural-language description (e.g. "add search bar to
// user list"). job.Goal logs it for traceability.
// ─────────────────────────────────────────────
func (s *BuildService) Evolve(change string) (*BuildJob, error) {
	s.mu.Lock()
	if s.job != nil && s.job.Status == JobRunning {
		s.mu.Unlock()
		return nil, fmt.Errorf("build already running")
	}
	goal := fmt.Sprintf("evolve: %s", change)
	job := &BuildJob{
		ID:        fmt.Sprintf("evolve-%d", time.Now().UnixMilli()),
		Goal:      goal,
		Status:    JobRunning,
		StartedAt: time.Now().UnixMilli(),
	}
	s.job = job
	s.mu.Unlock()

	agent.EventHook = func(phase, msg string) {
		s.broker.Publish("agent_event", map[string]string{
			"phase": phase, "dot": dotFor(phase), "text": msg,
		})
	}

	go s.runEvolve(job, change)
	return job, nil
}

func (s *BuildService) runEvolve(job *BuildJob, change string) {
	s.broker.Publish("build_started", job)

	emit := func(dot, text string) {
		s.broker.Publish("agent_event", map[string]string{"phase": dot, "dot": dot, "text": text})
	}

	emit("purple", fmt.Sprintf("Evolution engine, change: \"%s\"", change))

	// Load current IR to discover all known entities
	ir, err := compiler.LoadPreviousIR()
	if err != nil || len(ir.Entities) == 0 {
		s.fail(job, "no IR found; run a full build before evolving")
		return
	}

	emit("purple", fmt.Sprintf("Loaded IR: %d entities", len(ir.Entities)))

	// Build evolve_frontend tasks for every known entity.
	// EvolveForm/EvolveList are marker-safe. If markers don't exist yet
	// they fall back to full overwrite (same as GenerateForm/GenerateList).
	var tasks []planner.Task
	for _, e := range ir.Entities {
		taskID := fmt.Sprintf("evolve_frontend_%s", e.Name)
		tasks = append(tasks, planner.Task{
			ID:         taskID,
			Type:       "evolve_frontend",
			EntityName: e.Name,
			Status:     planner.StatusPending,
			Payload:    map[string]interface{}{"entity": e.Name, "change": change},
		})
	}

	if len(tasks) == 0 {
		s.fail(job, "no entities found in IR")
		return
	}

	plan := planner.Plan{
		Goal:     fmt.Sprintf("evolve: %s", change),
		Entities: irToSchemaEntities(ir), // v18.1: was `ir.Entities`, a type mismatch
		Tasks:    tasks,
	}

	emit("purple", fmt.Sprintf("Running %d evolve_frontend tasks", len(tasks)))

	if err := agent.RunFromPlan(plan, s.root); err != nil {
		s.fail(job, "evolution failed: "+err.Error())
		return
	}

	s.mu.Lock()
	job.Status = JobDone
	job.DoneAt = time.Now().UnixMilli()
	s.mu.Unlock()

	s.broker.Publish("build_complete", job)
	emit("purple", fmt.Sprintf("Evolution complete: %d components updated", len(tasks)))

	// Snapshot after successful evolution
	if s.snapSvc != nil {
		desc := fmt.Sprintf("Evolved frontend: \"%s\"", change)
		if len(desc) > 80 {
			desc = desc[:80] + "…"
		}
		_, _ = s.snapSvc.Create(
			fmt.Sprintf("Evolve: %s", strings.TrimSpace(change)),
			desc, "agent", "default", false,
		)
	}
}

// wireAgentHooks wires the agent's SSE hooks (free-text + structured heal) to the
// broker. v25.8: extracted so the continue flow publishes the same live events as
// build/evolve.
func (s *BuildService) wireAgentHooks() {
	agent.EventHook = func(phase, msg string) {
		s.broker.Publish("agent_event", map[string]string{
			"phase": phase, "dot": dotFor(phase), "text": msg,
		})
	}
	agent.HealObserver = func(ev agent.HealEvent) {
		s.broker.Publish("heal", map[string]interface{}{
			"task_id": ev.TaskID, "func": ev.Func,
			"attempt": ev.Attempt, "max": ev.Max,
			"phase": ev.Phase, "strategy": ev.Strategy, "detail": ev.Detail,
		})
	}
	agent.ResetAgentSinks()
	agent.AddAgentSink(agent.FuncSink{Fn: func(ev agent.AgentEvent) {
		s.broker.Publish("agent_structured", map[string]interface{}{
			"kind": string(ev.Kind), "ts": ev.TS, "phase": ev.Phase,
			"message": ev.Message, "task_id": ev.TaskID, "func": ev.Func,
			"attempt": ev.Attempt, "max": ev.Max, "strategy": ev.Strategy,
		})
	}})
}

func (s *BuildService) fail(job *BuildJob, msg string) {
	s.mu.Lock()
	job.Status = JobFailed
	job.Error = msg
	job.DoneAt = time.Now().UnixMilli()
	s.mu.Unlock()
	s.broker.Publish("build_failed", job)
	s.broker.Publish("agent_event", map[string]string{
		"phase": "error", "dot": "red", "text": "Failed: " + msg,
	})
}

func dotFor(phase string) string {
	switch phase {
	case "heal":
		return "amber"
	case "error", "fail":
		return "red"
	case "done":
		return "green"
	case "evolve":
		return "purple"
	default:
		return "cyan"
	}
}

// irToSchemaEntities converts a compiler.SchemaIR (the relational IR
// produced by LoadPreviousIR) into the []schema.Entity shape that
// planner.Plan.Entities expects. compiler.SchemaIR holds Entities and
// Relations as parallel slices; schema.Entity nests Relations per-entity,
// so we group SchemaIR.Relations by FromEntity and translate the
// SchemaIR relation type ("1:1" / "1:N" / "N:M") into the schema string
// form ("belongsTo" / "hasMany") that downstream code reads.
//
// Added in v18.1 to fix the type mismatch that prevented gen-server
// from building.
func irToSchemaEntities(ir compiler.SchemaIR) []schema.Entity {
	// Group relations by their FromEntity for O(1) lookup.
	byFrom := map[string][]compiler.RelationIR{}
	for _, r := range ir.Relations {
		byFrom[r.FromEntity] = append(byFrom[r.FromEntity], r)
	}

	out := make([]schema.Entity, 0, len(ir.Entities))
	for _, e := range ir.Entities {
		fields := make([]schema.Field, 0, len(e.Fields))
		for _, f := range e.Fields {
			fields = append(fields, schema.Field{
				Name: f.Name,
				Type: f.Type,
			})
		}

		var rels []schema.Relation
		for _, r := range byFrom[e.Name] {
			rels = append(rels, schema.Relation{
				Type:       relTypeIRToSchema(r.Type),
				Target:     r.ToEntity,
				ForeignKey: r.FromField,
			})
		}

		out = append(out, schema.Entity{
			Name:      e.Name,
			Fields:    fields,
			Relations: rels,
		})
	}
	return out
}

// relTypeIRToSchema maps compiler.RelationIR.Type ("1:1", "1:N", "N:M")
// to the schema.Relation.Type strings the downstream planner reads
// ("belongsTo", "hasMany"). The FromEntity side of a 1:N or N:M owns
// the collection; the FromEntity side of a 1:1 owns the foreign key.
// Anything unrecognised falls through as "hasMany" rather than getting
// dropped. A permissive default is better than a silent inconsistency.
func relTypeIRToSchema(t string) string {
	switch t {
	case "1:1":
		return "belongsTo"
	case "N:1":
		// v26.58 (D043): compiler/pass_infer.go sets "N:1" for Task.userId -> User,
		// and it is the only type the compiler sets. The FromEntity holds the
		// foreign key, so it is belongsTo, not the hasMany default.
		return "belongsTo"
	case "1:N", "N:M":
		return "hasMany"
	default:
		return "hasMany"
	}
}
