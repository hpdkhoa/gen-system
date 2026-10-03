// agent.go — v25.5: (step 2) RunFromPlan runs an Understand-verify pass after the
// validation loop (runVerifyStage) — the Understand track validates Generate's
// output; stop|autofix via VerifyUnderstandMode, both reporting to the human.
// (step 1) emit() also fans a structured AgentEvent out to any registered sink
// (recordLifecycle) after its existing fmt.Print + EventHook. Inert with no sink
// / VerifyOff. Prior:
// v23.15: RunFromPlan prunes orphaned per-entity files from a prior
// run (pruneOrphanEntityFiles) after the snapshot and before execution, so a
// reused dir with a different entity set can't break the build with stale
// service/handler/model files referencing now-absent store interfaces.
// agent.go — v23.13 (Phase 5): optional web/mobile/docker build gates (runFrontendGates) after the Go validation loop, each opt-in via GEN_WEB_GATE/GEN_MOBILE_GATE/GEN_DOCKER_GATE.
package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/ai"
	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	"github.com/hpdkhoa/gen-system/generator/internal/safety"
	"github.com/hpdkhoa/gen-system/generator/internal/style"
)

const maxValidationRounds = 3
const maxConsecutiveFails = 3

// EventHook, if set, receives agent lifecycle events via gen-server SSE.
// Nil in CLI mode — plain fmt.Printf is used instead.
var EventHook func(phase, message string)

func emit(phase, msg string) {
	// v18.3: was fmt.Printf(msg) — Go vet flags this as non-constant
	// format string. Use fmt.Print which doesn't interpret % verbs.
	fmt.Print(msg)
	if EventHook != nil {
		EventHook(phase, msg)
	}
	// v25.5: also fan out a structured record to any registered sink (JSONL
	// trace for CLI, SSE broker for the Studio). Inert when no sink is set.
	recordLifecycle(phase, msg)
}

