// cli/report_test.go, v23.31 (gen report test fix): the v23.30 test referred
// to a non-existent buildModelFromRuns and an invalid []struct{} conversion,
// so the package failed to compile under `go test`. Rewired to the real
// buildModel([]bench.Run, ...) signature; behaviour assertions unchanged.
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/bench"
)

// baselinePath locates the committed frozen baseline relative to this package.
func baselinePath(t *testing.T) string {
	t.Helper()
	// cli/ is generator/internal/cli; baseline is generator/internal/bench/baselines.
	p := filepath.Join("..", "bench", "baselines", "cobolcraft_v23.24.json")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("baseline not found at %s: %v", p, err)
	}
	return p
}

func TestReport_RendersFrozenBaseline(t *testing.T) {
	run, err := loadRun(baselinePath(t))
	if err != nil {
		t.Fatalf("loadRun: %v", err)
	}
	if run.Schema != "genbench/v1" {
		t.Fatalf("schema = %q, want genbench/v1", run.Schema)
	}

	m := buildModel([]bench.Run{run}, nil, "")
	_ = m
}

func TestReport_Model(t *testing.T) {
	run, err := loadRun(baselinePath(t))
	if err != nil {
		t.Fatalf("loadRun: %v", err)
	}
	m := buildModel([]bench.Run{run}, nil, "")

	// The baseline is all-green: 9 pass / 0 fail across 3 suites.
	if m.TotalFail != 0 {
		t.Errorf("TotalFail = %d, want 0", m.TotalFail)
	}
	if m.SuiteCount != 3 {
		t.Errorf("SuiteCount = %d, want 3", m.SuiteCount)
	}
	if m.TotalPass != 9 {
		t.Errorf("TotalPass = %d, want 9", m.TotalPass)
	}

	// Recall highlight must surface the exact got-string from the data,
	// never a fabricated number.
	var sawRecall bool
	for _, h := range m.Highlights {
		if h.Label == "Internal-call recall" {
			sawRecall = true
			if !strings.Contains(h.Value, "302/302") {
				t.Errorf("recall highlight = %q, want substring 302/302", h.Value)
			}
		}
	}
	if !sawRecall {
		t.Error("expected an Internal-call recall highlight")
	}

	// Render must produce well-formed HTML with no unresolved template actions.
	html, err := renderReport(m)
	if err != nil {
		t.Fatalf("renderReport: %v", err)
	}
	if strings.Contains(html, "{{") || strings.Contains(html, "}}") {
		t.Error("rendered HTML contains unresolved template delimiters")
	}
	if !strings.Contains(html, "302/302") {
		t.Error("rendered HTML missing the recall evidence string")
	}
	if !strings.Contains(html, "cfg_correctness") {
		t.Error("rendered HTML missing a suite name from the data")
	}
	// html/template must have escaped the >= in the want column.
	if strings.Contains(html, ">=90.00%</td>") {
		t.Error("expected html/template to escape '>=' in want column")
	}
}

func TestFormatMetric(t *testing.T) {
	cases := map[float64]string{
		268:                 "268",
		0:                   "0",
		1:                   "1",
		0.715898400752587:   "0.7159",
		0.28410159924741296: "0.2841",
	}
	for in, want := range cases {
		if got := formatMetric(in); got != want {
			t.Errorf("formatMetric(%v) = %q, want %q", in, got, want)
		}
	}
}
