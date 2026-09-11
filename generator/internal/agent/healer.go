// healer.go — v25.5: emitHeal() now also fans a structured AgentEvent out via
// recordHeal (JSONL trace + Studio SSE) after invoking HealObserver; the
// observer path is unchanged. Prior:
// v23.15: resolveTargetFile now delegates to targetFileFor (target_resolve.go),
// fixing the casing/dir mismatch that made every handler/validation self-heal
// fail with "no such file or directory".
// Patched in Phase A: structured trace logging added alongside existing
// fmt.Printf output. Terminal UX preserved; trace events go to .gen/trace.log
// when --trace is active, enabling diagnosis of healing failures.
package agent

import (
	stdctx "context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/ai"
	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/intelligence"
	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	"github.com/hpdkhoa/gen-system/generator/internal/rag"
	"github.com/hpdkhoa/gen-system/generator/internal/trace"
)

const maxHealAttempts = 3

// HealEvent is one observable moment in the self-healing loop (v24). It carries
// enough for a UI to show progress without exposing the full prompt/response by
// default — Detail is a short human string, not the raw model output.
type HealEvent struct {
	TaskID   string
	Func     string
	Attempt  int
	Max      int
	Phase    string // "start" | "verify" | "success" | "fail" | "exhausted"
	Strategy string
	Detail   string
}

// HealObserver, when set, is called at each step of the healing loop. It defaults
// to nil (no observation). The GENERATION side (CLI/engine) may wire this to an
// SSE broker so the Studio can show repair attempts live. Kept as a package-level
// hook — NOT a constructor dependency — so understand-server (which never links
// the agent package) is unaffected and the architecture boundary test still
// passes. The callback must not block.
var HealObserver func(HealEvent)

func emitHeal(ev HealEvent) {
	if HealObserver != nil {
		HealObserver(ev)
	}
	// v25.5: also fan out a structured record to any registered sink so heal
	// steps land in the JSONL trace and stream to the Studio. Inert when no
	// sink is set; the HealObserver path above is unchanged.
	recordHeal(ev)
}

