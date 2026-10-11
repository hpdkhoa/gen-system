// generator/cmd/gen-enrich/main.go, v26.15 (session 9): the independent enrichment
// producer. It loads a project, asks the local model to describe each routine, and
// writes the results into the project's belief store as persistent enrich beliefs
// (which then survive parser rebuilds and are stale-checked against the source).
//
// This is a separate binary on purpose. Enrichment is slow and needs the AI stack.
// Keeping it out of band means the fast, deterministic parser rebuild never pays the
// model cost, and understand-server never links it. Running it is a deliberate,
// offline batch step: `gen-enrich --root ./myproject`.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/belief"
	gencontext "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/enrich"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func main() {
	root := flag.String("root", ".", "project root to enrich")
	limit := flag.Int("limit", 0, "max routines to enrich (0 = all)")
	summarize := flag.Bool("summarize", false, "also roll up per-service summaries from the beliefs")
	flag.Parse()

	if !enrich.Available() {
		fmt.Fprintln(os.Stderr, "gen-enrich: local model not reachable (Ollama). Nothing to do.")
		fmt.Fprintln(os.Stderr, "The parser and human belief tiers work without enrichment;")
		fmt.Fprintln(os.Stderr, "start Ollama and re-run to add model descriptions.")
		os.Exit(1)
	}

	fmt.Printf("Loading %s …\n", *root)
	ctx := gencontext.LoadContext(*root)
	store := belief.NewStore(ctx.Root)

	// Supply routine source so the store can stale-check enrich beliefs against the
	// current body on rebuild (v26.17). Without a provider, SourceHash fingerprints
	// are stored but never compared, so a changed routine would not flag its
	// description out of date.
	routines, bodies := routinesAndBodies(&ctx, *limit)
	store.SetBodyProvider(func(id string) string { return bodies[id] })

	fmt.Printf("Enriching %d routines with the local model …\n", len(routines))

	e := enrich.New()
	res := e.Run(store, routines)

	fmt.Printf("Done: %d routines enriched, %d skipped, %d beliefs written to %s/.gen/beliefs.json\n",
		res.Enriched, res.Skipped, res.Beliefs, ctx.Root)
	if res.SaveErrors > 0 {
		fmt.Printf("⚠️  %d routines were enriched but their beliefs could not be saved\n", res.SaveErrors)
	}

	if *summarize {
		// Rebuild so the fresh enrich beliefs are live in the store, then roll up
		// per-service summaries from the belief set (not raw source).
		store.RebuildFromIndex(ctx.Index, ctx.Effects)
		nsum := e.SummarizeServices(store)
		fmt.Printf("Rolled up %d service summaries.\n", nsum)
	}

	// Final rebuild before exit (v26.17). UpsertEnrichBeliefs stores the raw model
	// claims; the parser-validation gate runs during a rebuild. Without this pass
	// the persisted beliefs.json carries "validated": null and a reader can't see
	// whether a claim contradicts the code until something else happens to reload
	// the project. After this pass, beliefs.json on disk shows each validation result.
	store.RebuildFromIndex(ctx.Index, ctx.Effects)
	reportValidation(store)

	fmt.Println("Open the Studio Beliefs page to review them. The page marks enrich beliefs as model")
	fmt.Println("guesses and validated against the parser facts; verify or correct them there.")
}

