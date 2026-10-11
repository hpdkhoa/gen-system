// executor.go (v23.26): review blocks now print a positive "[review] ✓ <file>
// clean" line when a file passes with no warnings and no info notes, so a silent
// reviewer no longer looks like it didn't run.
// executor.go, v23.25 (task C): review blocks print ⚠️ only for WARN-level
// findings (Warnings()), and a quiet ℹ note for info-level ones; SQL-injection
// false positives on the in-memory backend are downgraded inside reviewer.go.
// executor.go (v23.15): handler security-review target now uses targetFileFor
// (correct <Entity>_handler.go casing) instead of a lower-cased path that
// pointed at a nonexistent file.
// executor.go, v23.10 (Phase 4-B): generate_service now also runs the LLM
// business-logic generator (engine.GenerateServiceLogic) via a LogicProvider
// chosen by GEN_LOGIC (replay offline / ollama on box); idea text + entities are
// set per run by SetLogicRun (called from RunFromPlan).
// Patched in Phase A: added structured trace at task dispatch and on unknown
// task types. Case bodies preserved verbatim. Trace import added.
// v23.2: generate_router also calls engine.GenerateBackendApp (go.mod + main.go)
// so the generated in-memory CRUD backend compiles and runs.
package agent

import (
	stdctx "context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hpdkhoa/gen-system/generator/internal/ai"
	"github.com/hpdkhoa/gen-system/generator/internal/engine"
	"github.com/hpdkhoa/gen-system/generator/internal/frontend"
	"github.com/hpdkhoa/gen-system/generator/internal/frontend/router"
	"github.com/hpdkhoa/gen-system/generator/internal/mobile"
	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
	"github.com/hpdkhoa/gen-system/generator/internal/trace"
)

// ─────────────────────────────────────────────
// Phase 4-B run state: the business-logic generator needs the user's idea text
// and a LogicProvider. These are set once per run by RunFromPlan (SetLogicRun)
// rather than threading them through every task signature. Default provider is
// the deterministic replay provider, so a run without SetLogicRun still works
// offline.
// ─────────────────────────────────────────────
var (
	runIdea        string
	runLogicProv   ai.LogicProvider
	runAllEntities []schema.Entity
)

// SetLogicRun configures the business-logic generation for the current run.
// provider may be nil → the deterministic replay provider is used.
func SetLogicRun(idea string, all []schema.Entity, provider ai.LogicProvider) {
	runIdea = idea
	runAllEntities = all
	if provider == nil {
		provider = ai.ReplayProvider()
	}
	runLogicProv = provider
}

func logicProvider() ai.LogicProvider {
	if runLogicProv == nil {
		return ai.ReplayProvider()
	}
	return runLogicProv
}

