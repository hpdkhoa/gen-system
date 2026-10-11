// genpipe_test.go, v26.24 (WP1): the gen_pipeline suite must run fully
// offline (replay provider, planner fallbacks) and emit every WP1 metric.
// This is the test that proves the new quality axis is wired to the real
// pipeline, not to a bench-only shim. Skipped under -short: it generates and
// compiles a real backend in a temp dir (tens of seconds with the toolchain).
package bench

import (
	"os"
	"testing"
)

func TestGenPipelineOfflineEmitsWP1Metrics(t *testing.T) {
	if testing.Short() {
		t.Skip("generates+compiles a backend; skipped under -short")
	}
	// Force the offline replay provider and a dead model endpoint so planner
	// fallbacks engage deterministically and quickly.
	os.Unsetenv("GEN_LOGIC")
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:1")
	t.Setenv("OLLAMA_TIMEOUT", "1")

	s := GenPipeline([]string{
		"A small shop where customers place orders for products",
	})

	if s.Name != "gen_pipeline" {
		t.Fatalf("suite name = %q", s.Name)
	}
	if len(s.Cases) != 1 {
		t.Fatalf("want 1 case (1 idea), got %d", len(s.Cases))
	}

	want := []string{
		"ideas_total", "ops_total", "ops_stubbed", "stub_rate_pct",
		"op_repair_attempts", "op_repair_successes",
		"heal_attempts_total", "heal_success",
		"entities_total", "verify_findings",
		"backend_builds", "go_test_pass",
		"wall_s_total", "prompt_tokens", "eval_tokens", "model_requests",
	}
	have := map[string]float64{}
	for _, m := range s.Metrics {
		have[m.Name] = m.Value
	}
	for _, name := range want {
		if _, ok := have[name]; !ok {
			t.Errorf("metric %q missing from gen_pipeline suite", name)
		}
	}

	// The offline path must produce a building backend for the fixture-backed
	// idea; if this fails the case's Note explains why.
	if have["backend_builds"] < 1 {
		t.Errorf("offline pipeline did not produce a building backend: %+v", s.Cases[0])
	}
	// Replay fixtures compile as-is: nothing should be stubbed offline.
	if have["ops_stubbed"] != 0 {
		t.Errorf("offline replay run stubbed %v ops; fixtures should compile", have["ops_stubbed"])
	}
}