// reportValidation prints how the model's claims fared against the parser facts,
// so the operator sees the verdict immediately rather than only in the Studio.
func reportValidation(store *belief.Store) {
	counts := map[string]int{}
	total := 0
	for _, b := range store.All() {
		if b.Source != belief.SourceEnrich {
			continue
		}
		// Summaries are enrich-sourced but have no parser facts to validate against
		// (they describe a scope, not a routine), so counting them as "unvalidated"
		// is misleading (v26.21).
		if b.Kind == belief.KindSummary {
			continue
		}
		total++
		v := b.Validated
		if v == "" {
			v = "(unvalidated)"
		}
		counts[v]++
	}
	if total == 0 {
		return
	}
	fmt.Printf("Validation against parser facts: %d enrich beliefs: ", total)
	parts := make([]string, 0, len(counts))
	for _, k := range []string{"consistent", "needs-human", "contradicts", "(unvalidated)"} {
		if n, ok := counts[k]; ok {
			parts = append(parts, fmt.Sprintf("%s=%d", k, n))
		}
	}
	fmt.Println(strings.Join(parts, ", "))
	if counts["contradicts"] > 0 {
		fmt.Printf("  ⚠ %d claim(s) contradict what the parser observed. Review these first.\n", counts["contradicts"])
	}
}

// routinesAndBodies returns the routines to enrich (at most limit, 0 means all)
// and the body of each routine by symbol ID, for the store's stale check.
func routinesAndBodies(ctx *gencontext.Context, limit int) ([]enrich.Routine, map[string]string) {
	// v26.58 (D016): the body map was filled only from the first limit routines, so
	// a changed routine outside the limit had no body and the rebuild skipped its
	// stale check. Bodies now come from every routine; the limit only cuts the list
	// that goes to the model.
	bodies := map[string]string{}
	routines := buildRoutines(ctx, 0)
	for _, r := range routines {
		bodies[r.SymbolID] = r.Body
	}
	if limit > 0 && len(routines) > limit {
		routines = routines[:limit]
	}
	return routines, bodies
}