// ─────────────────────────────────────────────
// ExecuteTask dispatches a single Task to the correct engine function.
// entityMap maps entity names → schema.Entity for lookup.
// ─────────────────────────────────────────────
func ExecuteTask(task planner.Task, entityMap map[string]interface{}, rootPath string) error {
	tctx := trace.WithPhase(stdctx.Background(), "execute")
	timer := trace.Start(tctx, "ExecuteTask")
	defer timer.End("task", task.ID, "type", task.Type, "entity", task.EntityName)

	switch task.Type {

	// ── GENERATION TASKS ──────────────────────────────────────

	case "generate_model":
		e, err := resolveEntity(task, entityMap)
		if err != nil {
			return err
		}
		if err := engine.GenerateModel(e, rootPath); err != nil {
			return err // v26.58 (D029): a failed write fails the task
		}
		return nil

	case "generate_repo":
		e, err := resolveEntity(task, entityMap)
		if err != nil {
			return err
		}
		if err := engine.GenerateRepo(e, rootPath); err != nil {
			return err // v26.58 (D029): a failed write fails the task
		}
		return nil

	case "generate_service":
		e, err := resolveEntity(task, entityMap)
		if err != nil {
			return err
		}
		engine.GenerateService(e, rootPath)
		// Phase 4-B: generate the business-logic service layer from the idea +
		// schema via the selected LogicProvider (replay offline; ollama on box).
		all := runAllEntities
		if len(all) == 0 {
			all = collectEntities(entityMap)
		}
		if err := engine.GenerateServiceLogic(runIdea, e, all, logicProvider(), rootPath); err != nil {
			return err // v26.58 (D029): a failed write fails the task
		}
		// Phase 2.1, Reviewer Agent: security pass on generated service
		// v26.58 (D045): the path was built from the raw entity name, so
		// "Order Item" gave "order item_service.go", a file the generator never
		// writes; ReviewCode could not read it and reported Safe. targetFileFor
		// uses the generator's own naming, as the handler branch below does.
		targetFile := targetFileFor(e.Name, "generate_service", rootPath)
		review := ai.ReviewCode("service", e.Name, targetFile)
		if !review.Safe {
			fmt.Printf("     [review] ⚠️  Security issues in %s_service.go:\n%s\n",
				e.Name, ai.IssueHint(review.Warnings()))
			trace.Info(tctx, "review flagged service", "entity", e.Name, "issues", len(review.Warnings()))
		} else if notes := review.InfoNotes(); len(notes) > 0 {
			fmt.Printf("     [review] ℹ  %d note(s) in %s_service.go (in-memory backend; not warnings)\n",
				len(notes), e.Name)
		} else {
			fmt.Printf("     [review] ✓ %s_service.go clean\n", e.Name)
		}
		return nil

	case "generate_handler":
		e, err := resolveEntity(task, entityMap)
		if err != nil {
			return err
		}
		if err := engine.GenerateHandler(e, rootPath); err != nil {
			return err // v26.58 (D029): a failed write fails the task
		}
		engine.RenderBackend(e, rootPath)
		// Phase 2.1, Reviewer Agent: security pass on generated handler.
		// v23.15: handler files are <Entity>_handler.go (capitalized), not the
		// lower-cased name used before. The old path silently pointed the
		// reviewer at a nonexistent file, so handler issues were never surfaced.
		targetFile := targetFileFor(e.Name, "generate_handler", rootPath)
		review := ai.ReviewCode("handler", e.Name, targetFile)
		if !review.Safe {
			fmt.Printf("     [review] ⚠️  Security issues in %s_handler.go:\n%s\n",
				e.Name, ai.IssueHint(review.Warnings()))
			trace.Info(tctx, "review flagged handler", "entity", e.Name, "issues", len(review.Warnings()))
		} else if notes := review.InfoNotes(); len(notes) > 0 {
			fmt.Printf("     [review] ℹ  %d note(s) in %s_handler.go (in-memory backend; not warnings)\n",
				len(notes), e.Name)
		} else {
			fmt.Printf("     [review] ✓ %s_handler.go clean\n", e.Name)
		}
		return nil

	case "generate_validation":
		e, err := resolveEntity(task, entityMap)
		if err != nil {
			return err
		}
		if err := engine.GenerateValidation(e, rootPath); err != nil {
			return err // v26.58 (D029): a failed write fails the task
		}
		return nil

	case "generate_frontend":
		e, err := resolveEntity(task, entityMap)
		if err != nil {
			return err
		}
		// v26.58 (D032): a form or list that could not be written fails the
		// task; before, the write error was dropped and the task counted as done.
		if err := frontend.GenerateForm(e, rootPath); err != nil {
			return err
		}
		if err := frontend.GenerateList(e, rootPath); err != nil {
			return err
		}
		engine.RenderFrontend(e, rootPath)
		return nil

	// Phase 2.3: marker-based JSX evolution (preserves custom code outside markers)
	case "evolve_frontend":
		e, err := resolveEntity(task, entityMap)
		if err != nil {
			return err
		}
		frontend.EvolveForm(e, rootPath)
		frontend.EvolveList(e, rootPath)
		return nil

	case "generate_mobile":
		e, err := resolveEntity(task, entityMap)
		if err != nil {
			return err
		}
		if err := mobile.GenerateScreen(e, rootPath); err != nil {
			return err // v26.57 (D003): a missing screen now fails the task, so the healer and the summary see it
		}
		engine.RenderMobile(e, rootPath)
		return nil

	// Phase 0.3: TypeScript interface bridge
	// Writes frontend/src/types/{EntityName}.ts for each entity.
	case "generate_types":
		e, err := resolveEntity(task, entityMap)
		if err != nil {
			return err
		}
		frontend.GenerateTypeScript(e, rootPath)
		return nil

	// ── GLOBAL TASKS ──────────────────────────────────────────

	case "generate_router":
		entities := collectEntities(entityMap)
		routerErr := router.GenerateRouter(entities, rootPath)
		// v23.2: emit the in-memory backend app shell (go.mod + main.go wiring
		// every entity's routes) so the generated project compiles and runs.
		if err := engine.GenerateBackendApp(entities, rootPath); err != nil {
			return err // v26.58 (D029): a failed write fails the task
		}
		// v26.58 (D034): a router that could not be written fails the task. The
		// backend shell above does not depend on it, so it is still written.
		return routerErr

	// Phase 1.2: dynamic .env + .env.example + docker-compose.yml
	// components is extracted from the task payload (set by planner_v2).
	case "generate_env":
		entities := collectEntities(entityMap)
		var components []string
		if raw, ok := task.Payload["components"]; ok {
			if list, ok := raw.([]interface{}); ok {
				for _, c := range list {
					if s, ok := c.(string); ok {
						components = append(components, s)
					}
				}
			}
		}
		// Default components if planner didn't specify
		if len(components) == 0 {
			components = []string{"api", "database"}
		}
		engine.GenerateEnvFiles(entities, rootPath, components)
		return nil

	// Phase 1.2: TypeScript barrel: frontend/src/types/index.ts
	// re-exports all generated entity type files.
	case "generate_types_barrel":
		entities := collectEntities(entityMap)
		frontend.GenerateTypesBarrel(entities, rootPath)
		// Phase 5: the barrel/client/tsconfig now exist, so wrap the loose
		// frontend/ fragments in a buildable Vite+React+TS project (makes
		// `tsc --noEmit` + `npm run build` real gates). Mobile project scaffold
		// is emitted too when a mobile/ layer was generated (JS bundle only).
		if err := engine.GenerateWebProject(entities, rootPath); err != nil {
			return err // v26.58 (D030): a web project that cannot be written fails the task
		}
		if _, err := os.Stat(filepath.Join(rootPath, "mobile")); err == nil {
			if err := engine.GenerateMobileProject(entities, rootPath); err != nil {
				return err // v26.59 (N07): a mobile project that cannot be written fails the task
			}
		}
		return nil

	case "migrate":
		// Migration is handled via compiler pipeline; skip execution here
		// if not in a DB context. Full migrate is invoked via gen migrate.
		fmt.Println("     [migrate] Generating table SQL only (run 'gen migrate' to apply to DB)")
		entities := collectEntities(entityMap)
		for _, e := range entities {
			engine.GenerateTable(e, rootPath)
		}
		return nil

	case "test":
		return runGoVet(rootPath)

	case "fix":
		// FixTask dispatches fix tasks; this switch does not execute them
		return nil

	// Phase 3 placeholder: auth scaffolding deferred.
	// Planner emits this task when arch contains "auth"; executor skips gracefully
	// so builds don't crash. Remove this case when auth engine is implemented.
	case "generate_auth":
		fmt.Printf("     [exec] ⏭  generate_auth skipped (Phase 3, not implemented yet)\n")
		trace.Info(tctx, "task skipped (placeholder)", "type", "generate_auth")
		return nil

	// Phase 3 placeholder: worker task
	case "generate_worker":
		fmt.Printf("     [exec] ⏭  generate_worker skipped (Phase 3, not implemented yet)\n")
		trace.Info(tctx, "task skipped (placeholder)", "type", "generate_worker")
		return nil

	default:
		trace.Error(tctx, "unknown task type", "type", task.Type, "task", task.ID)
		return fmt.Errorf("unknown task type: %s", task.Type)
	}
}

// ─────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────

func resolveEntity(task planner.Task, entityMap map[string]interface{}) (schema.Entity, error) {
	name := task.EntityName
	if name == "" {
		if p, ok := task.Payload["entity"]; ok {
			name = fmt.Sprintf("%v", p)
		}
	}

	if name == "" {
		return schema.Entity{}, fmt.Errorf("task %s has no entity name", task.ID)
	}

	raw, ok := entityMap[name]
	if !ok {
		return schema.Entity{}, fmt.Errorf("entity %q not found in plan", name)
	}

	e, ok := raw.(schema.Entity)
	if !ok {
		return schema.Entity{}, fmt.Errorf("entity %q type assertion failed", name)
	}

	return e, nil
}

func collectEntities(entityMap map[string]interface{}) []schema.Entity {
	var out []schema.Entity
	for _, v := range entityMap {
		if e, ok := v.(schema.Entity); ok {
			out = append(out, e)
		}
	}
	return out
}

func runGoVet(rootPath string) error {
	backendPath := rootPath + "/backend"
	cmd := exec.Command("go", "vet", "./...")
	cmd.Dir = backendPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go vet failed:\n%s", string(out))
	}
	fmt.Println("     [test] go vet passed ✅")
	return nil
}
