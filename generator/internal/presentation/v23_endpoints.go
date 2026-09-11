// generator/internal/presentation/v23_endpoints.go — v23.32 (stdlib
// provenance): CallgraphNode carries Provenance; the callgraph BFS hides
// stdlib/third-party nodes unless include_stdlib=1, so "follow a call into the
// runtime" is a deliberate toggle rather than the default. ArchitectureResponse
// reports stdlib symbol/resolution counts. Prior:
// generator/internal/presentation/v23_endpoints.go — v23: new endpoints that
// power the redesigned Studio (studio-v23.html).
//
// Four new routes, all under /api/present/v23/:
//
//	GET /api/present/v23/architecture
//	    Language breakdown, top packages, entry-point candidates, unknown
//	    counts. Used as the landing-page payload.
//
//	GET /api/present/v23/search?q=<query>&effect=<db|fs|network|pure>&limit=N
//	    Fuzzy symbol search + optional effect facet. Returns ranked symbol
//	    hits with file, line, kind, language.
//
//	GET /api/present/v23/callgraph?root=<symbolID>&depth=N&direction=<callers|callees|both>
//	    Subgraph rooted at one symbol. Caps at depth=4 to keep payloads
//	    reasonable. Each node carries its dominant effect for colouring.
//
//	GET /api/present/v23/effects
//	    All inferred function effects, grouped by kind, for the effect-facet
//	    UI in Wave 4.
//
// This file does NOT modify the v15 endpoints in presentation.go.
// It plugs into the same Server via RegisterV23Routes, which gen-server's
// api.NewServer wiring calls after the existing RegisterRoutes.
package presentation

import (
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// RegisterV23Routes attaches the v23 endpoints to mux. Call after
// RegisterRoutes so the v15 routes win on path-prefix matching (they don't
// overlap, but be defensive).
func (s *Server) RegisterV23Routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/present/v23/architecture", s.handleArchitectureV23)
	mux.HandleFunc("/api/present/v23/search", s.handleSearchV23)
	mux.HandleFunc("/api/present/v23/callgraph", s.handleCallgraphV23)
	mux.HandleFunc("/api/present/v23/effects", s.handleEffectsV23)
}

// ─── /api/present/v23/architecture ────────────────────────────────────

// ArchitectureResponse is the landing-page payload.
type ArchitectureResponse struct {
	TotalSymbols int                `json:"total_symbols"`
	TotalFiles   int                `json:"total_files"`
	Languages    []LanguageStat     `json:"languages"`
	Packages     []PackageStat      `json:"packages"`
	EntryPoints  []EntryPointStat   `json:"entry_points"`
	Unknowns     []UnknownStatGroup `json:"unknowns"`
	EdgeStats    EdgeStats          `json:"edges"`
	Provenance   ProvenanceStats    `json:"provenance"` // v23.32
	// v26.5: what the stdlib pass attached / skipped and WHY. Rendering this
	// beside the edge-resolution % turns that number from a mystery into a
	// self-diagnosing one ("java skipped: JAVA_HOME not set" tells the reader
	// exactly what to pass to close the gap).
	Stdlib StdlibReportView `json:"stdlib"`
}

// ProvenanceStats summarises stdlib/third-party ingest separately from the
// project. The project-facing aggregates above (TotalSymbols, Languages,
// Packages, …) count PROJECT symbols only, so the landing page stays about the
// user's code; ingested stdlib shows up here instead.
type ProvenanceStats struct {
	ProjectSymbols int `json:"project_symbols"`
	StdlibSymbols  int `json:"stdlib_symbols"`
	// EdgesIntoStdlib is project-caller edges that resolved into a stdlib
	// symbol — the "calls into the runtime we can follow" number.
	EdgesIntoStdlib int `json:"edges_into_stdlib"`
}

type LanguageStat struct {
	Language string `json:"language"`
	Symbols  int    `json:"symbols"`
	Files    int    `json:"files"`
}

type PackageStat struct {
	Path    string `json:"path"`
	Symbols int    `json:"symbols"`
	Files   int    `json:"files"`
}

type EntryPointStat struct {
	SymbolID string `json:"symbol_id"`
	Name     string `json:"name"`
	File     string `json:"file"`
	Kind     string `json:"kind"` // "main", "handler", "transaction"
	Language string `json:"language"`
}

