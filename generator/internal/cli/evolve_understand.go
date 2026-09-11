// generator/internal/cli/evolve_understand.go — v23.11 (Phase 3): the
// Understand→Generate loop. `gen evolve <path> --schema f --understand` first
// reads the EXISTING generated code through gen-system's own compiler
// frontends (context.LoadContext → resolved symbol index + call graph + CFGs),
// then uses that structural knowledge to widen evolution: any entity whose
// code is in the IMPACT SET of a schema-changed entity is also regenerated, so
// evolution is informed by what already exists instead of blindly diffing
// schema-to-schema.
//
// This is purely additive: plain `gen evolve` (Evolve in evolve.go) is
// unchanged. The understand path is opt-in via the --understand flag and falls
// back to the normal evolve behaviour if the context load yields nothing
// (e.g. an empty or non-Go project), so it can never make evolution worse.
package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/agent"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// hasUnderstandFlag reports whether --understand was passed.
func HasUnderstandFlag() bool {
	for _, a := range os.Args {
		if a == "--understand" {
			return true
		}
	}
	return false
}

// EvolveWithUnderstanding is the entry point for `gen evolve ... --understand`.
// It mirrors Evolve() but inserts an understanding pass between "load schema"
// and "run the agent".
func EvolveWithUnderstanding() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: gen evolve <repo-path> --schema schema.json --understand")
		return
	}
	path := os.Args[2]
	schemaPath := GetSchemaPath()
	if schemaPath == "" {
		fmt.Println("❌ Missing --schema")
		return
	}

	// Safety policy: stage into a review dir by default; --apply to write in place.
	writeDir, _, ok := ResolveEvolveTarget(path)
	if !ok {
		return
	}

	fmt.Println("📂 Loading schema:", schemaPath)
	sfile, err := schema.LoadFromFile(schemaPath)
	if err != nil {
		fmt.Println("❌ Failed to load schema:", err)
		return
	}

	// ── 1. UNDERSTAND: read the existing code at compiler accuracy ──
	fmt.Println("🔬 Understanding existing code at", path, "…")
	ctx := context.LoadContextWithOpts(path, loadOpts()) // v26.34: stdlib auto detected
	resolvedPct, nSyms, nEdges := resolutionStats(ctx.Index)
	fmt.Printf("   resolved %.1f%% of %d call edges across %d symbols\n",
		resolvedPct, nEdges, nSyms)

	// ── 2. schema diff → directly-changed entities (as plain evolve) ──
	oldIR, _ := compiler.LoadPreviousIR()
	newIR, err := compiler.Compile(sfile)
	if err != nil {
		fmt.Println("⚠️ IR compile warning:", err, "— continuing")
	}
	_, buildPlan, _ := compiler.CompileV3(oldIR, newIR)

	changedSet := map[string]bool{}
	for _, name := range buildPlan.Entities {
		changedSet[name] = true
	}
	for _, name := range buildPlan.Changed {
		changedSet[name] = true
	}

	// ── 3. WIDEN by impact: anything that structurally depends on a ──
	//      changed entity must be regenerated too. This is the step that
	//      only the understanding pass can provide — schema-to-schema diff
	//      alone cannot see code-level dependencies.
	directly := sortedKeys(changedSet)
	impacted := impactedEntities(&ctx, sfile.Entities, changedSet)
	added := 0
	for name := range impacted {
		if !changedSet[name] {
			changedSet[name] = true
			added++
		}
	}
	if added > 0 {
		fmt.Printf("   impact analysis: %d directly-changed entit%s → +%d dependent regenerated\n",
			len(directly), plural1(len(directly)), added)
		fmt.Printf("   (dependents: %s)\n", strings.Join(sortedKeys(setDiff(changedSet, changedSetOf(directly))), ", "))
	} else if ctx.Index != nil {
		fmt.Println("   impact analysis: no additional dependents found")
	}

	// ── 4. build the task graph for the widened set & run ───────────
	tasks := planner.TaskGraphDynamic(sfile, writeDir, changedSet)
	if len(tasks) == 0 {
		fmt.Println("✅ No changes detected — nothing to evolve")
		return
	}

	processedNames := map[string]bool{}
	for _, t := range tasks {
		if t.EntityName != "" {
			processedNames[t.EntityName] = true
		}
	}
	var filtered []schema.Entity
	for _, e := range sfile.Entities {
		if processedNames[e.Name] {
			filtered = append(filtered, e)
		} else {
			fmt.Println("⏭  Skipping unchanged entity:", e.Name)
		}
	}

	plan := planner.Plan{Goal: "evolve", Entities: filtered, Tasks: tasks}
	fmt.Printf("🧬 Running evolution (%d entities, %d tasks, understanding-informed)…\n",
		len(filtered), len(tasks))

	// v25.5: activate the JSONL agent trace if --heal-log was passed.
	defer SetupHealLog()()
	SetupVerifyUnderstand() // v25.5 step 2: Understand-validates-Generate mode
	SetupStyle()            // v26.33: team style profile (--style / GEN_STYLE)
	SetupStdlib()           // v26.34: --stdlib / GEN_STDLIB, default auto
	if err := agent.RunFromPlan(plan, writeDir); err != nil {
		fmt.Println("❌ Evolution failed:", err)
		return
	}
	if err := compiler.SaveIR(newIR); err != nil {
		fmt.Println("⚠️ Could not save IR snapshot:", err)
	}
	fmt.Println("✅ Evolution complete (understanding-informed)")
}

