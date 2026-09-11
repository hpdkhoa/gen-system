package cli

import (
	"fmt"
	"os"

	"github.com/hpdkhoa/gen-system/generator/internal/agent"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

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
	_ = inPlace

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
		fmt.Println("⚠️ IR compile warning:", err, "— continuing")
	}

	// Build incremental change set
	_, buildPlan, _ := compiler.CompileV3(oldIR, newIR)

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

	plan := planner.Plan{
		Goal:     "evolve",
		Entities: filteredEntities,
		Tasks:    tasks,
	}

	fmt.Printf("🧬 Running evolution engine (%d entities, %d tasks)...\n",
		len(filteredEntities), len(tasks))

	// v25.5: activate the JSONL agent trace if --heal-log was passed.
	defer SetupHealLog()()
	SetupVerifyUnderstand() // v25.5 step 2: Understand-validates-Generate mode
	SetupStyle()            // v26.33: team style profile (--style / GEN_STYLE)
	SetupStdlib()           // v26.34: --stdlib / GEN_STDLIB, default auto
	if err := agent.RunFromPlan(plan, writeDir); err != nil {
		fmt.Println("❌ Evolution failed:", err)
		return
	}

	// Save updated IR
	if err := compiler.SaveIR(newIR); err != nil {
		fmt.Println("⚠️ Could not save IR snapshot:", err)
	}

	fmt.Println("✅ Evolution complete")
}