type UnknownStatGroup struct {
	Language string `json:"language"`
	Count    int    `json:"count"`
}

type EdgeStats struct {
	Total    int `json:"total"`
	Resolved int `json:"resolved"`
}

func (s *Server) handleArchitectureV23(w http.ResponseWriter, r *http.Request) {
	cors(w)
	if r.Method == http.MethodOptions {
		return
	}
	if s.idx == nil {
		writeJSON(w, ArchitectureResponse{})
		return
	}
	all := s.idx.AllSymbols()
	resp := ArchitectureResponse{}

	// Language + per-file aggregation.
	langSyms := map[string]int{}
	langFiles := map[string]map[string]struct{}{}
	pkgSyms := map[string]int{}
	pkgFiles := map[string]map[string]struct{}{}
	allFiles := map[string]struct{}{}
	for _, sym := range all {
		// v23.32: stdlib/third-party symbols are tallied in Provenance and
		// excluded from every project-facing aggregate so the landing page
		// describes the user's code, not the runtime.
		if sym.Provenance.IsExternal() {
			resp.Provenance.StdlibSymbols++
			continue
		}
		resp.Provenance.ProjectSymbols++
		lang := string(sym.Language)
		langSyms[lang]++
		if _, ok := langFiles[lang]; !ok {
			langFiles[lang] = map[string]struct{}{}
		}
		langFiles[lang][sym.DefinedIn] = struct{}{}
		allFiles[sym.DefinedIn] = struct{}{}

		// Package = the parent directory relative to root. Falls back to
		// the filename when we can't compute a clean parent.
		pkg := filepath.Dir(sym.DefinedIn)
		if s.root != "" {
			if rel, err := filepath.Rel(s.root, pkg); err == nil && !strings.HasPrefix(rel, "..") {
				pkg = rel
			}
		}
		if pkg == "" || pkg == "." {
			pkg = "(root)"
		}
		pkgSyms[pkg]++
		if _, ok := pkgFiles[pkg]; !ok {
			pkgFiles[pkg] = map[string]struct{}{}
		}
		pkgFiles[pkg][sym.DefinedIn] = struct{}{}
	}
	resp.TotalFiles = len(allFiles)
	resp.TotalSymbols = resp.Provenance.ProjectSymbols

	for lang, n := range langSyms {
		resp.Languages = append(resp.Languages, LanguageStat{
			Language: lang, Symbols: n, Files: len(langFiles[lang]),
		})
	}
	sort.Slice(resp.Languages, func(i, j int) bool { return resp.Languages[i].Symbols > resp.Languages[j].Symbols })

	for pkg, n := range pkgSyms {
		resp.Packages = append(resp.Packages, PackageStat{
			Path: pkg, Symbols: n, Files: len(pkgFiles[pkg]),
		})
	}
	sort.Slice(resp.Packages, func(i, j int) bool { return resp.Packages[i].Symbols > resp.Packages[j].Symbols })
	if len(resp.Packages) > 20 {
		resp.Packages = resp.Packages[:20]
	}

	// Entry points: function symbols named main, HTTP handler shapes,
	// or CICS transaction procedures. Heuristic but useful.
	for _, sym := range all {
		if sym.Provenance.IsExternal() {
			continue
		}
		kind := classifyEntry(sym)
		if kind == "" {
			continue
		}
		resp.EntryPoints = append(resp.EntryPoints, EntryPointStat{
			SymbolID: sym.ID, Name: sym.QualName, File: sym.DefinedIn,
			Kind: kind, Language: string(sym.Language),
		})
	}
	sort.Slice(resp.EntryPoints, func(i, j int) bool {
		if resp.EntryPoints[i].Language != resp.EntryPoints[j].Language {
			return resp.EntryPoints[i].Language < resp.EntryPoints[j].Language
		}
		return resp.EntryPoints[i].Name < resp.EntryPoints[j].Name
	})

	// Edge resolution stats — the headline "do we understand this?" number.
	// v23.32: counted over PROJECT-caller edges only, so ingesting a stdlib
	// (which brings its own internal edges) does not move these numbers. Among
	// project-caller edges, EdgesIntoStdlib is those resolving into the runtime.
	edges := s.idx.AllCallEdges()
	for _, e := range edges {
		caller, ok := s.idx.ByID(e.CallerID)
		if !ok || caller == nil || caller.Provenance.IsExternal() {
			continue
		}
		resp.EdgeStats.Total++
		if e.CalleeID != "" {
			resp.EdgeStats.Resolved++
			if callee, ok := s.idx.ByID(e.CalleeID); ok && callee != nil && callee.Provenance.IsExternal() {
				resp.Provenance.EdgesIntoStdlib++
			}
		}
	}

	// v26.5: attach the stdlib outcome so the UI can explain the resolution %.
	// Note this is read through the Server's own lock (StdlibReport()), so a
	// dynamic project swap can replace it safely while requests are in flight.
	resp.Stdlib = s.StdlibReport()

	writeJSON(w, resp)
}