// ─────────────────────────────────────────────
// RunFromPlan executes an already-reviewed Plan.
//
// Flow:
//  1. Snapshot the project (Time Machine)
//  2. Load context, topological sort
//  3. Execute each task; on failure: self-heal
//  4. If consecutiveFails >= 3: auto-rollback to snapshot
//  5. After all tasks: validation loop (go vet + go build)
//
// ─────────────────────────────────────────────
func RunFromPlan(plan planner.Plan, rootPath string) error {
	ctx := agentctx.LoadContextWithOpts(rootPath, LoadOpts) // v26.34: stdlib auto detected

	// v25.5: reset per-run Understand-verify state so a prior run's halt/report
	// never leaks into this one.
	verifyHalted = false
	LastVerifyReport = VerifyReport{}
	styleHalted = false
	LastStyleReport = style.Report{}

	// Phase 4-B: configure business-logic generation for this run. The idea text
	// comes from the plan goal; the provider is chosen by GEN_LOGIC (replay by
	// default, ollama when opted in).
	SetLogicRun(plan.Goal, collectEntities(buildEntityMap(plan)), ai.SelectLogicProvider())

	emit("start", fmt.Sprintf("\n🤖 Agent starting — %d tasks to execute\n", len(plan.Tasks)))
	emit("start", fmt.Sprintf("   Root: %s\n", rootPath))
	emit("start", fmt.Sprintf("   Context: %d files, %d entities tracked\n",
		len(ctx.Files), len(ctx.IR.Entities)))
	emit("start", fmt.Sprintf("   Stdlib: %s, %d root(s) attached\n\n",
		ctx.StdlibReport.Mode, len(ctx.StdlibReport.Roots)))

	// ── Phase 1.1: Snapshot before any mutation ───────────────────
	snapDir, snapErr := safety.SnapshotProject(rootPath)
	if snapErr != nil {
		emit("warn", fmt.Sprintf("⚠️  Pre-build snapshot failed: %s (continuing without rollback)\n", snapErr))
		snapDir = ""
	} else {
		emit("snapshot", fmt.Sprintf("📸 Snapshot taken → %s\n", filepath.Base(snapDir)))
	}

	// v23.15: prune orphaned per-entity files from a PREVIOUS run whose entity
	// set differed (the V2 planner is non-deterministic). Done AFTER the
	// snapshot (so it's rollback-covered) and BEFORE any generate task, so a
	// reused directory yields a clean, self-consistent backend instead of one
	// poisoned by e.g. service/user_service.go referencing a store.UserCRUD the
	// current run never generates ("undefined: store.UserCRUD").
	{
		var names []string
		for _, e := range plan.Entities {
			names = append(names, e.Name)
		}
		pruneOrphanEntityFiles(rootPath, names)
	}

	// ── Execution setup ───────────────────────────────────────────
	ordered := planner.TopologicalOrder(plan.Tasks)
	results := map[string]planner.TaskStatus{}
	entityMap := buildEntityMap(plan)
	failCount := 0
	consecutiveFails := 0

	// ── EXECUTION PHASE ───────────────────────────────────────────
	for _, task := range ordered {
		if hasDependencyFailure(task, results) {
			emit("skip", fmt.Sprintf("  ⏭  SKIP  [%s] — dependency failed\n", task.ID))
			results[task.ID] = planner.StatusSkipped
			continue
		}

		emit("run", fmt.Sprintf("  ▶  RUN   [%s] (%s)\n", task.ID, task.Type))

		err := ExecuteTask(task, entityMap, rootPath)
		if err == nil {
			emit("done", fmt.Sprintf("  ✅ DONE  [%s]\n", task.ID))
			results[task.ID] = planner.StatusDone
			consecutiveFails = 0 // reset on any success
			continue
		}

		// Task failed — attempt self-heal
		emit("fail", fmt.Sprintf("  ❌ FAIL  [%s]: %s\n", task.ID, err.Error()))
		task.Error = err.Error()

		emit("heal", fmt.Sprintf("  🔧 HEAL  [%s] — self-healing (up to %d attempts)...\n",
			task.ID, maxHealAttempts))
		healErr := FixTask(task, ctx, rootPath)

		if healErr != nil {
			emit("fail", fmt.Sprintf("  ❌ HEAL FAILED [%s]: %s\n", task.ID, healErr.Error()))
			results[task.ID] = planner.StatusFailed
			failCount++
			consecutiveFails++

			// Auto-rollback after maxConsecutiveFails straight failures
			if consecutiveFails >= maxConsecutiveFails && snapDir != "" {
				emit("rollback", fmt.Sprintf(
					"\n🔄 %d consecutive failures — rolling back to pre-build snapshot\n",
					maxConsecutiveFails))

				if rerr := safety.RestoreSnapshot(rootPath, snapDir); rerr != nil {
					emit("warn", fmt.Sprintf("⚠️  Rollback failed: %s\n", rerr))
				} else {
					emit("rollback", "✅ Rolled back to last working state\n")
				}
				return fmt.Errorf(
					"auto-rolled back after %d consecutive failures (last: %s)",
					maxConsecutiveFails, healErr)
			}
			continue
		}

		emit("heal", fmt.Sprintf("  ✅ HEAL OK [%s]\n", task.ID))
		results[task.ID] = planner.StatusDone
		consecutiveFails = 0 // successful heal resets the counter
	}

	// ── VALIDATION LOOP ───────────────────────────────────────────
	backendPath := filepath.Join(rootPath, "backend")
	if _, err := os.Stat(backendPath); err == nil {
		emit("validate", fmt.Sprintf("\n🔬 Validation loop starting (up to %d rounds)...\n",
			maxValidationRounds))
		vErr := runValidationLoop(rootPath, backendPath, ctx, maxValidationRounds)
		if vErr != nil {
			emit("warn", fmt.Sprintf("⚠️  Validation ended with issues: %s\n", vErr))
		} else {
			emit("validate", "✅ Validation loop passed\n")
		}

		// v25.5 (step 2): Understand validates Generate's output. Runs the
		// generated backend through the comprehension engine and, per
		// VerifyUnderstandMode, either reports+halts (stop) or routes findings to
		// the repair loop (autofix). Both always report to the human. Off by
		// default, so callers that don't set the mode are unaffected. A halt is
		// surfaced via the returned VerifyReport on RunFromPlan (below).
		if rep, halt := runVerifyStage(backendPath, rootPath, ctx); halt {
			LastVerifyReport = rep
			verifyHalted = true
		} else {
			LastVerifyReport = rep
		}

		// v26.33: the team style gate, after the structural one. Same
		// contract: report to the human always; halt only in gate mode on
		// error findings; never modify in report mode.
		LastStyleReport, styleHalted = runStyleStage(backendPath, rootPath, ctx)
	}

	// ── PHASE 5: OPTIONAL FRONTEND / DOCKER GATES ─────────────────
	// Off by default so the offline path needs no Node/Docker. Opt in with
	// GEN_WEB_GATE=1 (runs `tsc --noEmit` + `npm run build` on the generated
	// web project) and/or GEN_DOCKER_GATE=1 (runs `docker build`). A failing
	// gate warns but does not abort — the Go backend remains the hard guarantee.
	runFrontendGates(rootPath)

	// v25.5 (step 4): optional HTTP liveness smoke — boot the built backend and
	// GET each collection route, asserting non-000/non-5xx. Opt-in via
	// GEN_HTTP_SMOKE=1, advisory (warn, never abort), read-only probes only.
	runHTTPSmoke(backendPath)

	// ── SUMMARY ───────────────────────────────────────────────────
	done, failed, skipped := countResults(results)
	emit("summary", fmt.Sprintf(
		"\n──────────────────────────────────────────\n"+
			"Agent complete: ✅ %d done  ❌ %d failed  ⏭ %d skipped\n",
		done, failed, skipped))

	if failCount > 0 {
		return fmt.Errorf("%d task(s) failed — review output above", failCount)
	}
	return nil
}

