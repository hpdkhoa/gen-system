// staging.go — v21: propose-before-write evolve. Runs an evolve plan against
// a staging copy of the project (under .gen/staging), produces a file-level
// diff of staging vs. live, and commits (copy staging→live) or discards on
// the user's approval. Engine writes are already rootPath-relative
// (filepath.Join(path, ...)), so pointing RunFromPlan at staging keeps every
// write inside staging — no engine-internals change required.
package services

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/agent"
	"github.com/hpdkhoa/gen-system/generator/internal/api/sse"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/planner"
)

// stagingDirName is the working copy location, under the live .gen.
const stagingDirName = ".gen/staging"

// generatedLayers are the only directories diffed/committed. We never touch
// user-imported source outside these or the .gen state.
var generatedLayers = []string{"backend", "frontend", "mobile", "db"}

// FileDiff is one changed file in a staged proposal.
type FileDiff struct {
	Path   string `json:"path"`   // path relative to project root
	Layer  string `json:"layer"`  // backend|frontend|mobile|db
	Status string `json:"status"` // added|modified|removed
	Hunks  string `json:"hunks"`  // unified-ish diff text (truncated)
	Before int    `json:"before_lines"`
	After  int    `json:"after_lines"`
}

// StageResult is returned by Stage and re-read by Diff.
type StageResult struct {
	Change     string     `json:"change"`
	StagedAt   int64      `json:"staged_at"`
	StagingDir string     `json:"staging_dir"`
	Files      []FileDiff `json:"files"`
}

// StagingService owns the staging lifecycle. One staged proposal at a time.
type StagingService struct {
	broker *sse.Broker
	snap   *SnapshotService
	root   string
	mu     sync.Mutex
	last   *StageResult
}

func NewStagingService(broker *sse.Broker, snap *SnapshotService, root string) *StagingService {
	return &StagingService{broker: broker, snap: snap, root: root}
}

func (s *StagingService) emit(dot, text string) {
	if s.broker != nil {
		s.broker.Publish("agent_event", map[string]string{"phase": "stage", "dot": dot, "text": text})
	}
}

// Pending returns the last staged proposal, or nil.
func (s *StagingService) Pending() *StageResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// Stage copies the live project into a fresh staging dir, runs the evolve
// plan there, and computes the diff. It does NOT modify live files.
func (s *StagingService) Stage(change string) (*StageResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stageRoot := filepath.Join(s.root, stagingDirName)

	// Fresh staging tree each time.
	if err := os.RemoveAll(stageRoot); err != nil {
		return nil, fmt.Errorf("clearing staging: %w", err)
	}
	if err := os.MkdirAll(stageRoot, 0755); err != nil {
		return nil, fmt.Errorf("creating staging: %w", err)
	}
	s.emit("purple", "Copying project into staging…")

	// Copy generated layers + .gen (IR) into staging so the evolve engine
	// finds prior IR and existing components to patch.
	for _, layer := range generatedLayers {
		src := filepath.Join(s.root, layer)
		if _, err := os.Stat(src); err == nil {
			if err := copyTree(src, filepath.Join(stageRoot, layer)); err != nil {
				return nil, fmt.Errorf("staging %s: %w", layer, err)
			}
		}
	}
	// Copy .gen state but never the staging dir itself (no recursion).
	genSrc := filepath.Join(s.root, ".gen")
	if _, err := os.Stat(genSrc); err == nil {
		if err := copyTreeExcept(genSrc, filepath.Join(stageRoot, ".gen"), "staging"); err != nil {
			return nil, fmt.Errorf("staging .gen: %w", err)
		}
	}

	// Build the evolve plan from the staged IR. We load IR from the live
	// .gen (same content we just copied) to enumerate entities.
	ir, err := compiler.LoadPreviousIR()
	if err != nil || len(ir.Entities) == 0 {
		return nil, fmt.Errorf("no IR found — run a full build before evolving")
	}
	var tasks []planner.Task
	for _, e := range ir.Entities {
		tasks = append(tasks, planner.Task{
			ID:         fmt.Sprintf("evolve_frontend_%s", e.Name),
			Type:       "evolve_frontend",
			EntityName: e.Name,
			Status:     planner.StatusPending,
			Payload:    map[string]interface{}{"entity": e.Name, "change": change},
		})
	}
	plan := planner.Plan{
		Goal:     fmt.Sprintf("evolve: %s", change),
		Entities: irToSchemaEntities(ir),
		Tasks:    tasks,
	}

	s.emit("purple", fmt.Sprintf("Running %d evolve tasks in staging…", len(tasks)))
	if err := agent.RunFromPlan(plan, stageRoot); err != nil {
		return nil, fmt.Errorf("staged evolve failed: %w", err)
	}

	// Diff staging vs. live across generated layers.
	files := s.computeDiff(stageRoot)
	res := &StageResult{
		Change:     change,
		StagedAt:   time.Now().UnixMilli(),
		StagingDir: stageRoot,
		Files:      files,
	}
	s.last = res
	s.emit("green", fmt.Sprintf("Staged — %d files changed, awaiting approval", len(files)))
	if s.broker != nil {
		s.broker.Publish("stage_ready", res)
	}
	return res, nil
}

