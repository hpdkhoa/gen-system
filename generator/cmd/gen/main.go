// gen/main.go — v23.30 (gen report): adds the `report` command (render a
// standalone HTML validation report from genbench JSON + optional .skill).
// Prior: v23.24 (task 4A): adds the `continue` command (continue from an
// existing codebase via a natural-language idea). Prior: v23.11 (Phase 3):
// `evolve` honours --understand, routing to
// cli.EvolveWithUnderstanding (loads existing code via the understanding
// frontends and widens evolution by code-level impact). Plain `evolve` is
// unchanged. TASK 4 (v19): added 'skill' subcommand for SkillFile emission.
package main

import (
	"fmt"
	"os"

	"gen-system/generator/internal/cli"
	"gen-system/generator/internal/config"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	// v23.21: load runtime config from a .env file BEFORE anything reads the
	// environment. --env <path> picks a specific file; otherwise GEN_ENV=<name>
	// selects .env.<name>, defaulting to ./.env. Already-exported real env vars
	// always win over the file, so an operator/CI can override without editing.
	config.MustLoad(envFlag())

	switch os.Args[1] {

	// ── PROJECT LIFECYCLE ─────────────────────────────────────
	case "init":
		cli.Init()

	case "apply":
		if len(os.Args) < 3 {
			fmt.Println("Usage: gen apply <schema.json>")
			return
		}
		cli.Apply(os.Args[2])

	case "evolve":
		if cli.HasUnderstandFlag() {
			cli.EvolveWithUnderstanding()
		} else {
			cli.Evolve()
		}

	// ── CONTINUE FROM EXISTING CODE (v23.24, task 4A) ──────────
	// gen continue <path> "<idea>"
	//   → understand existing code → plan a delta → stage (or --apply)
	case "continue":
		cli.Continue()

	// ── PLANNER + AGENT (NEW v2) ───────────────────────────────
	// gen plan "build a blog with posts and comments"
	//   → generates plan.generated.json for review
	case "plan":
		cli.Plan()

	// gen run --plan plan.generated.json [--path ./myapp]
	//   → executes a reviewed plan via the agent loop
	case "run":
		cli.RunPlan()

	// ── SINGLE-STEP IDEA (skips review) ────────────────────────
	// gen idea "..."
	//   → plan → schema → generate (no pause for review)
	case "idea":
		cli.Idea()

	// ── SCHEMA / DEPS UTILITIES ───────────────────────────────
	case "schema":
		cli.Schema()

	case "deps":
		cli.Deps()

	// ── MIGRATIONS ────────────────────────────────────────────
	case "migrate":
		if len(os.Args) < 3 {
			fmt.Println("Usage: gen migrate <schema.json> [--dry] [--force]")
			return
		}
		cli.RunMigrate(os.Args[2:])

	// ── DIAGNOSTICS ───────────────────────────────────────────
	case "ingest":
		cli.Ingest()

	case "finetune":
		cli.Finetune()

	case "doctor":
		cli.Doctor()

	// ── SKILL FILE (v19 TASK 4) ───────────────────────────────
	// gen skill [--root .] [--out .gen/project.skill] [--constraints constraints.txt]
	//   → writes a versioned .skill file capturing the current symbol graph.
	case "skill":
		cli.WriteSkill(os.Args[2:])

	// gen report --in <genbench.json> [--in ...] [--skill f] [--out report.html]
	//   → render a standalone dark-terminal HTML validation report from
	//     frozen genbench runs (+ optional .skill export). Pure renderer.
	case "report":
		cli.WriteReport(os.Args[2:])

	// gen style print | derive <repo> [--out f] | check <root> [--profile f] [--json]
	//   → v26.33: team style profiles. A profile is used on a run with
	//     --style <file> [--style-mode off|report|gate|autofix].
	case "style":
		cli.Style(os.Args[2:])

	default:
		fmt.Println("Unknown command:", os.Args[1])
		printUsage()
	}
}

func printUsage() {
	fmt.Println("")
	fmt.Println("gen — AI-powered code evolution engine")
	fmt.Println("")
	fmt.Println("Usage: gen <command> [args]")
	fmt.Println("")
	fmt.Println("Output safety (applies to all generating commands):")
	fmt.Println("  --out <dir>   Where generated code goes. NEVER defaults to the source")
	fmt.Println("                tree or the bare cwd; generation into the gen-system source")
	fmt.Println("                is refused. New output lands in a clear, reviewable folder.")
	fmt.Println("  --force/--yes Overwrite an existing generated app without the prompt.")
	fmt.Println("")
	fmt.Println("Planning (with review):")
	fmt.Println("  plan <goal text>             Generate plan for review → plan.generated.json")
	fmt.Println("  run [--plan f] [--out dir]   Execute a reviewed plan via agent loop")
	fmt.Println("")
	fmt.Println("Full pipeline (no review pause):")
	fmt.Println("  idea [--v2] [--out dir] <goal text>   Idea → plan → schema → generate")
	fmt.Println("                                        (default out: ./generated-app)")
	fmt.Println("")
	fmt.Println("Schema-based:")
	fmt.Println("  init                         Initialize project directories")
	fmt.Println("  apply <schema.json> [--out dir]  Generate full stack (default out: ./output)")
	fmt.Println("  evolve <path> --schema f     Evolve existing project. By default STAGES into")
	fmt.Println("         [--apply]             <path>.evolve-review for inspection; --apply writes")
	fmt.Println("         [--review-dir d]      in place (with confirmation). Source is never")
	fmt.Println("         [--understand]        touched without --apply.")
	fmt.Println("  schema <plan.json>           Convert plan JSON → schema JSON")
	fmt.Println("")
	fmt.Println("Database:")
	fmt.Println("  migrate <schema.json> [--dry] [--force]  Apply DB migrations")
	fmt.Println("")
	fmt.Println("Intelligence:")
	fmt.Println("  skill [--root .] [--out .gen/project.skill] [--constraints file]")
	fmt.Println("                               Write versioned .skill context file")
	fmt.Println("  report --in <genbench.json> [--in ...] [--skill f] [--out report.html]")
	fmt.Println("                               Render a standalone HTML validation report")
	fmt.Println("  style print | derive <repo> [--out f] | check <root> [--profile f] [--json]")
	fmt.Println("                               Team style profiles (v26.33). On a run:")
	fmt.Println("                               --style gen-style.json [--style-mode off|report|gate|autofix]")
	fmt.Println("")
	fmt.Println("Utilities:")
	fmt.Println("  deps <path> <profile>        Write dependency files (stable | latest)")
	fmt.Println("  doctor                       Check project health")
	fmt.Println("")
	fmt.Println("Note: gen-server --root/--copybooks/--browse-root are READ-ONLY (Understand")
	fmt.Println("      tab indexing); they never write generated code, so they are safe.")
	fmt.Println("")
}

// envFlag returns the value of --env <path> (or --env=<path>) if present, so a
// caller can select a specific config file: `gen idea --env .env.staging ...`.
func envFlag() string {
	for i, a := range os.Args {
		if a == "--env" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if len(a) > 6 && a[:6] == "--env=" {
			return a[6:]
		}
	}
	return ""
}
