package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"gen-system/generator/internal/agent"
	"gen-system/generator/internal/planner"
	"gen-system/generator/internal/schema"
)

// Apply loads a schema JSON and runs the full generation pipeline
// via the agent/task-graph path (same execution engine as 'gen run').
// Output goes to --out (default ./output); never the source tree, and an
// existing generated app is not overwritten without confirmation.
func Apply(schemaFile string) {
	s, err := schema.LoadFromFile(schemaFile)
	if err != nil {
		fmt.Println("❌ Failed to load schema:", err)
		return
	}

	if len(s.Entities) == 0 {
		fmt.Println("❌ Schema has no entities")
		return
	}

	outDir, ok := ResolveOutDir(OutDirOptions{
		Flag:        GetFlag("out", ""),
		DefaultName: "output",
		Command:     "apply",
		Force:       HasFlag("force"),
		AssumeYes:   HasFlag("yes"),
	})
	if !ok {
		return
	}
	os.MkdirAll(filepath.Join(outDir, ".gen"), 0755)

	fmt.Printf("📦 Applying schema: %d entities\n", len(s.Entities))

	// Build a plan directly from the schema (bypass AI — schema is already known)
	tasks := planner.TaskGraphFromSchema(s)
	plan := planner.Plan{
		Goal:     "apply:" + schemaFile,
		Entities: s.Entities,
		Tasks:    tasks,
	}

	// v25.5: activate the JSONL agent trace if --heal-log was passed.
	defer SetupHealLog()()
	SetupVerifyUnderstand() // v25.5 step 2: Understand-validates-Generate mode
	SetupStyle()            // v26.33: team style profile (--style / GEN_STYLE)
	if err := agent.RunFromPlan(plan, outDir); err != nil {
		fmt.Println("❌ Apply failed:", err)
		return
	}

	fmt.Printf("✅ Apply complete → %s\n", outDir)
}