// buildRoutines turns the project's callable symbols into enrichment units, reading
// each routine's source from its file span. Decoupled from the enrich package so
// that package never imports internal/context.
func buildRoutines(ctx *gencontext.Context, limit int) []enrich.Routine {
	// Cache file contents so we read each source file at most once.
	fileCache := map[string][]byte{}
	readFile := func(path string) []byte {
		if b, ok := fileCache[path]; ok {
			return b
		}
		b, err := os.ReadFile(path)
		if err != nil {
			b = nil
		}
		fileCache[path] = b
		return b
	}

	var out []enrich.Routine
	// Deterministic ordering (v26.17). AllSymbols() iterates a map, so its order is
	// random per run. With --limit N each run enriched a different N routines, so
	// repeated bounded runs accumulated beliefs instead of replacing them. Sorting
	// by symbol ID makes --limit reproducible and re-runnable.
	syms := ctx.Index.AllSymbols()
	sort.Slice(syms, func(i, j int) bool { return syms[i].ID < syms[j].ID })

	// Per-file sorted routine start lines (v26.20). Frontends that don't record
	// byte offsets (Java, COBOL, TS, Python) are sliced by line instead; knowing
	// where the next routine starts lets that slice stop cleanly instead of running
	// a fixed window into the following routine.
	startsByFile := map[string][]int{}
	for _, s := range syms {
		if s.Provenance.IsExternal() || !isRoutine(s.Kind) {
			continue
		}
		if s.Span.Start.Line > 0 {
			startsByFile[s.DefinedIn] = append(startsByFile[s.DefinedIn], s.Span.Start.Line)
		}
	}
	for f := range startsByFile {
		sort.Ints(startsByFile[f])
	}
	nextStartAfter := func(file string, line int) int {
		for _, ln := range startsByFile[file] {
			if ln > line {
				return ln
			}
		}
		return 0
	}

	for _, sym := range syms {
		if sym.Provenance.IsExternal() {
			continue
		}
		if !isRoutine(sym.Kind) {
			continue
		}

		body := bodyFor(readFile(sym.DefinedIn), sym, nextStartAfter(sym.DefinedIn, sym.Span.Start.Line))
		subj := sym.QualName
		if subj == "" {
			subj = sym.Name
		}

		r := enrich.Routine{
			SymbolID:   sym.ID,
			Subject:    subj,
			Name:       sym.Name,
			Language:   string(sym.Language),
			Params:     formatParams(sym),
			Calls:      formatCalls(ctx.Index, sym.ID),
			Effects:    formatEffects(ctx, sym.ID),
			Body:       body,
			DocComment: sym.DocComment,
		}
		out = append(out, r)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// sliceSource extracts a routine body from file bytes using span byte offsets.
// Falls back to "" when offsets are missing or out of range (enrichment then works
// from the parser facts alone).
func sliceSource(src []byte, start, end int) string {
	if src == nil || start < 0 || end <= start || end > len(src) {
		return ""
	}
	return string(src[start:end])
}

// sliceByLine extracts a routine body using 1-based line numbers. This is the
// fallback for frontends that do not populate byte offsets (v26.20).
//
// Only the Go frontend sets Span.*.Offset. It gets them at no cost from go/ast's
// token.FileSet. The hand-rolled Java, COBOL, TypeScript and Python frontends set
// Line only, so every routine in those languages had start==end==0 and produced an
// empty body: enrichment silently fell back to facts-only prose. Slicing by line
// recovers the real source for them.
//
// endLine <= 0 means the frontend recorded no end (common: only Start.Line is set).
// In that case we take a bounded window from startLine so the model still sees the
// routine, rather than nothing. The window is generous enough for a typical routine
// and capped so a huge file can't blow up the prompt.
func sliceByLine(src []byte, startLine, endLine int) string {
	if src == nil || startLine <= 0 {
		return ""
	}
	lines := strings.Split(string(src), "\n")
	if startLine > len(lines) {
		return ""
	}
	const fallbackWindow = 60 // lines, when the frontend gave no end
	last := endLine
	if last <= 0 || last < startLine {
		last = startLine + fallbackWindow
	}
	if last > len(lines) {
		last = len(lines)
	}
	return strings.Join(lines[startLine-1:last], "\n")
}

// bodyFor returns a routine's source, preferring exact byte offsets and falling
// back to line-based slicing for frontends that don't record offsets. nextLine, if
// > 0, is the start line of the next routine in the same file. It stops a
// windowed slice before it bleeds into a neighbouring routine.
func bodyFor(src []byte, sym *symbols.Symbol, nextLine int) string {
	if b := sliceSource(src, sym.Span.Start.Offset, sym.Span.End.Offset); b != "" {
		return b
	}
	end := sym.Span.End.Line
	if end <= 0 && nextLine > sym.Span.Start.Line {
		end = nextLine - 1 // stop just before the next routine begins
	}
	return sliceByLine(src, sym.Span.Start.Line, end)
}

// isRoutine reports whether a symbol kind is an enrichable callable.
func isRoutine(k symbols.SymbolKind) bool {
	switch k {
	case symbols.SymFunc, symbols.SymMethod, symbols.SymProcedure:
		return true
	}
	return false
}

func formatParams(sym *symbols.Symbol) string {
	parts := make([]string, 0, len(sym.Params))
	for _, p := range sym.Params {
		switch {
		case p.Name != "" && p.Type.Name != "":
			parts = append(parts, p.Name+" "+p.Type.Name)
		case p.Type.Name != "":
			parts = append(parts, p.Type.Name)
		case p.Name != "":
			parts = append(parts, p.Name)
		}
	}
	return strings.Join(parts, ", ")
}

func formatCalls(idx *symbols.Index, symID string) string {
	var names []string
	seen := map[string]bool{}
	for _, c := range idx.Callees(symID) {
		n := c.QualName
		if n == "" {
			n = c.Name
		}
		if n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	return strings.Join(names, ", ")
}

func formatEffects(ctx *gencontext.Context, symID string) string {
	e, ok := ctx.Effects[symID]
	if !ok {
		return ""
	}
	var effs []string
	for k, on := range e.Effects {
		if on {
			effs = append(effs, string(k))
		}
	}
	return strings.Join(effs, ", ")
}
