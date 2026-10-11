// generator/internal/bench/effects.go (v19.7): precision/recall on inferred
// side-effects against a hand-labelled ground-truth file.
//
// The labels file is JSON of the form:
//
//	{
//	  "labels": [
//	    {"file": "generator/internal/symbols/index.go",
//	     "symbol": "Save",
//	     "kind": "method",                 // optional ("func"|"method"|"procedure")
//	     "receiver": "Index",              // required if kind=="method"
//	     "effects": ["fs"]                 // any of: db, fs, network, pure
//	    }
//	  ]
//	}
//
// Precision = correct labels predicted / all labels predicted
// Recall    = correct labels predicted / all labels in ground truth
//
// Both are computed per-effect-kind across all labelled symbols. We treat
// "unknown" as an explicit non-prediction (neither TP nor FP).
package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

type EffectLabel struct {
	File     string   `json:"file"`
	Symbol   string   `json:"symbol"`
	Kind     string   `json:"kind"`
	Receiver string   `json:"receiver"`
	Effects  []string `json:"effects"`
}

type labelsFile struct {
	Labels []EffectLabel `json:"labels"`
}

// EffectPrecisionRecall loads ground-truth labels from labelsPath (relative
// to root, or absolute) and scores ctx.Effects against them.
func EffectPrecisionRecall(root string, copybookPath []string, labelsPath string) Suite {
	t0 := time.Now()
	suite := Suite{Name: "effect_precision_recall"}

	data, err := os.ReadFile(labelsPath)
	if err != nil {
		suite.Cases = []Case{{Name: "read_labels", Pass: false, Note: err.Error()}}
		suite.Duration = time.Since(t0)
		return suite
	}
	var lf labelsFile
	if err := json.Unmarshal(data, &lf); err != nil {
		suite.Cases = []Case{{Name: "parse_labels", Pass: false, Note: err.Error()}}
		suite.Duration = time.Since(t0)
		return suite
	}
	if len(lf.Labels) == 0 {
		suite.Cases = []Case{{Name: "labels_non_empty", Pass: false, Note: "no labels in file"}}
		suite.Duration = time.Since(t0)
		return suite
	}

	opts := agentctx.LoadContextOpts{CopybookPath: copybookPath}
	ctx := agentctx.LoadContextWithOpts(root, opts)
	if ctx.Effects == nil {
		suite.Cases = []Case{{Name: "have_effects", Pass: false, Note: "ctx.Effects is nil"}}
		suite.Duration = time.Since(t0)
		return suite
	}

	// Per-kind tallies.
	type tally struct{ tp, fp, fn int }
	stats := map[flow.EffectKind]*tally{
		flow.EffectDB:      {},
		flow.EffectFS:      {},
		flow.EffectNetwork: {},
		flow.EffectPure:    {},
	}
	matched, unmatched := 0, 0
	var unmatchedExamples []string

	for _, lab := range lf.Labels {
		kind := symbols.SymFunc
		switch lab.Kind {
		case "method":
			kind = symbols.SymMethod
		case "procedure":
			kind = symbols.SymProcedure
		}
		// Labels carry a relative path (e.g. "generator/internal/symbols/index.go")
		// but ctx.Effects is keyed by whatever path the walker produced
		// (absolute when --root is absolute, relative otherwise). Match by
		// suffix on the file portion of the SymbolID so labels stay
		// portable across invocations.
		var qualName string
		if kind == symbols.SymMethod {
			qualName = lab.Receiver + "." + lab.Symbol
		} else {
			qualName = lab.Symbol
		}
		want := "#" + string(kind) + ":" + qualName
		fileSep := string(os.PathSeparator)
		labFile := lab.File
		if fileSep != "/" {
			// Normalise both sides to forward-slash for matching: labels
			// files are written portably with forward slashes, but
			// filepath.Walk on Windows uses backslashes.
			labFile = strings.ReplaceAll(labFile, "/", fileSep)
		}
		var efx *flow.FuncEffects
		for id, candidate := range ctx.Effects {
			if !strings.HasSuffix(id, want) {
				continue
			}
			// Strip the "#kind:qualName" portion to get the file portion.
			filePortion := id[:len(id)-len(want)]
			if filePortion == labFile || strings.HasSuffix(filePortion, fileSep+labFile) {
				efx = candidate
				break
			}
		}
		if efx == nil {
			unmatched++
			if len(unmatchedExamples) < 5 {
				unmatchedExamples = append(unmatchedExamples, lab.File+want)
			}
			continue
		}
		matched++

		// Build set views.
		truth := map[flow.EffectKind]bool{}
		for _, e := range lab.Effects {
			truth[flow.EffectKind(e)] = true
		}
		predicted := efx.Effects

		// Score the four real effect kinds (skip Unknown, because it's an
		// abstention, not a prediction).
		for k := range stats {
			t := truth[k]
			p := predicted[k]
			switch {
			case t && p:
				stats[k].tp++
			case !t && p:
				stats[k].fp++
			case t && !p:
				stats[k].fn++
			}
		}
	}

	suite.Cases = append(suite.Cases, Case{
		Name: "label_match_rate",
		Pass: matched > 0 && float64(matched)/float64(matched+unmatched) >= 0.9,
		Got:  fmt.Sprintf("%d matched, %d unmatched", matched, unmatched),
		Want: ">=90% of labels matched to a symbol",
	})

	// Stable kind ordering for deterministic output.
	kinds := []flow.EffectKind{flow.EffectDB, flow.EffectFS, flow.EffectNetwork, flow.EffectPure}
	for _, k := range kinds {
		s := stats[k]
		prec := 0.0
		if s.tp+s.fp > 0 {
			prec = float64(s.tp) / float64(s.tp+s.fp)
		}
		rec := 0.0
		if s.tp+s.fn > 0 {
			rec = float64(s.tp) / float64(s.tp+s.fn)
		}
		suite.Metrics = append(suite.Metrics,
			Metric{Name: fmt.Sprintf("precision_%s", k), Value: prec, Unit: "ratio", HigherIsBetter: true},
			Metric{Name: fmt.Sprintf("recall_%s", k), Value: rec, Unit: "ratio", HigherIsBetter: true},
			Metric{Name: fmt.Sprintf("tp_%s", k), Value: float64(s.tp), Unit: "count", HigherIsBetter: true},
			Metric{Name: fmt.Sprintf("fp_%s", k), Value: float64(s.fp), Unit: "count"},
			Metric{Name: fmt.Sprintf("fn_%s", k), Value: float64(s.fn), Unit: "count"},
		)
		// Bench passes when we have at least 1 prediction in this kind
		// and precision/recall both >= 0.75. With 50+ labels these
		// thresholds are tight but achievable; with very small label
		// sets they're informative noise.
		nonempty := s.tp+s.fp+s.fn > 0
		suite.Cases = append(suite.Cases, Case{
			Name: fmt.Sprintf("%s_p_and_r_above_0.75", k),
			Pass: !nonempty || (prec >= 0.75 && rec >= 0.75),
			Got:  fmt.Sprintf("p=%.2f r=%.2f", prec, rec),
			Want: "p>=0.75 r>=0.75 (or no data)",
		})
	}

	sort.Strings(unmatchedExamples)
	if len(unmatchedExamples) > 0 {
		suite.Notes = "unmatched label IDs (first 5): "
		for _, ex := range unmatchedExamples {
			suite.Notes += ex + " "
		}
	}
	suite.Duration = time.Since(t0)
	return suite
}