// ─────────────────────────────────────────────
// FixTask — intelligent self-healing pipeline
//
// healer.go owns:
//  1. Error classification (intelligence.ClassifyError)
//  2. Memory cache check + apply
//  3. System graph construction for context
//  4. Retry loop: escalating context per attempt
//  5. Compile verification
//  6. Memory persistence
//
// ai/ package owns:
//   - The repair prompt string (ai.BuildRepairPrompt)
//   - The LLM call (ai.AskLLM)
//   - Output extraction (ai.ExtractBodyOnly)
//   - Output validation (ai.ValidateFunctionBody)
//
// Context escalation per attempt:
//
//	Attempt 1: error + existing code + classification hint
//	Attempt 2: + architecture summary + RAG framework docs (Phase 2.2)
//	Attempt 3: + related files from system graph
//
// ─────────────────────────────────────────────
func FixTask(task planner.Task, ctx agentctx.Context, rootPath string) error {
	tctx := trace.WithPhase(stdctx.Background(), "heal")
	timer := trace.Start(tctx, "FixTask")
	defer timer.End("task", task.ID, "entity", task.EntityName)

	if task.EntityName == "" {
		trace.Error(tctx, "missing entity name", "task", task.ID)
		return fmt.Errorf("cannot auto-fix task %s — no entity name", task.ID)
	}

	targetFile := resolveTargetFile(task, rootPath)
	if targetFile == "" {
		trace.Error(tctx, "cannot resolve target file", "task", task.ID, "type", task.Type)
		return fmt.Errorf("cannot determine target file for task %s", task.ID)
	}

	funcName := resolveFuncName(task)
	if funcName == "" {
		trace.Error(tctx, "cannot resolve func name", "task", task.ID, "type", task.Type)
		return fmt.Errorf("cannot determine function name for task %s", task.ID)
	}

	errMsg := task.Error
	existingCode := ctx.FileContent(relPath(targetFile, rootPath))

	// ── STEP 1: Classify the error ────────────────────────────
	classification := intelligence.ClassifyError(errMsg)
	fmt.Printf("     [heal] 🧠 Error classified: %s → strategy: %s\n",
		classification.ErrorType,
		classification.StrategyDescription())
	trace.Info(tctx, "error classified",
		"task", task.ID,
		"error_type", classification.ErrorType,
		"strategy", classification.Strategy,
		"target", relPath(targetFile, rootPath),
		"func", funcName,
	)

	// ── STEP 2: Check memory for cached fix ───────────────────
	mem := intelligence.LoadMemory()
	errHash := intelligence.HashError(errMsg)

	if rec, ok := mem.Get(errHash); ok && rec.SuccessCount > 0 {
		fmt.Printf("     [heal] ⚡ Memory hit — applying cached fix (success×%d)\n",
			rec.SuccessCount)
		trace.Info(tctx, "memory hit", "err_hash", errHash, "success_count", rec.SuccessCount)

		if err := ast.RewriteFunction(targetFile, funcName, rec.FixBody); err != nil {
			fmt.Printf("     [heal] ⚠️  Cached fix AST write failed — falling through to AI\n")
			trace.Error(tctx, "cached fix rewrite failed", "func", funcName, "err", err)
			mem.RecordFailure(errHash)
			_ = intelligence.SaveMemory(mem)
		} else {
			// Phase 0.2: auto-fix imports after body rewrite
			ast.AutoResolveImports(targetFile, rec.FixBody)

			if compileErr := verifyCompiles(rootPath); compileErr == nil {
				fmt.Printf("     [heal] ✅ Cached fix applied and verified\n")
				trace.Info(tctx, "cached fix verified", "func", funcName)
				mem.RecordSuccess(errHash, string(classification.ErrorType),
					task.Type, task.EntityName, rec.FixBody)
				_ = intelligence.SaveMemory(mem)
				return nil
			} else {
				fmt.Printf("     [heal] ⚠️  Cached fix did not compile — falling through to AI\n")
				trace.Info(tctx, "cached fix did not compile", "outcome", "degraded", "err", compileErr)
				mem.RecordFailure(errHash)
				_ = intelligence.SaveMemory(mem)
				_ = ast.RewriteFunction(targetFile, funcName, existingCode)
			}
		}
	}

	// ── STEP 3: Build system graph for related-file context ───
	graph := intelligence.BuildSystemGraph(ctx.Files)
	relatedFiles := graph.GetRelatedFiles(task.EntityName, ctx.Files)
	trace.Info(tctx, "system graph built", "related_files", len(relatedFiles))

	// ── STEP 4: Attempt repair with escalating context ────────
	var lastErr error
	var successBody string

	for attempt := 1; attempt <= maxHealAttempts; attempt++ {
		actx := trace.WithPhase(tctx, "heal.attempt")
		attemptTimer := trace.Start(actx, fmt.Sprintf("attempt-%d", attempt))
		fmt.Printf("     [heal] Attempt %d/%d [%s → %s]\n",
			attempt, maxHealAttempts, task.ID, classification.Strategy)
		trace.Info(actx, "attempt start",
			"attempt", attempt,
			"max", maxHealAttempts,
			"task", task.ID,
			"strategy", classification.Strategy,
		)
		emitHeal(HealEvent{
			TaskID: task.ID, Func: funcName, Attempt: attempt, Max: maxHealAttempts,
			Phase: "start", Strategy: string(classification.Strategy),
			Detail: fmt.Sprintf("attempt %d/%d", attempt, maxHealAttempts),
		})

		// Build RepairCtx — healer decides WHAT context to include per attempt.
		// ai.BuildRepairPrompt decides HOW to format it.
		rctx := ai.RepairCtx{
			TaskType:     task.Type,
			EntityName:   task.EntityName,
			FuncName:     funcName,
			ErrMsg:       errMsg,
			ErrType:      string(classification.ErrorType),
			ErrHint:      classification.Hint,
			Strategy:     string(classification.Strategy),
			ExistingCode: existingCode,
			StyleRules:   stylePromptBlock(), // v26.33: empty when no profile is active
		}

		// Attempt 2+: add architecture summary + RAG framework docs (Phase 2.2)
		if attempt >= 2 {
			rctx.ArchSummary = ctx.Summary()
			// Phase 2.2: inject retrieved framework documentation snippets.
			// SearchSummary returns "" if no docs ingested or no relevant match — safe to set.
			rctx.RAGContext = rag.SearchSummary(errMsg, 3)
			if rctx.RAGContext != "" {
				fmt.Printf("     [heal] 📚 RAG: injecting %d framework doc chunks\n",
					strings.Count(rctx.RAGContext, "["))
				trace.Info(actx, "RAG context injected", "chunks", strings.Count(rctx.RAGContext, "["))
			}
		}

		// Attempt 3: add related file content
		if attempt >= 3 && len(relatedFiles) > 0 {
			rctx.RelatedFiles = make(map[string]string)
			for _, f := range relatedFiles {
				content := ctx.FileContent(f)
				if content != "" && len(content) < 1500 {
					rctx.RelatedFiles[f] = content
				}
			}
			trace.Info(actx, "related files added", "count", len(rctx.RelatedFiles))
		}

		// All prompt building is in ai.BuildRepairPrompt
		prompt := ai.BuildRepairPrompt(rctx)

		resp, err := ai.AskLLM(prompt)
		if err != nil {
			lastErr = fmt.Errorf("AI call failed (attempt %d): %w", attempt, err)
			trace.Error(actx, "AI call failed", "attempt", attempt, "err", err)
			attemptTimer.End("outcome", "ai_error")
			continue
		}

		body := ai.ExtractBodyOnly(resp)
		if err := ai.ValidateFunctionBody(body); err != nil {
			lastErr = fmt.Errorf("AI output invalid (attempt %d): %w", attempt, err)
			trace.Error(actx, "AI output invalid", "attempt", attempt, "err", err)
			attemptTimer.End("outcome", "invalid_body")
			continue
		}

		if err := ast.RewriteFunction(targetFile, funcName, body); err != nil {
			lastErr = fmt.Errorf("AST rewrite failed (attempt %d): %w", attempt, err)
			trace.Error(actx, "AST rewrite failed", "attempt", attempt, "func", funcName, "err", err)
			attemptTimer.End("outcome", "rewrite_error")
			continue
		}

		// Phase 0.2: resolve any new imports introduced by the AI-generated body
		ast.AutoResolveImports(targetFile, body)

		if compileErr := verifyCompiles(rootPath); compileErr != nil {
			lastErr = fmt.Errorf("compile failed after attempt %d: %w", attempt, compileErr)
			fmt.Printf("     [heal] ❌ Compile check failed — restoring and retrying\n")
			trace.Info(actx, "compile failed, restoring", "attempt", attempt, "outcome", "degraded", "err", compileErr)
			_ = ast.RewriteFunction(targetFile, funcName, existingCode)
			attemptTimer.End("outcome", "compile_fail")
			continue
		}

		successBody = body
		fmt.Printf("     [heal] ✅ Verified (attempt %d) — %s in %s\n",
			attempt, funcName, filepath.Base(targetFile))
		trace.Info(actx, "attempt succeeded", "attempt", attempt, "func", funcName)
		emitHeal(HealEvent{
			TaskID: task.ID, Func: funcName, Attempt: attempt, Max: maxHealAttempts,
			Phase: "success", Strategy: string(classification.Strategy),
			Detail: "verified",
		})
		attemptTimer.End("outcome", "fixed")
		break
	}

	if successBody == "" {
		trace.Error(tctx, "healing exhausted", "attempts", maxHealAttempts, "task", task.ID, "err", lastErr)
		emitHeal(HealEvent{
			TaskID: task.ID, Func: funcName, Attempt: maxHealAttempts, Max: maxHealAttempts,
			Phase: "exhausted", Strategy: string(classification.Strategy),
			Detail: "all attempts failed",
		})
		return fmt.Errorf("healing exhausted after %d attempts: %w", maxHealAttempts, lastErr)
	}

	// ── STEP 5: Save successful fix to memory ─────────────────
	mem.RecordSuccess(errHash, string(classification.ErrorType),
		task.Type, task.EntityName, successBody)
	mem.Evict()
	if err := intelligence.SaveMemory(mem); err != nil {
		fmt.Printf("     [heal] ⚠️  Failed to save memory: %s\n", err)
		trace.Error(tctx, "memory save failed", "err", err)
	} else {
		fmt.Printf("     [heal] 💾 Fix saved to memory (%s)\n", mem.Stats())
		trace.Info(tctx, "memory saved", "stats", mem.Stats())
	}

	return nil
}