func classifyEntry(sym *symbols.Symbol) string {
	if sym == nil {
		return ""
	}
	name := sym.Name
	if name == "" {
		name = sym.QualName
	}
	switch sym.Kind {
	case symbols.SymFunc:
		if name == "main" {
			return "main"
		}
		if strings.HasPrefix(name, "Handle") || strings.Contains(name, "Handler") {
			return "handler"
		}
	case symbols.SymProcedure:
		if sym.Layer == "" || sym.Layer == "transaction" {
			return "transaction"
		}
	}
	return ""
}

// ─── /api/present/v23/search ──────────────────────────────────────────

type SearchHit struct {
	SymbolID   string   `json:"symbol_id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Language   string   `json:"language"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Exported   bool     `json:"exported"`
	Provenance string   `json:"provenance,omitempty"` // v23.32
	Effects    []string `json:"effects,omitempty"`
	MatchRank  int      `json:"match_rank"`
}

type SearchResponse struct {
	Query string      `json:"query"`
	Total int         `json:"total"`
	Hits  []SearchHit `json:"hits"`
}

func (s *Server) handleSearchV23(w http.ResponseWriter, r *http.Request) {
	cors(w)
	if r.Method == http.MethodOptions {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	effect := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("effect")))
	includeStdlib := r.URL.Query().Get("include_stdlib") == "1" ||
		r.URL.Query().Get("include_stdlib") == "true"
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	resp := SearchResponse{Query: q}
	if s.idx == nil {
		writeJSON(w, resp)
		return
	}

	all := s.idx.AllSymbols()
	s.mu.RLock()
	efxCopy := make(map[string]*flow.FuncEffects, len(s.efx))
	for k, v := range s.efx {
		efxCopy[k] = v
	}
	s.mu.RUnlock()

	qLower := strings.ToLower(q)
	var hits []SearchHit
	for _, sym := range all {
		if sym == nil || sym.Kind == symbols.SymModule {
			continue
		}
		// v23.32: stdlib hidden by default so search stays about the project.
		if !includeStdlib && sym.Provenance.IsExternal() {
			continue
		}
		nameLower := strings.ToLower(sym.Name)
		qualLower := strings.ToLower(sym.QualName)
		rank := -1
		switch {
		case q == "":
			rank = 4
		case nameLower == qLower || qualLower == qLower:
			rank = 0
		case strings.HasPrefix(nameLower, qLower) || strings.HasPrefix(qualLower, qLower):
			rank = 1
		case strings.Contains(nameLower, qLower) || strings.Contains(qualLower, qLower):
			rank = 2
		case subsequenceMatch(qLower, nameLower) || subsequenceMatch(qLower, qualLower):
			rank = 3
		}
		if rank < 0 {
			continue
		}

		var efxNames []string
		if fe, ok := efxCopy[sym.ID]; ok && fe != nil {
			for k, v := range fe.Effects {
				if v {
					efxNames = append(efxNames, string(k))
				}
			}
			sort.Strings(efxNames)
		}
		if effect != "" {
			match := false
			for _, e := range efxNames {
				if e == effect {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

		hits = append(hits, SearchHit{
			SymbolID: sym.ID, Name: sym.QualName, Kind: string(sym.Kind),
			Language: string(sym.Language), File: sym.DefinedIn,
			Line: sym.Span.Start.Line, Exported: sym.Exported,
			Provenance: string(sym.Provenance),
			Effects:    efxNames, MatchRank: rank,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].MatchRank != hits[j].MatchRank {
			return hits[i].MatchRank < hits[j].MatchRank
		}
		return hits[i].Name < hits[j].Name
	})
	resp.Total = len(hits)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	resp.Hits = hits
	writeJSON(w, resp)
}

// subsequenceMatch returns true if every rune of needle appears in haystack
// in order (not necessarily contiguously). Both args must already be lower-cased.
func subsequenceMatch(needle, haystack string) bool {
	if needle == "" {
		return true
	}
	i := 0
	for _, c := range haystack {
		if i >= len(needle) {
			break
		}
		if rune(needle[i]) == c {
			i++
		}
	}
	return i == len(needle)
}

// ─── /api/present/v23/callgraph ───────────────────────────────────────

type CallgraphNode struct {
	SymbolID   string   `json:"symbol_id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Language   string   `json:"language"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Depth      int      `json:"depth"`
	Provenance string   `json:"provenance,omitempty"` // v23.32: "", "stdlib", "thirdparty"
	IsInit     bool     `json:"is_init,omitempty"`    // v24: constructor / allocation (new X)
	Effects    []string `json:"effects,omitempty"`
}

type CallgraphEdge struct {
	From       string `json:"from"`
	To         string `json:"to,omitempty"`
	ToName     string `json:"to_name"`
	Unresolved bool   `json:"unresolved"`
}

type CallgraphResponse struct {
	Root      string          `json:"root"`
	Direction string          `json:"direction"`
	Depth     int             `json:"depth"`
	Nodes     []CallgraphNode `json:"nodes"`
	Edges     []CallgraphEdge `json:"edges"`
	Truncated bool            `json:"truncated,omitempty"`
}

const callgraphMaxNodes = 200

// isConstructorSym reports whether a symbol is a constructor — a method whose
// name equals its receiver type (Java `OrderService.OrderService`, and similar
// in other OO frontends). v24: used to relabel such nodes as an allocation
// ("new OrderService") in the call graph instead of the confusing Type.Type form.
func isConstructorSym(sym *symbols.Symbol) bool {
	return sym != nil && sym.Kind == symbols.SymMethod &&
		sym.ReceiverType != "" && sym.Name == sym.ReceiverType
}

func (s *Server) handleCallgraphV23(w http.ResponseWriter, r *http.Request) {
	cors(w)
	if r.Method == http.MethodOptions {
		return
	}
	root := r.URL.Query().Get("root")
	if root == "" {
		http.Error(w, "root param required", http.StatusBadRequest)
		return
	}
	depth, _ := strconv.Atoi(r.URL.Query().Get("depth"))
	if depth <= 0 || depth > 4 {
		depth = 2
	}
	direction := r.URL.Query().Get("direction")
	if direction == "" {
		direction = "callees"
	}
	// v23.32: stdlib nodes are hidden by default so a project's own graph
	// isn't drowned by the runtime. include_stdlib=1 reveals them, turning the
	// graph into a "follow the call into the runtime" view. The root is always
	// shown even when it is itself a stdlib symbol (direct stdlib exploration).
	includeStdlib := r.URL.Query().Get("include_stdlib") == "1" ||
		r.URL.Query().Get("include_stdlib") == "true"

	resp := CallgraphResponse{Root: root, Direction: direction, Depth: depth}
	if s.idx == nil {
		writeJSON(w, resp)
		return
	}
	rootSym, ok := s.idx.ByID(root)
	if !ok {
		byName := s.idx.ByName(root)
		if len(byName) == 0 {
			http.Error(w, "symbol not found", http.StatusNotFound)
			return
		}
		rootSym = byName[0]
	}

	s.mu.RLock()
	efxCopy := make(map[string]*flow.FuncEffects, len(s.efx))
	for k, v := range s.efx {
		efxCopy[k] = v
	}
	s.mu.RUnlock()

	visited := map[string]int{}
	addNode := func(sym *symbols.Symbol, d int) bool {
		if _, ok := visited[sym.ID]; ok {
			return false
		}
		visited[sym.ID] = d
		var efxNames []string
		if fe, ok := efxCopy[sym.ID]; ok && fe != nil {
			for k, v := range fe.Effects {
				if v {
					efxNames = append(efxNames, string(k))
				}
			}
			sort.Strings(efxNames)
		}
		// v24: a constructor (a method whose name equals its receiver type, e.g.
		// Java `OrderService.OrderService`) is an allocation, not a normal call.
		// Relabel it "new OrderService" and flag IsInit so the UI shows it as an
		// init/allocation rather than the confusing dotted "Type.Type" form.
		displayName := sym.QualName
		isInit := isConstructorSym(sym)
		if isInit {
			displayName = "new " + sym.ReceiverType
		}
		resp.Nodes = append(resp.Nodes, CallgraphNode{
			SymbolID: sym.ID, Name: displayName, Kind: string(sym.Kind),
			Language: string(sym.Language), File: sym.DefinedIn,
			Line: sym.Span.Start.Line, Depth: d, Effects: efxNames,
			Provenance: string(sym.Provenance), IsInit: isInit,
		})
		return true
	}
	addNode(rootSym, 0)

	// Cache all call edges once and index unresolved edges by CallerID so
	// the BFS doesn't re-scan the edge list per frontier node.
	allEdges := s.idx.AllCallEdges()
	unresolvedByCaller := map[string][]symbols.CallEdge{}
	for _, e := range allEdges {
		if e.CalleeID == "" {
			unresolvedByCaller[e.CallerID] = append(unresolvedByCaller[e.CallerID], e)
		}
	}

	frontier := []*symbols.Symbol{rootSym}
	for d := 1; d <= depth; d++ {
		var next []*symbols.Symbol
		for _, cur := range frontier {
			if len(resp.Nodes) >= callgraphMaxNodes {
				resp.Truncated = true
				break
			}
			var neighbours []*symbols.Symbol
			if direction == "callees" || direction == "both" {
				neighbours = append(neighbours, s.idx.Callees(cur.ID)...)
				for _, e := range unresolvedByCaller[cur.ID] {
					resp.Edges = append(resp.Edges, CallgraphEdge{
						From: cur.ID, ToName: e.CalleeName, Unresolved: true,
					})
				}
			}
			if direction == "callers" || direction == "both" {
				neighbours = append(neighbours, s.idx.Callers(cur.ID)...)
			}
			for _, nb := range neighbours {
				if nb == nil {
					continue
				}
				// v23.32: skip stdlib/third-party neighbours unless asked for.
				// We drop both the edge and the node so the graph stays the
				// project's own shape by default.
				if !includeStdlib && nb.Provenance.IsExternal() {
					continue
				}
				if direction == "callers" {
					resp.Edges = append(resp.Edges, CallgraphEdge{
						From: nb.ID, To: cur.ID, ToName: cur.Name,
					})
				} else {
					resp.Edges = append(resp.Edges, CallgraphEdge{
						From: cur.ID, To: nb.ID, ToName: nb.Name,
					})
				}
				if addNode(nb, d) {
					next = append(next, nb)
				}
			}
		}
		if resp.Truncated {
			break
		}
		frontier = next
		if len(frontier) == 0 {
			break
		}
	}

	writeJSON(w, resp)
}

// ─── /api/present/v23/effects ─────────────────────────────────────────

type EffectsListResponse struct {
	ByEffect map[string][]EffectListEntry `json:"by_effect"`
	Total    int                          `json:"total"`
}

type EffectListEntry struct {
	SymbolID string `json:"symbol_id"`
	Name     string `json:"name"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Language string `json:"language"`
}

func (s *Server) handleEffectsV23(w http.ResponseWriter, r *http.Request) {
	cors(w)
	if r.Method == http.MethodOptions {
		return
	}
	resp := EffectsListResponse{ByEffect: map[string][]EffectListEntry{}}
	if s.idx == nil {
		writeJSON(w, resp)
		return
	}
	s.mu.RLock()
	efxCopy := make(map[string]*flow.FuncEffects, len(s.efx))
	for k, v := range s.efx {
		efxCopy[k] = v
	}
	s.mu.RUnlock()

	for id, fe := range efxCopy {
		if fe == nil {
			continue
		}
		sym, ok := s.idx.ByID(id)
		if !ok {
			continue
		}
		entry := EffectListEntry{
			SymbolID: sym.ID, Name: sym.QualName, File: sym.DefinedIn,
			Line: sym.Span.Start.Line, Language: string(sym.Language),
		}
		for k, v := range fe.Effects {
			if v {
				ks := string(k)
				resp.ByEffect[ks] = append(resp.ByEffect[ks], entry)
				resp.Total++
			}
		}
	}
	for k := range resp.ByEffect {
		entries := resp.ByEffect[k]
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		resp.ByEffect[k] = entries
	}
	writeJSON(w, resp)
}

// cors writes minimal CORS headers so the studio can hit these endpoints
// from a non-default origin during dev.
func cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}