// ─────────────────────────────────────────────
// runValidationLoop: execute → test → fix → repeat
// ─────────────────────────────────────────────
func runValidationLoop(rootPath, backendPath string, ctx agentctx.Context, rounds int) error {
	for round := 1; round <= rounds; round++ {
		emit("validate", fmt.Sprintf("  [validate] Round %d/%d — go vet + go build...\n",
			round, rounds))

		vetOut, vetErr := runCmd("go", []string{"vet", "./..."}, backendPath)
		if vetErr == nil {
			buildOut, buildErr := runCmd("go", []string{"build", "./..."}, backendPath)
			if buildErr == nil {
				// Build is green. Run the generated tests too — crucially the
				// per-service no-panic contract test, which calls each model
				// operation and fails if it panics at runtime (e.g. an unchecked
				// type assertion on the payload). A body that COMPILES but
				// PANICS would otherwise reach the user as a 500/crash.
				testOut, testErr := runCmd("go", []string{"test", "./..."}, backendPath)
				if testErr == nil {
					emit("validate", fmt.Sprintf("  [validate] ✅ Round %d passed (vet + build + test)\n", round))
					return nil
				}
				// A failing test means a runtime defect in a generated op. Surface
				// it and attempt repair on the implicated SERVICE file(s). We
				// must NOT feed extractBrokenFiles here: it would return the
				// *_test.go file (which has a .go: line), and repairing the test
				// is wrong — the defect is in the service body the test exercises.
				emit("validate", "  [validate] ⚠️  go test failed — a generated operation misbehaves at runtime\n")
				broken := servicesFromTestOutput(testOut, rootPath)
				if len(broken) > 0 {
					dispatchRepairs(broken, testOut, ctx, rootPath, round)
					continue
				}
				// Could not localize the failure to a service file; build is green
				// so the app still runs (the dispatcher recovers panics as 500).
				// Report and accept rather than loop forever.
				emit("validate", "  [validate] (test failure not localizable to a service; backend builds — continuing)\n")
				return nil
			}
			emit("validate", "  [validate] ❌ go build failed — extracting broken files\n")
			dispatchRepairs(extractBrokenFiles(buildOut), buildOut, ctx, rootPath, round)
			continue
		}

		emit("validate", "  [validate] ❌ go vet failed — extracting broken files\n")
		dispatchRepairs(extractBrokenFiles(vetOut), vetOut, ctx, rootPath, round)
	}
	return fmt.Errorf("validation still failing after %d rounds", rounds)
}

