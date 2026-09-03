// generator/internal/bench/genpipe.go — v26.24 (WP1): the end-to-end Generate
// pipeline benchmark. Runs idea → plan → generate → compile gate → heal →
// Understand-verify for each task idea, through the SAME production entry
// points the CLI uses (planner_v2.BuildPlanV2 + agent.RunFromPlan), and
// reports the quality axis that can actually move:
//
//	ops_total / ops_stubbed      — how much model logic survived the compile gate
//	op_repair_attempts/successes — op-level repair round-trips (cap 2, see
//	                               engine.maxRepairAttempts)
//	heal_attempts_total/success  — task-level FixTask attempts (cap 3)
//	go_test_pass                 — independent `go test ./...` on each backend
//	verify_findings              — Understand-verify findings on the output
//	wall_s / prompt_tokens       — cost per idea set
//
// compile_pass stays reported (backend_builds) but is NOT the headline: the
// stub path makes it green by construction; the stub and repair counts are
// where quantization or model changes show up.
//
// Provider selection is untouched: GEN_LOGIC=ollama measures the live model,
// default is the deterministic replay provider (offline, used by the unit
// test). The web gate is disabled for the duration of the suite (GEN_WEB_GATE=0,
// restored after) so the measurement spends its wall clock on the model and the
// Go gates, not on npm; the web gate has its own suite (web_gate).
package bench

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gen-system/generator/internal/agent"
	"gen-system/generator/internal/ai"
	"gen-system/generator/internal/engine"
	"gen-system/generator/internal/planner"
	planner_v2 "gen-system/generator/internal/planner_v2"
	"gen-system/generator/internal/schema"
)

// BuiltinGenIdeas is the frozen 5-idea task set (mirrors bench/tasks.json;
// tasks.json is the committed source of record for on-box runs, this copy keeps
// the suite runnable with no flags).
var BuiltinGenIdeas = []string{
	"A small library lending service: members borrow and return books, librarians add books and see overdue loans",
	"An inventory service for a hardware store: products with stock counts, suppliers, and purchase orders that restock products",
	"A gym membership service: members, plans and checkins; staff can freeze or cancel memberships",
	"A support ticket tracker: customers open tickets, agents reply to tickets and close them, tags for triage",
	"An event registration service: events with capacity, attendees register and cancel, a waitlist promotes automatically",
}

