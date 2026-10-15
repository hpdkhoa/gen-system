// regression_exact_v2669_test.go (v26.69): the COBOL baseline gate allowed 5%
// drift and only in the "worse" direction, so 200 call edges could vanish
// without a failure. RegressionExact fails on any change and on a lost metric.
package bench

import "testing"

func runWith(metrics ...Metric) Run {
	return Run{Suites: []Suite{{Name: "s", Metrics: metrics}}}
}

func TestRegressionExact(t *testing.T) {
	base := runWith(Metric{Name: "cfgs_total", Value: 463}, Metric{Name: "recall", Value: 1, HigherIsBetter: true})
	cases := []struct {
		name string
		new  Run
		ok   bool
	}{
		{"same", runWith(Metric{Name: "cfgs_total", Value: 463}, Metric{Name: "recall", Value: 1, HigherIsBetter: true}), true},
		{"count down", runWith(Metric{Name: "cfgs_total", Value: 462}, Metric{Name: "recall", Value: 1, HigherIsBetter: true}), false},
		{"count up", runWith(Metric{Name: "cfgs_total", Value: 464}, Metric{Name: "recall", Value: 1, HigherIsBetter: true}), false},
		{"metric missing", runWith(Metric{Name: "cfgs_total", Value: 463}), false},
	}
	for _, c := range cases {
		if _, ok := RegressionExact(base, c.new); ok != c.ok {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.ok)
		}
	}
	// The tolerant gate still lets a 0.2% drop through, which is why the
	// baseline uses the exact one.
	if _, ok := Regression(base, runWith(Metric{Name: "cfgs_total", Value: 462}, Metric{Name: "recall", Value: 1, HigherIsBetter: true}), 0.05); !ok {
		t.Error("Regression with 5% tolerance failed on a 0.2% change")
	}
}