// runFrontendGates runs the Phase 5 web/mobile/docker build gates. v25.5: the
// WEB gate is now default-ON for internal builds. "Fully executable" should mean
// that the backend and the frontend both build. It is opt-OUT via GEN_WEB_GATE=0
// (and still explicitly opt-in-able with =1). It remains ADVISORY (warns, never
// aborts) and a NO-OP when there is no generated frontend or Node is unavailable,
// so a Node-less offline box is unaffected. Mobile/docker stay opt-IN (heavier,
// need extra toolchains). All gates no-op unless their generated dir exists.
func runFrontendGates(rootPath string) {
	// Web: tsc --noEmit + npm run build. Default ON; GEN_WEB_GATE=0 disables.
	if webGateEnabled() {
		webDir := filepath.Join(rootPath, "frontend")
		if _, err := os.Stat(filepath.Join(webDir, "package.json")); err == nil {
			if !nodeAvailable() {
				emit("warn", "  [web] ⚠️  skipping web gate — Node/npm not found on PATH (set GEN_WEB_GATE=0 to silence)\n")
			} else {
				emit("validate", "\n🌐 Web gate (default on; GEN_WEB_GATE=0 to disable): npm ci + tsc --noEmit + vite build...\n")
				if out, err := runCmd("npm", []string{"ci"}, webDir); err != nil {
					emit("warn", fmt.Sprintf("  [web] ⚠️  npm ci failed:\n%s\n", tail(out)))
				} else if out, err := runCmd("npm", []string{"run", "build"}, webDir); err != nil {
					emit("warn", fmt.Sprintf("  [web] ⚠️  web build/type-check failed:\n%s\n", tail(out)))
				} else {
					emit("validate", "  [web] ✅ web project type-checks and builds\n")
				}
			}
		}
	}
	// Mobile: JS bundle only (NOT a signed app).
	if os.Getenv("GEN_MOBILE_GATE") == "1" {
		mDir := filepath.Join(rootPath, "mobile")
		if _, err := os.Stat(filepath.Join(mDir, "package.json")); err == nil {
			emit("validate", "\n📱 Mobile gate (GEN_MOBILE_GATE=1): npm ci + RN JS bundle (not a signed app)...\n")
			if out, err := runCmd("npm", []string{"ci"}, mDir); err != nil {
				emit("warn", fmt.Sprintf("  [mobile] ⚠️  npm ci failed:\n%s\n", tail(out)))
			} else if out, err := runCmd("npm", []string{"run", "bundle:android"}, mDir); err != nil {
				emit("warn", fmt.Sprintf("  [mobile] ⚠️  RN bundle failed (verify on a box with the RN toolchain):\n%s\n", tail(out)))
			} else {
				emit("validate", "  [mobile] ✅ JS bundle produced (signed app is a separate native build)\n")
			}
		}
	}
	// Docker: docker build of the backend image.
	if os.Getenv("GEN_DOCKER_GATE") == "1" {
		if _, err := os.Stat(filepath.Join(rootPath, "Dockerfile")); err == nil {
			emit("validate", "\n🐳 Docker gate (GEN_DOCKER_GATE=1): docker build...\n")
			if out, err := runCmd("docker", []string{"build", "-t", "generated-app:latest", "."}, rootPath); err != nil {
				emit("warn", fmt.Sprintf("  [docker] ⚠️  docker build failed (is a daemon running?):\n%s\n", tail(out)))
			} else {
				emit("validate", "  [docker] ✅ backend image builds\n")
			}
		}
	}
}

// webGateEnabled reports whether the web build gate should run. v25.5: default ON
// (part of "fully executable" for internal builds). GEN_WEB_GATE=0 disables it;
// GEN_WEB_GATE=1 is still honoured (explicit on). Any other value → default on.
func webGateEnabled() bool {
	return os.Getenv("GEN_WEB_GATE") != "0"
}

// nodeAvailable reports whether npm is on PATH, so the web gate can no-op cleanly
// on a Node-less offline box instead of emitting a scary npm failure.
func nodeAvailable() bool {
	_, err := exec.LookPath("npm")
	return err == nil
}

// tail returns the last ~1200 chars of build output so warnings stay readable.
func tail(s string) string {
	const n = 1200
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// servicesFromTestOutput maps a failing no-panic contract test back to the
// service source file to repair. A `go test` failure for a panicking operation
// reports both a test name like "Test<Entity>Service_OperationsHonorContract"
// and a "<entity>_service_test.go:NN:" file line; either form is mapped to the
// corresponding "<entity>_service.go" so the SERVICE body (not the test) is
// repaired.
func servicesFromTestOutput(output, rootPath string) []string {
	seen := map[string]bool{}
	var files []string
	add := func(entity string) {
		if entity == "" {
			return
		}
		fname := strings.ToLower(entity) + "_service.go"
		rel := "backend/internal/service/" + fname
		if !seen[rel] {
			seen[rel] = true
			files = append(files, rel)
		}
	}
	for _, line := range strings.Split(output, "\n") {
		// Form A: "<entity>_service_test.go:NN: <Op> panicked…"
		if i := strings.Index(line, "_service_test.go"); i != -1 {
			// walk back to the start of the file token
			start := strings.LastIndexAny(line[:i], " \t/")
			ent := line[start+1 : i]
			add(ent) // entity is the lowercased route name, e.g. "product"
			continue
		}
		// Form B: "--- FAIL: Test<Entity>Service_OperationsHonorContract"
		if k := strings.Index(line, "Test"); k != -1 {
			rest := line[k+len("Test"):]
			if j := strings.Index(rest, "Service"); j > 0 {
				add(rest[:j])
			}
		}
	}
	return files
}

func extractBrokenFiles(output string) []string {
	seen := map[string]bool{}
	var files []string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, ".go:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) > 0 {
				f := strings.TrimSpace(parts[0])
				f = strings.TrimPrefix(f, "./")
				if !seen[f] {
					seen[f] = true
					files = append(files, f)
				}
			}
		}
	}
	return files
}