// verifyCompiles runs go build to confirm a repair actually compiles.
func verifyCompiles(rootPath string) error {
	for _, subdir := range []string{"backend", "generator"} {
		dir := filepath.Join(rootPath, subdir)
		if _, err := exec.Command("test", "-d", dir).Output(); err == nil {
			cmd := exec.Command("go", "build", "./...")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("go build: %s", strings.TrimSpace(string(out)))
			}
			return nil
		}
	}
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = rootPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// ─────────────────────────────────────────────
// helpers — no AI calls
// ─────────────────────────────────────────────

// resolveTargetFile returns the generated file a heal task targets. v23.15: it
// now delegates to targetFileFor (see target_resolve.go), which uses the
// generator's REAL per-layer filename conventions — capitalized handler/
// validation files, the store (not repository) dir — and falls back to a
// case-insensitive directory scan. The previous all-lowercase paths (e.g.
// customer_handler.go) never existed, so every handler/validation AST rewrite
// failed with "no such file or directory" and the self-heal loop could not
// repair those layers.
func resolveTargetFile(task planner.Task, rootPath string) string {
	return targetFileFor(task.EntityName, task.Type, rootPath)
}

func resolveFuncName(task planner.Task) string {
	name := task.EntityName
	switch task.Type {
	case "generate_repo", "generate_service", "generate_handler":
		return "GetAll" + name
	case "generate_validation":
		return "Validate" + name
	}
	return ""
}

func relPath(abs, root string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return abs
	}
	return rel
}
