// generator/internal/bench/resolution_stdlib.go — v23.32 (stdlib provenance):
// NEW. ResolutionRecallWithStdlib loads the project WITH one or more stdlib
// roots ingested read-only (ProvStdlib), then reports TWO separate numbers:
//
//	resolution_recall_internal       — UNCHANGED meaning: of calls to the
//	                                    project's OWN symbols, how many resolved.
//	                                    Stdlib symbols are excluded from this
//	                                    denominator by PROVENANCE, not by name,
//	                                    so the headline number stays honest even
//	                                    though the stdlib is now in the index.
//	resolution_recall_with_stdlib     — NEW: of all calls that land on a symbol
//	                                    we hold (project OR stdlib), how many
//	                                    resolved. This is the "we resolve into
//	                                    the runtime" number.
//
// Keeping the internal metric provenance-gated is the whole point: merging the
// stdlib into the index would otherwise silently redefine "internal" and make
// the 100%-internal claim meaningless. ResolutionRecall (resolution.go) is left
// exactly as-is for callers that don't pass stdlib roots.
package bench

import (
	"fmt"
	"sort"
	"strings"
	"time"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// ResolutionRecallWithStdlib is ResolutionRecall plus read-only stdlib ingest.
// stdlibRoots is the list of standard-library trees to parse (e.g. GOROOT/src).
// When stdlibRoots is empty it behaves like ResolutionRecall but additionally
// emits the with-stdlib metric (which will equal internal recall, since no
// stdlib symbols exist to resolve into).
func ResolutionRecallWithStdlib(root string, copybookPath []string, stdlibRoots []agentctx.StdlibRoot, passThreshold float64) Suite {
	if passThreshold == 0 {
		passThreshold = 0.90
	}
	t0 := time.Now()
	suite := Suite{Name: "resolution_recall_stdlib"}

	opts := agentctx.LoadContextOpts{
		CopybookPath: copybookPath,
		StdlibRoots:  stdlibRoots,
	}
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

	// Partition the symbol set by provenance so "project" and "stdlib" names
	// are distinguishable even when a stdlib symbol shares a bare name with a
	// project symbol. A name is "project" if ANY project symbol has it; a name
	// is "stdlib-only" if it is held solely by stdlib symbols.
	projectNames := map[string]struct{}{}
	stdlibNames := map[string]struct{}{}
	var stdlibSymCount, projectSymCount int
	for _, s := range ctx.Index.AllSymbols() {
		if s.Provenance.IsExternal() {
			stdlibNames[s.Name] = struct{}{}
			stdlibSymCount++
		} else {
			projectNames[s.Name] = struct{}{}
			projectSymCount++
		}
	}

	var (
		internal         int // call to a project-owned name
		resolvedInternal int // ...that resolved to a project symbol
		stdlibResolvable int // call to a name held by project OR stdlib
		resolvedAny      int // resolved to a symbol we hold (project or stdlib)
		intoStdlib       int // resolved specifically into a stdlib symbol
		external         int // name we hold nowhere
	)

	for _, e := range edges {
		// Only edges whose CALLER is a project symbol count toward project
		// metrics. Without this gate, stdlib→stdlib calls (which flood in once
		// the stdlib is ingested) would inflate every denominator and silently
		// redefine "internal recall". This is the single most important line
		// in the file: it is what keeps the project number identical whether
		// or not a stdlib was ingested.
		callerProv := symbols.ProvProject
		if caller, ok := ctx.Index.ByID(e.CallerID); ok && caller != nil {
			callerProv = caller.Provenance
		}
		if callerProv.IsExternal() {
			continue
		}

		bare := e.CalleeName
		if dot := strings.LastIndex(bare, "."); dot >= 0 {
			bare = bare[dot+1:]
		}

		_, isProject := projectNames[bare]
		_, isStdlibName := stdlibNames[bare]

		if isProject {
			internal++
		}
		if isProject || isStdlibName {
			stdlibResolvable++
		} else {
			external++
		}

		if e.CalleeID != "" {
			// Where did it resolve TO?
			calleeProv := symbols.ProvProject
			if callee, ok := ctx.Index.ByID(e.CalleeID); ok && callee != nil {
				calleeProv = callee.Provenance
			}
			if calleeProv.IsExternal() {
				intoStdlib++
				resolvedAny++
			} else {
				resolvedAny++
				if isProject {
					resolvedInternal++
				}
			}
		}
	}

	internalRate := 0.0
	if internal > 0 {
		internalRate = float64(resolvedInternal) / float64(internal)
	}
	withStdlibRate := 0.0
	if stdlibResolvable > 0 {
		withStdlibRate = float64(resolvedAny) / float64(stdlibResolvable)
	}

	// Honest internal recall — project symbols only, unchanged definition.
	suite.Cases = append(suite.Cases, Case{
		Name: fmt.Sprintf("internal_recall_at_least_%.0f%%", passThreshold*100),
		Pass: internal == 0 || internalRate >= passThreshold,
		Got:  fmt.Sprintf("%.2f%% (%d/%d project calls resolved)", internalRate*100, resolvedInternal, internal),
		Want: fmt.Sprintf(">=%.2f%%", passThreshold*100),
	})
	// New: resolution INCLUDING the stdlib boundary.
	suite.Cases = append(suite.Cases, Case{
		Name: "stdlib_ingested",
		Pass: stdlibSymCount > 0,
		Got:  fmt.Sprintf("%d stdlib symbols in index", stdlibSymCount),
		Want: ">0 (when --stdlib supplied)",
	})

	suite.Metrics = append(suite.Metrics,
		Metric{Name: "resolution_recall_internal", Value: internalRate, Unit: "ratio", HigherIsBetter: true},
		Metric{Name: "resolution_recall_with_stdlib", Value: withStdlibRate, Unit: "ratio", HigherIsBetter: true},
		Metric{Name: "call_edges_total", Value: float64(total), Unit: "edges"},
		Metric{Name: "call_edges_into_stdlib", Value: float64(intoStdlib), Unit: "edges", HigherIsBetter: true},
		Metric{Name: "call_edges_external_unheld", Value: float64(external), Unit: "edges"},
		Metric{Name: "symbols_project", Value: float64(projectSymCount), Unit: "symbols"},
		Metric{Name: "symbols_stdlib", Value: float64(stdlibSymCount), Unit: "symbols"},
	)

	// Per-language internal recall (project only), stable order.
	type langStat struct{ internal, resolved int }
	byLang := map[string]*langStat{}
	for _, e := range edges {
		caller, ok := ctx.Index.ByID(e.CallerID)
		if !ok || caller == nil || caller.Provenance.IsExternal() {
			continue // project callers only
		}
		bare := e.CalleeName
		if dot := strings.LastIndex(bare, "."); dot >= 0 {
			bare = bare[dot+1:]
		}
		if _, ok := projectNames[bare]; !ok {
			continue
		}
		lang := string(caller.Language)
		if lang == "" {
			lang = "unknown"
		}
		st := byLang[lang]
		if st == nil {
			st = &langStat{}
			byLang[lang] = st
		}
		st.internal++
		if e.CalleeID != "" {
			if callee, ok := ctx.Index.ByID(e.CalleeID); ok && callee != nil && !callee.Provenance.IsExternal() {
				st.resolved++
			}
		}
	}
	langs := make([]string, 0, len(byLang))
	for k := range byLang {
		langs = append(langs, k)
	}
	sort.Strings(langs)
	for _, lang := range langs {
		st := byLang[lang]
		if st.internal == 0 {
			continue
		}
		r := float64(st.resolved) / float64(st.internal)
		suite.Metrics = append(suite.Metrics, Metric{
			Name: "recall_internal_" + lang, Value: r, Unit: "ratio", HigherIsBetter: true,
		})
	}

	suite.Notes = fmt.Sprintf("project_syms=%d stdlib_syms=%d total_edges=%d internal=%d into_stdlib=%d unheld=%d",
		projectSymCount, stdlibSymCount, total, internal, intoStdlib, external)
	suite.Duration = time.Since(t0)
	return suite
}
