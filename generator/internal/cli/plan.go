package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gen-system/generator/internal/agent"
	"gen-system/generator/internal/planner"
	planner_v2 "gen-system/generator/internal/planner_v2"
)

const defaultPlanFile = "plan.generated.json"

// Plan handles `gen plan [--v2] "goal"`.
//
// Without --v2: V1 pipeline — GeneratePlan() → GenerateSchemaFromPlan() → tasks
// With --v2:    V2 pipeline — features → architecture → entities → tasks (real AI)
//
// Both write plan.generated.json compatible with `gen run --plan`.
func Plan() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: gen plan [--v2] \"your goal\"")
		fmt.Println("  --v2    Multi-step reasoning: features → architecture → entities → tasks")
		return
	}

	useV2 := false
	var goalParts []string
	for _, arg := range os.Args[2:] {
		if arg == "--v2" {
			useV2 = true
		} else {
			goalParts = append(goalParts, arg)
		}
	}
	goal := strings.Join(goalParts, " ")
	if goal == "" {
		fmt.Println("❌ Goal cannot be empty")
		return
	}

	var plan planner.Plan
	var err error

	if useV2 {
		fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("🧠 Planner V2 — multi-step reasoning\n")
		fmt.Printf("   Goal: %s\n", goal)
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
		plan, err = planner_v2.BuildPlanV2(goal)
	} else {
		fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("🧠 Planner V1 — standard pipeline\n")
		fmt.Printf("   Goal: %s\n", goal)
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
		plan, err = planner.BuildPlan(goal)
	}

	if err != nil {
		fmt.Printf("❌ Plan generation failed: %s\n", err)
		return
	}

	// Plan is a review artifact, not generated source — but still honour --out
	// so it doesn't pollute the cwd/source. Default keeps prior behaviour (cwd).
	planOut := defaultPlanFile
	if d := GetFlag("out", ""); d != "" {
		if looksLikeGenSourceTree(d) {
			fmt.Printf("⛔ Refusing to write plan into the gen-system source tree (%s).\n", d)
			return
		}
		os.MkdirAll(d, 0o755)
		planOut = filepath.Join(d, defaultPlanFile)
	}

	if err := planner.SavePlan(plan, planOut); err != nil {
		fmt.Printf("❌ Failed to save plan: %s\n", err)
		return
	}

	v := "V1"
	if useV2 {
		v = "V2"
	}

	fmt.Printf("\n✅ Plan [%s] saved → %s\n", v, planOut)
	fmt.Printf("   Entities: %d\n", len(plan.Entities))
	fmt.Printf("   Tasks:    %d\n", len(plan.Tasks))
	if len(plan.Notes) > 0 {
		fmt.Printf("   Notes:    %s\n", plan.Notes[0])
	}
	fmt.Printf("\nReview %s, then:\n  gen run --plan %s --out <dir>\n", planOut, planOut)
}

// RunPlan handles `gen run --plan <file> [--out <dir>] [--force]`.
// --path is accepted as an alias for --out (back-compat). The output dir is
// resolved through the same safety policy as idea/apply: never the source tree,
// and no overwrite of an existing generated app without confirmation.
func RunPlan() {
	planFile := defaultPlanFile

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--plan":
			if i+1 < len(args) {
				i++
				planFile = args[i]
			}
		}
	}

	plan, err := planner.LoadPlan(planFile)
	if err != nil {
		fmt.Printf("❌ Failed to load plan: %s\n", err)
		return
	}

	// --out preferred; --path accepted as a legacy alias.
	outFlag := GetFlag("out", "")
	if outFlag == "" {
		outFlag = GetFlag("path", "")
	}
	outDir, ok := ResolveOutDir(OutDirOptions{
		Flag:        outFlag,
		DefaultName: "output",
		Command:     "run",
		Force:       HasFlag("force"),
		AssumeYes:   HasFlag("yes"),
	})
	if !ok {
		return
	}

	fmt.Printf("🚀 Running plan: %s\n", planFile)
	fmt.Printf("   Goal:     %s\n", plan.Goal)
	fmt.Printf("   Entities: %d\n", len(plan.Entities))
	fmt.Printf("   Tasks:    %d\n", len(plan.Tasks))
	if len(plan.Notes) > 0 {
		fmt.Printf("   Notes:    %s\n", plan.Notes[0])
	}
	fmt.Println()

	// v25.5: activate the JSONL agent trace if --heal-log was passed.
	defer SetupHealLog()()
	SetupVerifyUnderstand() // v25.5 step 2: Understand-validates-Generate mode
	SetupStyle()            // v26.33: team style profile (--style / GEN_STYLE)
	if err := agent.RunFromPlan(plan, outDir); err != nil {
		fmt.Printf("❌ Execution failed: %s\n", err)
	}
}