// repairEvidence returns the lines of output that name the file, or the tail of
// the whole output when no line does (a failing test names its _test.go file,
// not the service it exercises). v26.57 (D011): the healer finds the function
// to repair from these lines; before, every synthetic task carried only the
// text "compilation error in <file>".
func repairEvidence(output, file string) string {
	base := strings.ToLower(filepath.Base(file))
	var lines []string
	for _, l := range strings.Split(output, "\n") {
		if strings.Contains(strings.ToLower(l), base) {
			lines = append(lines, strings.TrimRight(l, "\r"))
		}
	}
	if len(lines) > 0 {
		return strings.Join(lines, "\n")
	}
	return tail(output)
}

// dispatchRepairs hands each broken file to FixTask as a synthetic task whose
// Error holds the output lines about that file.
func dispatchRepairs(files []string, output string, ctx agentctx.Context, rootPath string, round int) {
	for _, f := range files {
		emit("repair", fmt.Sprintf("  [validate] 🔧 Repairing: %s\n", f))
		entityName, taskType := inferEntityFromPath(f)
		if entityName == "" {
			emit("warn", fmt.Sprintf("  [validate] ⚠️  Cannot infer entity from %s\n", f))
			continue
		}
		synthetic := planner.Task{
			ID:         fmt.Sprintf("validate_fix_%s_%d", entityName, round),
			Type:       taskType,
			EntityName: entityName,
			Error:      fmt.Sprintf("errors in %s (round %d):\n%s", f, round, repairEvidence(output, f)),
			Status:     planner.StatusFailed,
		}
		if err := FixTask(synthetic, ctx, rootPath); err != nil {
			emit("repair", fmt.Sprintf("  [validate] ❌ Repair failed for %s: %s\n", f, err))
		} else {
			emit("repair", fmt.Sprintf("  [validate] ✅ Repaired %s\n", f))
		}
	}
}

func inferEntityFromPath(path string) (string, string) {
	// v26.58 (D104): the cases below look for forward slashes, but on Windows
	// go build prints internal\service\order_service.go and a verify finding
	// carries an absolute C:\...\ path, so no case matched and no repair ran.
	// Both separators now count. The proposed filepath.ToSlash would leave a
	// backslash path unchanged on a system whose separator is "/", so the
	// backslash is replaced directly.
	path = strings.ReplaceAll(path, `\`, "/")
	lower := strings.ToLower(path)
	var layer string
	switch {
	case strings.Contains(lower, "/model/"):
		layer = "generate_model"
	case strings.Contains(lower, "/repository/"):
		layer = "generate_repo"
	case strings.Contains(lower, "/store/"):
		// v26.58 (D079): the generator writes the repo layer to internal/store
		// (layerFor in target_resolve.go); the old "repository" dir is gone, so
		// an error in a store file matched no case and got no repair.
		layer = "generate_repo"
	case strings.Contains(lower, "/service/"):
		layer = "generate_service"
	case strings.Contains(lower, "/handler/"):
		layer = "generate_handler"
	case strings.Contains(lower, "/validation/"):
		layer = "generate_validation"
	default:
		return "", ""
	}
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".go")
	for _, sfx := range []string{"_handler", "_repo", "_service", "_validation", "_tx", "_mapper"} {
		base = strings.TrimSuffix(base, sfx)
	}
	if len(base) > 0 {
		return strings.ToUpper(base[:1]) + base[1:], layer
	}
	return "", ""
}

func runCmd(name string, args []string, dir string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func buildEntityMap(plan planner.Plan) map[string]interface{} {
	m := map[string]interface{}{}
	for _, e := range plan.Entities {
		m[e.Name] = e
	}
	return m
}

func hasDependencyFailure(task planner.Task, results map[string]planner.TaskStatus) bool {
	for _, dep := range task.Dependencies {
		if st, ok := results[dep]; ok && (st == planner.StatusFailed || st == planner.StatusSkipped) {
			return true
		}
	}
	return false
}

func countResults(results map[string]planner.TaskStatus) (done, failed, skipped int) {
	for _, v := range results {
		switch v {
		case planner.StatusDone:
			done++
		case planner.StatusFailed:
			failed++
		case planner.StatusSkipped:
			skipped++
		}
	}
	return
}
