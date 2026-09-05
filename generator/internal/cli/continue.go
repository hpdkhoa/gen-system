// generator/internal/cli/continue.go — v23.24 (task 4A): `gen continue` — take
// an EXISTING codebase plus a free-text idea and continue building it. It loads
// the existing code through gen-system's own Understand engine
// (context.LoadContext → compiler-accurate entities/fields/ops), asks the
// planner for a DELTA grounded in what already exists, stages the result via the
// v23.19 evolve-review safety path (source untouched until --apply), and runs
// the same agent loop (compile-gate + per-operation repair) as every other
// generating command.
//
// Surface:
//
//	gen continue <path> "<idea>" [--v2] [--apply] [--review-dir <dir>]
//	             [--force|--yes] [--allow-remove]
//	gen idea --from <path> "<idea>"        # thin alias → same code path
//
// GENERATE-side only; understand-server must never link this (archtest enforces).
package cli

import (
	"fmt"
	"os"
	"strings"

	"gen-system/generator/internal/agent"
	"gen-system/generator/internal/belief"
	"gen-system/generator/internal/compiler"
	"gen-system/generator/internal/context"
	planner_v2 "gen-system/generator/internal/planner_v2"
	"gen-system/generator/internal/schema"
)

// Continue handles `gen continue <path> "<idea>" [flags]`.
func Continue() {
	path, idea, ok := parseContinueArgs(os.Args[2:])
	if !ok {
		printContinueUsage()
		return
	}
	ContinueFromInput(path, idea)
}

// ContinueViaIdeaFrom handles `gen idea --from <path> "<idea>"`. The --from
// value and the idea text are extracted from the full idea-arg list.
func ContinueViaIdeaFrom(path string, ideaParts []string) {
	idea := strings.TrimSpace(strings.Join(ideaParts, " "))
	if path == "" || idea == "" {
		printContinueUsage()
		return
	}
	ContinueFromInput(path, idea)
}