// GenPipeline runs the full Generate pipeline once per idea and aggregates the
// WP1 metrics. One Case per idea (pass = plan ok + agent ok + backend builds +
// go test passes) plus the aggregate metrics.
func GenPipeline(ideas []string) Suite {
	t0 := time.Now()
	s := Suite{Name: "gen_pipeline"}
	if len(ideas) == 0 {
		ideas = BuiltinGenIdeas
	}

	// Web gate off for the suite (measured separately by web_gate).
	prevWebGate, hadWebGate := os.LookupEnv("GEN_WEB_GATE")
	os.Setenv("GEN_WEB_GATE", "0")
	defer func() {
		if hadWebGate {
			os.Setenv("GEN_WEB_GATE", prevWebGate)
		} else {
			os.Unsetenv("GEN_WEB_GATE")
		}
	}()

	// Understand-verify in report-only mode; restore whatever the caller had.
	prevVerify := agent.VerifyUnderstandMode
	agent.VerifyUnderstandMode = agent.VerifyStop
	defer func() { agent.VerifyUnderstandMode = prevVerify }()

	// Count heal attempts via the existing observer hook, chaining any observer
	// already installed.
	var healAttempts, healSuccesses int
	prevObs := agent.HealObserver
	agent.HealObserver = func(ev agent.HealEvent) {
		if prevObs != nil {
			prevObs(ev)
		}
		switch ev.Phase {
		case "start":
			healAttempts++
		case "success":
			healSuccesses++
		}
	}
	defer func() { agent.HealObserver = prevObs }()

	var (
		opsTotal, opsStubbed      int
		repairAttempts, repairOK  int
		entitiesTotal             int
		verifyFindings            int
		buildsPassed, testsPassed int
		promptTokens, evalTokens  int
		modelRequests             int
		plannerFallbacks          int
		wallTotal                 time.Duration
	)

	for i, idea := range ideas {
		dir, err := os.MkdirTemp("", "genbench-pipe-*")
		if err != nil {
			s.Cases = append(s.Cases, Case{Name: ideaCaseName(i, idea), Pass: false, Note: "mktemp: " + err.Error()})
			continue
		}

		ai.ResetUsage()
		engine.ResetLogicStats()
		healBefore, healOKBefore := healAttempts, healSuccesses

		ideaStart := time.Now()
		plan, planErr := planner_v2.BuildPlanV2(idea)
		planFellBack := false
		if planErr == nil && len(plan.Entities) == 0 {
			// The V2 planner's entity step has no deterministic fallback: with
			// no reachable model it returns zero entities. Take the same path
			// `gen apply` takes — a fixed schema through TaskGraphFromSchema —
			// so the offline (replay) benchmark still exercises the full
			// generate→gate→verify pipeline. NEVER silent: counted in the
			// planner_fallbacks metric and named in the case note, so a live
			// sweep where the model died mid-run cannot masquerade as measured
			// planning.
			planFellBack = true
			plannerFallbacks++
			plan = fallbackPlan(idea)
		}
		var runErr error
		if planErr == nil {
			runErr = agent.RunFromPlan(plan, dir)
		}
		wall := time.Since(ideaStart)
		wallTotal += wall

		backend := filepath.Join(dir, "backend")
		buildOK := dirCmdOK(backend, "go", "build", "./...")
		testOK := buildOK && dirCmdOK(backend, "go", "test", "./...")

		ls := engine.LogicStatsSnapshot()
		u := ai.UsageSnapshot()
		rep := agent.LastVerifyReport

		opsTotal += ls.OpsTotal
		opsStubbed += ls.OpsStubbed
		repairAttempts += ls.RepairAttempts
		repairOK += ls.RepairSuccesses
		ideaEntities := 0
		if planErr == nil {
			ideaEntities = len(plan.Entities)
		}
		entitiesTotal += ideaEntities
		verifyFindings += len(rep.Findings)
		if buildOK {
			buildsPassed++
		}
		if testOK {
			testsPassed++
		}
		promptTokens += u.PromptTokens
		evalTokens += u.EvalTokens
		modelRequests += u.Requests

		pass := planErr == nil && runErr == nil && buildOK && testOK
		got := fmt.Sprintf(
			"ops=%d stubbed=%d repairs=%d/%d heals=%d/%d entities=%d findings=%d build=%v test=%v wall=%.0fs",
			ls.OpsTotal, ls.OpsStubbed, ls.RepairSuccesses, ls.RepairAttempts,
			healSuccesses-healOKBefore, healAttempts-healBefore,
			ideaEntities, len(rep.Findings), buildOK, testOK, wall.Seconds())
		note := ""
		if planErr != nil {
			note = "plan: " + planErr.Error()
		} else if runErr != nil {
			note = "agent: " + runErr.Error()
		}
		if planFellBack {
			note = strings.TrimSpace(note + " [planner offline fallback: fixed Order/Product schema]")
		}
		s.Cases = append(s.Cases, Case{
			Name: ideaCaseName(i, idea), Pass: pass,
			Got: got, Want: "0 stubs, build+test green", Note: note,
		})

		os.RemoveAll(dir)
	}

	n := float64(len(ideas))
	stubRate := 0.0
	if opsTotal > 0 {
		stubRate = 100 * float64(opsStubbed) / float64(opsTotal)
	}
	s.Metrics = append(s.Metrics,
		Metric{Name: "ideas_total", Value: n, Unit: "ideas"},
		Metric{Name: "ops_total", Value: float64(opsTotal), Unit: "ops", HigherIsBetter: true},
		Metric{Name: "ops_stubbed", Value: float64(opsStubbed), Unit: "ops"},
		Metric{Name: "stub_rate_pct", Value: stubRate, Unit: "%"},
		Metric{Name: "op_repair_attempts", Value: float64(repairAttempts), Unit: "attempts"},
		Metric{Name: "op_repair_successes", Value: float64(repairOK), Unit: "attempts", HigherIsBetter: true},
		Metric{Name: "heal_attempts_total", Value: float64(healAttempts), Unit: "attempts"},
		Metric{Name: "heal_success", Value: float64(healSuccesses), Unit: "attempts", HigherIsBetter: true},
		Metric{Name: "entities_total", Value: float64(entitiesTotal), Unit: "entities"},
		Metric{Name: "planner_fallbacks", Value: float64(plannerFallbacks), Unit: "ideas"},
		Metric{Name: "verify_findings", Value: float64(verifyFindings), Unit: "findings"},
		Metric{Name: "backend_builds", Value: float64(buildsPassed), Unit: "ideas", HigherIsBetter: true},
		Metric{Name: "go_test_pass", Value: float64(testsPassed), Unit: "ideas", HigherIsBetter: true},
		Metric{Name: "wall_s_total", Value: wallTotal.Seconds(), Unit: "s"},
		Metric{Name: "prompt_tokens", Value: float64(promptTokens), Unit: "tokens"},
		Metric{Name: "eval_tokens", Value: float64(evalTokens), Unit: "tokens"},
		Metric{Name: "model_requests", Value: float64(modelRequests), Unit: "requests"},
	)
	s.Duration = time.Since(t0)
	return s
}

func ideaCaseName(i int, idea string) string {
	words := strings.Fields(idea)
	if len(words) > 4 {
		words = words[:4]
	}
	return fmt.Sprintf("idea_%d: %s…", i+1, strings.Join(words, " "))
}

// fallbackPlan is the deterministic plan used when the planner had no model to
// talk to: the same fixed schema shape `gen apply` would take, with entities
// matching the replay fixtures so the offline path exercises real op bodies.
func fallbackPlan(idea string) planner.Plan {
	s := schema.SchemaFile{Entities: []schema.Entity{
		{Name: "Order", Fields: []schema.Field{
			{Name: "total", Type: "float"},
			{Name: "status", Type: "string"},
		}},
		{Name: "Product", Fields: []schema.Field{
			{Name: "name", Type: "string"},
			{Name: "stock", Type: "int"},
		}},
	}}
	return planner.Plan{
		Goal:     idea,
		Entities: s.Entities,
		Tasks:    planner.TaskGraphFromSchema(s),
	}
}

// dirCmdOK runs a command in dir and reports success. A missing dir is a
// failure (the pipeline was expected to create it).
func dirCmdOK(dir, name string, args ...string) bool {
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.Run() == nil
}
