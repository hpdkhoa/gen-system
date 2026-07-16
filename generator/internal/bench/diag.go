// generator/internal/bench/diag.go — v19.8b: resolution diagnostic.
// Not a pass/fail scenario. Prints the raw state needed to understand
// why resolution recall reports 0%:
//
//   - exact counts: total edges, edges with empty CalleeID, edges with set CalleeID
//   - first 10 unresolved edges, each showing CallerID + CalleeName
//   - for each unresolved edge: whether a symbol with that bare name exists
//     in the index at all (after stripping at the last '.')
//   - first 10 symbol names actually in the index
//
// This is the bench you run when "0% resolved" doesn't match expectations
// and you need to find out whether the bug is in the resolver, in the
// frontend's Callee extraction, or somewhere else.
package bench

import (
	"fmt"
	"sort"
	"strings"
	"time"

	agentctx "gen-system/generator/internal/context"
	"gen-system/generator/internal/symbols"
)

func ResolutionDiagnostic(root string, copybookPath []string, limit int) Suite {
	if limit <= 0 {
		limit = 10
	}
	t0 := time.Now()
	suite := Suite{Name: "resolution_diagnostic"}

	opts := agentctx.LoadContextOpts{CopybookPath: copybookPath}
	ctx := agentctx.LoadContextWithOpts(root, opts)
	if ctx.Index == nil {
		suite.Cases = []Case{{Name: "load_context", Pass: false, Note: "nil Index"}}
		suite.Duration = time.Since(t0)
		return suite
	}

	edges := ctx.Index.AllCallEdges()
	allSyms := ctx.Index.AllSymbols()

	// Build a name→count map of every symbol in the index.
	nameCount := map[string]int{}
	for _, s := range allSyms {
		nameCount[s.Name]++
	}

	totalEdges := len(edges)
	withCalleeID := 0
	withCalleeName := 0
	for _, e := range edges {
		if e.CalleeID != "" {
			withCalleeID++
		}
		if e.CalleeName != "" {
			withCalleeName++
		}
	}

	suite.Metrics = []Metric{
		{Name: "edges_total", Value: float64(totalEdges), Unit: "edges"},
		{Name: "edges_with_calleeid", Value: float64(withCalleeID), Unit: "edges", HigherIsBetter: true},
		{Name: "edges_with_calleename", Value: float64(withCalleeName), Unit: "edges", HigherIsBetter: true},
		{Name: "symbols_total", Value: float64(len(allSyms)), Unit: "symbols"},
		{Name: "distinct_symbol_names", Value: float64(len(nameCount)), Unit: "names"},
	}

	suite.Cases = append(suite.Cases, Case{
		Name: "calleename_populated",
		Pass: withCalleeName == totalEdges && totalEdges > 0,
		Got:  fmt.Sprintf("%d of %d edges have CalleeName set", withCalleeName, totalEdges),
		Want: "every edge has a CalleeName",
	})

	// Sample first `limit` UNRESOLVED edges and check whether the bare
	// name (after stripping at last '.') exists in the symbol index.
	type sample struct {
		callerID    string
		calleeName  string
		bareName    string
		nameInIndex int
	}
	var samples []sample
	for _, e := range edges {
		if e.CalleeID != "" {
			continue
		}
		bare := e.CalleeName
		if dot := strings.LastIndex(bare, "."); dot >= 0 {
			bare = bare[dot+1:]
		}
		samples = append(samples, sample{
			callerID:    e.CallerID,
			calleeName:  e.CalleeName,
			bareName:    bare,
			nameInIndex: nameCount[bare],
		})
		if len(samples) >= limit {
			break
		}
	}

	wouldResolve := 0
	for _, s := range samples {
		if s.nameInIndex > 0 {
			wouldResolve++
		}
	}
	suite.Cases = append(suite.Cases, Case{
		Name: fmt.Sprintf("first_%d_unresolved_match_a_symbol_in_index", limit),
		Pass: wouldResolve == len(samples) && len(samples) > 0,
		Got:  fmt.Sprintf("%d of first %d unresolved samples have a name match in index", wouldResolve, len(samples)),
		Want: "every sampled unresolved edge has its bare name present in idx.allSymbols",
	})

	// Print samples and a few representative symbol names in Notes.
	var sb strings.Builder
	sb.WriteString("unresolved samples: ")
	for i, s := range samples {
		if i > 0 {
			sb.WriteString(" | ")
		}
		fmt.Fprintf(&sb, "callerID=%q calleeName=%q bare=%q nameInIndex=%d",
			truncate(s.callerID, 60), s.calleeName, s.bareName, s.nameInIndex)
	}
	// Plus the first 10 symbol names in lex order so we can eyeball them.
	names := make([]string, 0, len(nameCount))
	for n := range nameCount {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) > 10 {
		names = names[:10]
	}
	sb.WriteString(" || first_10_symbol_names=[")
	sb.WriteString(strings.Join(names, ","))
	sb.WriteString("]")
	suite.Notes = sb.String()

	// Keep symbols import used in case the package import would otherwise
	// be flagged.
	_ = symbols.SymFunc

	suite.Duration = time.Since(t0)
	return suite
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
