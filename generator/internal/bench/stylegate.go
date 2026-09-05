// generator/internal/bench/stylegate.go — v26.33: the team style gate as a
// benchmark suite. One case per active rule (pass when that rule has no
// finding) plus counts, so a profile can be run over any Go tree and the
// result diffed like every other suite. Deterministic, no model.
package bench

import (
	"fmt"
	"time"

	"gen-system/generator/internal/style"
)

// StyleGate runs p against root.
func StyleGate(root string, p *style.Profile) Suite {
	t0 := time.Now()
	s := Suite{Name: "style_gate"}
	rep := style.Check(root, p)
	perRule := map[string]int{}
	perRuleErr := map[string]int{}
	for _, f := range rep.Findings {
		perRule[f.Rule]++
		if f.Severity == style.SevError {
			perRuleErr[f.Rule]++
		}
	}
	for _, rule := range rep.RulesChecked {
		s.Cases = append(s.Cases, Case{
			Name: rule, Pass: perRule[rule] == 0,
			Got: fmt.Sprintf("%d finding(s), %d error", perRule[rule], perRuleErr[rule]), Want: "0",
		})
	}
	s.Metrics = append(s.Metrics,
		Metric{Name: "style_rules_checked", Value: float64(len(rep.RulesChecked)), Unit: "rules"},
		Metric{Name: "style_findings", Value: float64(len(rep.Findings)), Unit: "findings"},
		Metric{Name: "style_errors", Value: float64(rep.Errors()), Unit: "findings"},
		Metric{Name: "style_warnings", Value: float64(rep.Warnings()), Unit: "findings"},
		Metric{Name: "style_files", Value: float64(rep.Files), Unit: "files"},
		Metric{Name: "style_functions", Value: float64(rep.Functions), Unit: "functions"},
	)
	s.Notes = "profile " + rep.Profile
	s.Duration = time.Since(t0)
	return s
}
