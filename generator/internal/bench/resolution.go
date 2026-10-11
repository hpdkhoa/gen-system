// generator/internal/bench/resolution.go (v19.9): redefine the headline
// metric. The v19.7 version counted "any call edge", which means stdlib
// calls (fmt.Println, http.ListenAndServe, etc.) sat in the denominator
// even though they can't resolve: our index intentionally does not
// contain external-package symbols. That gave a misleading 45% ceiling
// on a real Go codebase where the engine is doing its job.
//
// The fix splits the population:
//
//	internal edges  = edges whose bare CalleeName matches a symbol name
//	                  present in the project index
//	external edges  = everything else (stdlib, third-party, dynamic)
//
//	resolution_recall_internal = resolved / internal
//	external_call_ratio        = external / total
//	resolution_recall_overall  = resolved / total   (kept for continuity)
//
// The internal metric is what the engine can actually be evaluated on.
// It's the number a reader of the benchmark cares about: "of the calls
// you could resolve in this repo, how many did you?"
//
// Note on cache staleness: if you applied v19.8 with `unzip -o` and then
// `go build`, Go's content-addressed build cache may have served the old
// compiled package (mtime semantics on unzip can leave files older than
// the cached object). If diag.edges_with_calleeid is 0 but the engine
// should have resolved, do `rm -rf $(go env GOCACHE) && go build` and
// re-run.
package bench

import (
	"fmt"
	"sort"
	"time"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// ResolutionRecall loads the project at root, computes resolution rates,
// and asserts the "internal" rate (calls to symbols the index knows about)
// is above passThreshold. Default 0.90 if zero is passed.
func ResolutionRecall(root string, copybookPath []string, passThreshold float64) Suite {
	if passThreshold == 0 {
		passThreshold = 0.90
	}
	t0 := time.Now()
	suite := Suite{Name: "resolution_recall", Duration: time.Since(t0)}

	opts := agentctx.LoadContextOpts{CopybookPath: copybookPath}
	ctx := agentctx.LoadContextWithOpts(root, opts)

	if ctx.Index == nil {
		suite.Cases = []Case{{Name: "load_context", Pass: false, Note: "ctx.Index is nil"}}
		suite.Duration = time.Since(t0)
		return suite
	}

	edges := ctx.Index.AllCallEdges()
	total := len(edges)
	if total == 0 {
		suite.Cases = []Case{{Name: "has_call_edges", Pass: false, Note: "no call edges in index"}}
		suite.Duration = time.Since(t0)
		return suite
	}

	// v26.34: internal versus external is decided by symbols.EdgeClassifier,
	// the same rule the Understand verify pass uses. An edge is internal when
	// it resolved to a project symbol, or when it is unresolved, a project
	// symbol carries its bare name, and no import rules it out. Function
	// literals called in place are neither. The old rule matched the bare name
	// against every symbol including stdlib ones, so a stdlib ingest changed
	// the population it was supposed to leave alone.
	classifier := ctx.Index.NewEdgeClassifier()

	type langStat struct {
		total    int
		internal int
		resolved int // resolved and internal (the cell that earns recall)
	}
	byLang := map[string]*langStat{}

	internal := 0
	resolved := 0         // resolved overall (CalleeID set)
	resolvedInternal := 0 // resolved and internal
	external := 0         // bare name not in knownNames

	literal := 0
	for _, e := range edges {
		class, toProject := classifier.Classify(e)
		isInternal := class == symbols.EdgeInternal
		switch class {
		case symbols.EdgeInternal:
			internal++
		case symbols.EdgeLiteral:
			literal++
		default:
			external++
		}

		isResolved := e.CalleeID != ""
		if isResolved {
			resolved++
			if toProject {
				resolvedInternal++
			}
		}

		// Bucket by caller language.
		lang := "unknown"
		if callerSym, ok := ctx.Index.ByID(e.CallerID); ok && callerSym != nil {
			lang = string(callerSym.Language)
		}
		st, exists := byLang[lang]
		if !exists {
			st = &langStat{}
			byLang[lang] = st
		}
		st.total++
		if isInternal {
			st.internal++
			if isResolved {
				st.resolved++
			}
		}
	}

	overallRate := float64(resolved) / float64(total)
	internalRate := 0.0
	if internal > 0 {
		internalRate = float64(resolvedInternal) / float64(internal)
	}
	externalRatio := float64(external) / float64(total)

	suite.Cases = append(suite.Cases, Case{
		Name: fmt.Sprintf("internal_recall_at_least_%.0f%%", passThreshold*100),
		Pass: internal == 0 || internalRate >= passThreshold,
		Got:  fmt.Sprintf("%.2f%% (%d/%d internal calls resolved)", internalRate*100, resolvedInternal, internal),
		Want: fmt.Sprintf(">=%.2f%%", passThreshold*100),
	})
	suite.Metrics = append(suite.Metrics,
		Metric{Name: "resolution_recall_internal", Value: internalRate, Unit: "ratio", HigherIsBetter: true},
		Metric{Name: "resolution_recall_overall", Value: overallRate, Unit: "ratio", HigherIsBetter: true},
		Metric{Name: "external_call_ratio", Value: externalRatio, Unit: "ratio"},
		Metric{Name: "call_edges_total", Value: float64(total), Unit: "edges", HigherIsBetter: true},
		Metric{Name: "call_edges_internal", Value: float64(internal), Unit: "edges", HigherIsBetter: true},
		Metric{Name: "call_edges_resolved", Value: float64(resolved), Unit: "edges", HigherIsBetter: true},
		Metric{Name: "call_edges_external", Value: float64(external), Unit: "edges"},
		Metric{Name: "call_edges_literal", Value: float64(literal), Unit: "edges"},
	)

	// Stable per-language breakdown: recall on the internal slice.
	langs := make([]string, 0, len(byLang))
	for k := range byLang {
		langs = append(langs, k)
	}
	sort.Strings(langs)
	for _, lang := range langs {
		st := byLang[lang]
		if st.internal == 0 {
			// Lang has no internal calls, so there is nothing to score. Skip the case.
			suite.Metrics = append(suite.Metrics, Metric{
				Name: "recall_internal_" + lang, Value: 0, Unit: "ratio", HigherIsBetter: true,
			})
			continue
		}
		r := float64(st.resolved) / float64(st.internal)
		suite.Metrics = append(suite.Metrics, Metric{
			Name: "recall_internal_" + lang, Value: r, Unit: "ratio", HigherIsBetter: true,
		})
		suite.Cases = append(suite.Cases, Case{
			Name: "internal_recall_" + lang + "_at_least_85%",
			Pass: r >= 0.85,
			Got:  fmt.Sprintf("%.2f%% (%d/%d)", r*100, st.resolved, st.internal),
			Want: ">=85%",
		})
	}

	// Spot-check: nothing in the index should have a SymbolID with empty filepath.
	bad := 0
	for _, s := range ctx.Index.AllSymbols() {
		if s.DefinedIn == "" {
			bad++
		}
	}
	suite.Cases = append(suite.Cases, Case{
		Name: "symbols_have_definedin",
		Pass: bad == 0,
		Got:  fmt.Sprintf("%d symbols with empty DefinedIn", bad),
		Want: "0",
	})

	_ = symbols.SymFunc

	suite.Notes = fmt.Sprintf("symbols=%d total=%d internal=%d resolved=%d external=%d",
		len(ctx.Index.AllSymbols()), total, internal, resolved, external)
	suite.Duration = time.Since(t0)
	return suite
}
