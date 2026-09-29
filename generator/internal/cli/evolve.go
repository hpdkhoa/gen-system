package cli

import (
	"fmt"
	"os"

	"github.com/hpdkhoa/gen-system/generator/internal/agent"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// evolveRunFromPlan runs the agent for gen evolve. v26.58: a variable so the
// tests of D037, D044 and D071 can see the plan without a model call.
var evolveRunFromPlan = agent.RunFromPlan

func Evolve() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: gen evolve <repo-path> --schema schema.json")
		return
	}

	path := os.Args[2]
	schemaPath := GetSchemaPath()

	if schemaPath == "" {
		fmt.Println("❌ Missing --schema")
		return
	}

	// Safety policy: evolve stages into a review dir by default; --apply writes
	// in place (guarded). writeDir is where generation actually goes.
	writeDir, inPlace, ok := ResolveEvolveTarget(path)
	if !ok {
		return
	}

	fmt.Println("📂 Loading schema:", schemaPath)

	sfile, err := schema.LoadFromFile(schemaPath)
	if err != nil {
		fmt.Println("❌ Failed to load schema:", err)
		return
	}

	// Load previous IR for evolution-aware task graph
	oldIR, _ := compiler.LoadPreviousIR()
	newIR, err := compiler.Compile(sfile)
	if err != nil {
		// v26.58 (D044): Evolve went on with an empty IR, so the diff saw no
		// change (or the wrong one) and the agent could prune the files of
		// schema entities. A schema that does not compile cannot be evolved.
		fmt.Println("❌ IR compile error:", err)
		return
	}

	// Build incremental change set
	cs, buildPlan, _ := compiler.CompileV3(oldIR, newIR)
	// v26.58: a field that left the schema stays in the code, with a warning
	// (evolve_keep.go). The plan and the snapshot are rebuilt from the schema
	// with the kept fields.
	if keptIR, err := keepFieldsAndReplan(&sfile, oldIR, newIR, cs); err != nil {
		fmt.Println("❌ IR compile error:", err)
		return
	} else if len(keptIR.Entities) > 0 {
		newIR = keptIR
		_, buildPlan, _ = compiler.CompileV3(oldIR, newIR)
	}

	changedSet := map[string]bool{}
	for _, name := range buildPlan.Entities {
		changedSet[name] = true
	}
	for _, name := range buildPlan.Changed {
		changedSet[name] = true
	}

	// ✅ Use DYNAMIC task graph — only generates tasks for changed entities,
	// and only adds migrate tasks for entities that are actually new/changed.
	tasks := planner.TaskGraphDynamic(sfile, writeDir, changedSet)

	if len(tasks) == 0 {
		fmt.Println("✅ No changes detected — nothing to evolve")
		return
	}

	// Determine which entities will actually be processed
	var filteredEntities []schema.Entity
	processedNames := map[string]bool{}
	for _, t := range tasks {
		if t.EntityName != "" {
			processedNames[t.EntityName] = true
		}
	}
	for _, e := range sfile.Entities {
		if processedNames[e.Name] {
			filteredEntities = append(filteredEntities, e)
		} else {
			fmt.Println("⏭  Skipping unchanged entity:", e.Name)
		}
	}

	// v26.58 (D044): plan.Entities held only the processed entities, and the
	// agent prunes the files of every entity outside plan.Entities, so the
	// files of an unchanged schema entity were deleted and main.go lost its
	// routes. The tasks already limit the work; the entity set must be the
	// full schema, as BuildContinuePlan does.
	plan := planner.Plan{
		Goal:     "evolve",
		Entities: sfile.Entities,
		Tasks:    tasks,
	}

	fmt.Printf("🧬 Running evolution engine (%d entities, %d tasks)...\n",
		len(filteredEntities), len(tasks))

	// v25.5: activate the JSONL agent trace if --heal-log was passed.
	defer SetupHealLog()()
	SetupVerifyUnderstand() // v25.5 step 2: Understand-validates-Generate mode
	SetupStyle()            // v26.33: team style profile (--style / GEN_STYLE)
	SetupStdlib()           // v26.34: --stdlib / GEN_STDLIB, default auto
	if err := evolveRunFromPlan(plan, writeDir); err != nil {
		fmt.Println("❌ Evolution failed:", err)
		return
	}

	// Save updated IR
	saveEvolveSnapshot(newIR, inPlace)

	fmt.Println("✅ Evolution complete")
}

// saveEvolveSnapshot saves the IR snapshot after an evolve run that wrote in
// place. v26.58 (D037): a staged run saved it too, although the source was
// untouched, so a later gen migrate or gen evolve --apply in the same working
// directory saw no change and skipped the SQL and the code for it. The
// snapshot must describe the source, so a staged run leaves it alone.
func saveEvolveSnapshot(newIR compiler.SchemaIR, inPlace bool) {
	if !inPlace {
		fmt.Println("   IR snapshot not updated: the source is untouched until --apply")
		return
	}
	if err := compiler.SaveIR(newIR); err != nil {
		fmt.Println("⚠️ Could not save IR snapshot:", err)
	}
}