// ContinueFromInput is the shared implementation.
func ContinueFromInput(path, idea string) {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(idea) == "" {
		printContinueUsage()
		return
	}
	if _, err := os.Stat(path); err != nil {
		fmt.Printf("❌ Source path not found: %s\n", path)
		return
	}

	allowRemove := HasFlag("allow-remove")

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🔁 Continue from existing code")
	fmt.Println("📂 Source:", path)
	fmt.Println("💡 Idea:  ", idea)
	if allowRemove {
		fmt.Println("⚠️  --allow-remove: REMOVE changes will be applied")
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// ── 1. UNDERSTAND the existing code ──────────────────────────
	fmt.Println("🔬 Understanding existing code…")
	ctx := context.LoadContext(path)
	pct, nSyms, nEdges := resolutionStats(ctx.Index)
	fmt.Printf("   resolved %.1f%% of %d call edges across %d symbols\n", pct, nEdges, nSyms)

	existingState := planner_v2.SummarizeExisting(&ctx)
	existingSummary := planner_v2.RenderExistingSummary(existingState)
	fmt.Printf("   existing entities: %d\n", len(existingState.EntityOrder))

	// Steer the delta with any human belief corrections for this project. A person
	// who corrected the machine's understanding in the Understand UI has their
	// corrections appended to the "what exists" block, so the planner honors them
	// (session 5). Empty when the project has no corrections — no-op then.
	if cc := belief.BuildCorrectionContext(ctx.Root, ctx.Index, ctx.Effects); cc != "" {
		existingSummary += "\n\n" + cc
		fmt.Println("   applied human belief corrections to the plan context")
	}

	// The authoritative existing entity set comes from the TARGET codebase's
	// own model structs (the deterministic inverse of what the generator wrote).
	// We deliberately do NOT trust a .gen IR snapshot here: it lives in the
	// working directory, not the target, so for an arbitrary <path> it is stale
	// or unrelated. Model recovery is the only source tied to the actual target.
	existingEntities := planner_v2.RecoverEntitiesFromModels(ctx.Root)
	if len(existingEntities) == 0 {
		// No recognizable model structs (e.g. a non-generated or empty project):
		// fall back to whatever IR context could load, rather than nothing.
		existingEntities = irEntitiesToSchema(ctx.IR)
	}

	// ── 2. Resolve the staged write target (source untouched by default) ─
	writeDir, inPlace, ok := ResolveEvolveTarget(path)
	if !ok {
		return
	}
	_ = inPlace

	// ── 3. Plan the DELTA grounded in what exists ────────────────
	fmt.Println("🧠 Planning the delta (model call)…")
	result, err := planner_v2.BuildContinuePlan(existingEntities, existingSummary, idea, writeDir, allowRemove)
	if err != nil {
		fmt.Println("❌ Delta planning failed:", err)
		return
	}
	fmt.Println("   change set:", result.Summary)
	if len(result.ChangedSet) == 0 {
		fmt.Println("✅ Nothing to do — the existing code already satisfies the idea.")
		return
	}
	fmt.Printf("   regenerating %d entit%s; merged schema has %d total\n",
		len(result.ChangedSet), plural1(len(result.ChangedSet)), len(result.Merged.Entities))

	// ── 3.5 CONSENT GATE (v25.7) ─────────────────────────────────
	// Before staging/generating, show what exists + what the delta changes, push
	// back on conflicts with the existing code's purpose, and wait for
	// confirmation. Default on; --no-gate restores the old silent behaviour;
	// --yes/--force skip the prompt (conflicts still printed).
	if !HasFlag("no-gate") {
		if !RunConsentGate(existingState, result, result.Plan) {
			return
		}
	}

	// ── 4. Run the agent (compile-gate + repair-loop containment) ─
	fmt.Printf("🚀 Running agent (%d entities, %d tasks)…\n",
		len(result.Plan.Entities), len(result.Plan.Tasks))
	// v25.5: activate the JSONL agent trace if --heal-log was passed.
	defer SetupHealLog()()
	SetupVerifyUnderstand() // v25.5 step 2: Understand-validates-Generate mode
	SetupStyle()            // v26.33: team style profile (--style / GEN_STYLE)
	if err := agent.RunFromPlan(result.Plan, writeDir); err != nil {
		fmt.Println("❌ Continuation failed:", err)
		return
	}

	// v25.5 step 2: if the Understand-verify pass halted in stop mode, write a
	// findings report next to the staged review dir so the human has a written
	// artifact alongside the untouched staged output.
	WriteVerifyFindingsIfHalted(writeDir)

	// Note: we deliberately do NOT write a .gen IR snapshot here. continue's
	// merge base is recovered from the target's model structs, not from an IR
	// snapshot, and writing one into the working directory would poison a later
	// continue/evolve run that happens to share the cwd.

	fmt.Println("✅ Continuation complete.")
	if !inPlace {
		fmt.Printf("   Staged in: %s (source untouched)\n", writeDir)
		fmt.Printf("   Review, then apply with:\n")
		fmt.Printf("     gen continue %s \"%s\" --apply\n", path, idea)
	}
}

// parseContinueArgs splits `<path> "<idea>"` out of the args after `continue`,
// ignoring known flags and their values.
func parseContinueArgs(args []string) (path, idea string, ok bool) {
	var positional []string
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		switch {
		case a == "--review-dir" || a == "--env":
			skipNext = true
		case strings.HasPrefix(a, "--"):
			// boolean flags (--v2, --apply, --force, --yes, --allow-remove) or
			// --flag=value forms; consumed by HasFlag/GetFlag elsewhere.
		default:
			positional = append(positional, a)
		}
	}
	if len(positional) < 2 {
		return "", "", false
	}
	path = positional[0]
	idea = strings.TrimSpace(strings.Join(positional[1:], " "))
	return path, idea, idea != ""
}

func printContinueUsage() {
	fmt.Println("Usage: gen continue <path> \"<what to add or change>\" [flags]")
	fmt.Println("  Continue building an EXISTING app from a natural-language idea.")
	fmt.Println("")
	fmt.Println("  --v2            Use the multi-step reasoning planner")
	fmt.Println("  --apply         Write changes into <path> in place (default: stage to review dir)")
	fmt.Println("  --review-dir D  Stage into D instead of <path>.evolve-review")
	fmt.Println("  --allow-remove  Honor REMOVE changes (destructive; off by default)")
	fmt.Println("  --force/--yes   Skip confirmation prompts")
	fmt.Println("")
	fmt.Println("  Alias: gen idea --from <path> \"<idea>\"")
}

// irEntitiesToSchema converts the loaded compiler IR (entities + relations) into
// []schema.Entity so the delta planner and merge operate on schema types. IR
// relations are mapped onto their owning entity as belongsTo so existing FKs are
// preserved through the merge.
func irEntitiesToSchema(ir compiler.SchemaIR) []schema.Entity {
	byName := map[string]int{}
	out := make([]schema.Entity, 0, len(ir.Entities))
	for _, e := range ir.Entities {
		fields := make([]schema.Field, 0, len(e.Fields))
		for _, f := range e.Fields {
			fields = append(fields, schema.Field{Name: f.Name, Type: f.Type})
		}
		byName[e.Name] = len(out)
		out = append(out, schema.Entity{Name: e.Name, Fields: fields})
	}
	for _, r := range ir.Relations {
		idx, ok := byName[r.FromEntity]
		if !ok {
			continue
		}
		out[idx].Relations = append(out[idx].Relations, schema.Relation{
			Type:       r.Type,
			Target:     r.ToEntity,
			ForeignKey: r.FromField,
		})
	}
	return out
}
