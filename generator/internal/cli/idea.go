// generator/internal/cli/idea.go — v23.24 (task 4A): `gen idea --from <path>`
// now aliases to the continue-from-existing-code path (ContinueViaIdeaFrom).
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/agent"
	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	planner_v2 "github.com/hpdkhoa/gen-system/generator/internal/planner_v2"
)

// IdeaFromInput runs goal → plan → execute without review pause.
// The --v2 flag selects the multi-step reasoning pipeline.
// outDir is the resolved, safe output directory (never the source tree); plan
// and schema artifacts are written there too, keeping the source clean.
func IdeaFromInput(input string, useV2 bool, outDir string) {
	if input == "" {
		fmt.Println("❌ Idea is empty")
		return
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("💡 Idea:", input)
	if useV2 {
		fmt.Println("🧠 Planner V2 — multi-step reasoning (no review pause)")
	} else {
		fmt.Println("🧠 Planner V1 — standard pipeline (no review pause)")
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	var plan planner.Plan
	var err error

	if useV2 {
		plan, err = planner_v2.BuildPlanV2(input)
	} else {
		plan, err = planner.BuildPlan(input)
	}

	if err != nil {
		fmt.Println("❌ Plan generation failed:", err)
		return
	}

	// Save plan + schema INTO the output dir for reference/review (not cwd).
	planFile := filepath.Join(outDir, "plan.generated.json")
	_ = planner.SavePlan(plan, planFile)

	schemaBytes, _ := json.MarshalIndent(map[string]interface{}{
		"entities": plan.Entities,
	}, "", "  ")
	_ = os.WriteFile(filepath.Join(outDir, "schema.generated.json"), schemaBytes, 0644)

	v := "V1"
	if useV2 {
		v = "V2"
	}

	fmt.Printf("\n✅ Plan [%s]: %d entities, %d tasks\n",
		v, len(plan.Entities), len(plan.Tasks))
	fmt.Printf("📄 Saved: %s, %s\n\n", planFile, filepath.Join(outDir, "schema.generated.json"))
	fmt.Println("🚀 Starting agent execution...")

	// v25.5: activate the JSONL agent trace if --heal-log was passed.
	defer SetupHealLog()()
	SetupVerifyUnderstand() // v25.5 step 2: Understand-validates-Generate mode
	SetupStyle()            // v26.33: team style profile (--style / GEN_STYLE)
	SetupStdlib()           // v26.34: --stdlib / GEN_STDLIB, default auto

	if err := agent.RunFromPlan(plan, outDir); err != nil {
		fmt.Println("\n❌ Agent run completed with errors:", err)
		fmt.Printf("   Review %s, edit if needed, then: gen run --plan %s --out %s\n",
			planFile, planFile, outDir)
		return
	}

	fmt.Printf("\n✅ Done — generated into %s\n", outDir)
	fmt.Printf("   Review it there, then run it:  cd %s/backend && go run .\n", outDir)
}

// Idea handles `gen idea [--v2] [--out <dir>] [--force] [--yes] "goal"`.
func Idea() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: gen idea [--v2] [--out <dir>] [--force] \"your idea\"")
		fmt.Println("  --v2          Use multi-step reasoning planner")
		fmt.Println("  --out <dir>   Output directory (default ./generated-app; never the source tree)")
		fmt.Println("  --from <path> Continue an EXISTING app at <path> (alias for: gen continue)")
		fmt.Println("  --force/--yes Overwrite an existing generated app without prompting")
		fmt.Println("Tip:   gen plan [--v2] \"your idea\"  (to review plan before running)")
		return
	}

	useV2 := false
	var parts []string
	skipNext := false
	for i, arg := range os.Args[2:] {
		if skipNext {
			skipNext = false
			continue
		}
		switch {
		case arg == "--v2":
			useV2 = true
		case arg == "--from":
			skipNext = true // value consumed via GetFlag("from")
			_ = i
		case strings.HasPrefix(arg, "--from="):
			// value embedded; consumed via GetFlag("from")
		case arg == "--out" || arg == "--env":
			skipNext = true // flag value consumed elsewhere (GetFlag/envFlag)
			_ = i
		case strings.HasPrefix(arg, "--out=") || strings.HasPrefix(arg, "--env="):
			// value embedded; nothing to add to idea text
		case arg == "--force" || arg == "--yes":
			// consumed by HasFlag
		default:
			parts = append(parts, arg)
		}
	}

	// `gen idea --from <path> "<idea>"` is an alias for `gen continue`.
	if from := GetFlag("from", ""); from != "" {
		ContinueViaIdeaFrom(from, parts)
		return
	}

	outDir, ok := ResolveOutDir(OutDirOptions{
		Flag:        GetFlag("out", ""),
		DefaultName: "generated-app",
		Command:     "idea",
		Force:       HasFlag("force"),
		AssumeYes:   HasFlag("yes"),
	})
	if !ok {
		return
	}

	IdeaFromInput(strings.Join(parts, " "), useV2, outDir)
}