// ─────────────────────────────────────────────────────────────
// understanding helpers
// ─────────────────────────────────────────────────────────────

// resolutionStats computes the call-edge resolution percentage from a loaded
// symbol index — the headline "how well do we understand this code" number.
func resolutionStats(idx *symbols.Index) (pct float64, nSyms, nEdges int) {
	if idx == nil {
		return 0, 0, 0
	}
	syms := idx.AllSymbols()
	edges := idx.AllCallEdges()
	nSyms = len(syms)
	nEdges = len(edges)
	if nEdges == 0 {
		return 100.0, nSyms, 0 // nothing to resolve = fully understood
	}
	resolved := 0
	for _, e := range edges {
		if strings.TrimSpace(e.CalleeID) != "" {
			resolved++
		}
	}
	return float64(resolved) * 100.0 / float64(nEdges), nSyms, nEdges
}

// impactedEntities returns the set of entity names whose generated code is in
// the impact set of any directly-changed entity. It maps each entity to the
// symbols that reference it, then walks the index ImpactSet, and finally maps
// affected files back to entity names.
func impactedEntities(ctx *context.Context, entities []schema.Entity, changed map[string]bool) map[string]bool {
	out := map[string]bool{}
	if ctx.Index == nil {
		return out
	}
	// Build a quick file→entity association from filenames (the generator
	// names files after entities: model/<entity>.go, handler/<Entity>_*.go).
	entityNames := make([]string, 0, len(entities))
	for _, e := range entities {
		entityNames = append(entityNames, e.Name)
	}

	for _, e := range entities {
		if !changed[e.Name] {
			continue
		}
		// Files that reference this changed entity, via the understanding pass.
		for _, rel := range ctx.FilesReferencingEntity(e.Name) {
			for _, other := range entityNames {
				if other == e.Name {
					continue
				}
				if fileBelongsToEntity(rel, other) {
					out[other] = true
				}
			}
		}
		// Also use symbol-level impact: any symbol that calls into this
		// entity's symbols. FindReferences returns file paths referencing a
		// name; map those back to entities too.
		for _, rel := range ctx.FindReferences(e.Name) {
			for _, other := range entityNames {
				if other != e.Name && fileBelongsToEntity(rel, other) {
					out[other] = true
				}
			}
		}
	}
	return out
}

// fileBelongsToEntity reports whether a generated file path is one of the
// files generated for the given entity (case-insensitive match on the base).
func fileBelongsToEntity(relPath, entity string) bool {
	low := strings.ToLower(relPath)
	e := strings.ToLower(entity)
	// model/<entity>.go, store/<entity>.go, handler/<Entity>_handler.go, etc.
	return strings.Contains(low, "/"+e+".go") ||
		strings.Contains(low, "/"+e+"_") ||
		strings.Contains(low, "/"+e+"s.go")
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func changedSetOf(names []string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

// setDiff returns a−b. (v23.12: renamed from diff to avoid collision with the
// compiler/diff package imported elsewhere in package cli — pre-existing v23.11 bug.)
func setDiff(a, b map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		if !b[k] {
			out[k] = true
		}
	}
	return out
}

func plural1(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