// computeDiff walks the staged generated layers and compares each file to its
// live counterpart.
func (s *StagingService) computeDiff(stageRoot string) []FileDiff {
	var out []FileDiff
	for _, layer := range generatedLayers {
		stageLayer := filepath.Join(stageRoot, layer)
		_ = filepath.Walk(stageLayer, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(stageRoot, p)
			livePath := filepath.Join(s.root, rel)
			staged, _ := os.ReadFile(p)
			live, liveErr := os.ReadFile(livePath)
			if liveErr != nil {
				out = append(out, FileDiff{
					Path: rel, Layer: layer, Status: "added",
					After: countLines(staged), Hunks: addedHunks(staged),
				})
				return nil
			}
			if string(staged) == string(live) {
				return nil // unchanged
			}
			out = append(out, FileDiff{
				Path: rel, Layer: layer, Status: "modified",
				Before: countLines(live), After: countLines(staged),
				Hunks: lineDiff(string(live), string(staged)),
			})
			return nil
		})
	}
	return out
}

// Commit copies staged generated layers over live, snapshots, clears staging.
func (s *StagingService) Commit() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return 0, fmt.Errorf("nothing staged")
	}
	stageRoot := s.last.StagingDir
	n := 0
	for _, f := range s.last.Files {
		src := filepath.Join(stageRoot, f.Path)
		dst := filepath.Join(s.root, f.Path)
		if f.Status == "removed" {
			_ = os.Remove(dst)
			n++
			continue
		}
		if err := copyFileStaging(src, dst); err != nil {
			return n, fmt.Errorf("committing %s: %w", f.Path, err)
		}
		n++
	}
	change := s.last.Change
	s.last = nil
	_ = os.RemoveAll(stageRoot)
	if s.snap != nil {
		_, _ = s.snap.Create(fmt.Sprintf("Evolve: %s", strings.TrimSpace(change)),
			"Committed staged evolve", "user", "default", false)
	}
	s.emit("green", fmt.Sprintf("Committed %d files", n))
	if s.broker != nil {
		s.broker.Publish("stage_committed", map[string]int{"files": n})
	}
	return n, nil
}

// Discard throws away the staged proposal.
func (s *StagingService) Discard() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return nil
	}
	_ = os.RemoveAll(s.last.StagingDir)
	s.last = nil
	s.emit("amber", "Staged changes discarded")
	if s.broker != nil {
		s.broker.Publish("stage_discarded", map[string]bool{"ok": true})
	}
	return nil
}

// ─── small helpers (self-contained, no external deps) ─────────

func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	return strings.Count(string(b), "\n") + 1
}

func addedHunks(b []byte) string {
	lines := strings.Split(string(b), "\n")
	if len(lines) > 200 {
		lines = lines[:200]
	}
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString("+ ")
		sb.WriteString(l)
		sb.WriteString("\n")
	}
	return sb.String()
}

// lineDiff is a minimal LCS-free line diff: it emits context, - for removed,
// + for added, good enough for human review. Truncated to keep payload small.
func lineDiff(a, b string) string {
	al := strings.Split(a, "\n")
	bl := strings.Split(b, "\n")
	// Common prefix
	i := 0
	for i < len(al) && i < len(bl) && al[i] == bl[i] {
		i++
	}
	// Common suffix
	ja, jb := len(al), len(bl)
	for ja > i && jb > i && al[ja-1] == bl[jb-1] {
		ja--
		jb--
	}
	var sb strings.Builder
	ctxStart := i - 2
	if ctxStart < 0 {
		ctxStart = 0
	}
	for k := ctxStart; k < i; k++ {
		sb.WriteString("  " + al[k] + "\n")
	}
	for k := i; k < ja; k++ {
		sb.WriteString("- " + al[k] + "\n")
	}
	for k := i; k < jb; k++ {
		sb.WriteString("+ " + bl[k] + "\n")
	}
	end := jb + 2
	if end > len(bl) {
		end = len(bl)
	}
	for k := jb; k < end; k++ {
		sb.WriteString("  " + bl[k] + "\n")
	}
	out := sb.String()
	if len(out) > 8000 {
		out = out[:8000] + "\n… (truncated)"
	}
	return out
}

func copyFileStaging(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFileStaging(p, target)
	})
}

// copyTreeExcept copies src→dst but skips any top-level entry named `skip`.
func copyTreeExcept(src, dst, skip string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == skip || strings.HasPrefix(rel, skip+string(filepath.Separator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFileStaging(p, target)
	})
}
