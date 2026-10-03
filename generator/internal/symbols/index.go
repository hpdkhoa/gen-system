// index.go — v23.32 (stdlib provenance): AddModuleWithProvenance stamps a
// module's symbols with an origin tier; resolveByNameLocked is now
// provenance-aware (project-tier symbols win over stdlib on a bare-name
// collision, keeping project resolution stable and deterministic when a stdlib
// is ingested). Prior:
// Patched in v19.8 (resolve-coldload-edges): added the exported method
// `Resolve()` which sweeps callEdges and links CalleeName → CalleeID using
// the global symbol map. Callers that build the index via the streaming
// AddModule path (LoadContext, gen-server start-up) now call this once
// after the parse loop completes. Previously only BuildFromModules invoked
// the unexported resolveCallEdges, so any cold-load index had every
// CallEdge.CalleeID == "" — the call graph was empty until the watcher
// fired UpdateFile on something. The fix is purely additive: AddModule's
// invariants are unchanged and UpdateFile's existing per-call resolution
// sweep continues to work.
//
// Patched in Phase A (revision: consolidates the prior index_removefile.go):
//   - Added RemoveFile(path) and UpdateFile(path, module) for incremental rebuild.
//     These remove all symbols, refs, and call edges originating from a file,
//     then (for UpdateFile) re-ingest the new Module.
//   - Absorbs RemoveFile and fileFromSymbolID from the standalone
//     index_removefile.go file. That file MUST BE DELETED from the
//     symbols/ directory before this patch will compile.
//   - All existing tests in index_removefile_test.go remain valid against
//     the new RemoveFile semantics (verified against each assertion).
//   - extractCallEdges still runs under the write lock held by callers
//     (AddModule and UpdateFile), preserving thread safety.
package symbols

// ─────────────────────────────────────────────────────────────
// index.go — Cross-file symbol index and resolver
//
// The Index is the central "symbol database" for a repository.
// It aggregates per-file Tables, resolves cross-file references,
// and persists to .gen/symbols.json for incremental rebuilds.
//
// Resolver answers: "given this name in this file, what symbol
// does it refer to?" using:
//   1. Local table lookup
//   2. Import resolution (follow import path → find exported symbol)
//   3. Package-level lookup for same-package files
// ─────────────────────────────────────────────────────────────

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

// Index holds all symbols and references for a repository.
type Index struct {
	mu         sync.RWMutex
	tables     map[string]*Table      // filePath → Table
	allSymbols map[string]*Symbol     // symbolID → Symbol
	refs       map[string][]Reference // symbolID → []Reference
	callEdges  []CallEdge
}

// NewIndex creates an empty Index.
func NewIndex() *Index {
	return &Index{
		tables:     make(map[string]*Table),
		allSymbols: make(map[string]*Symbol),
		refs:       make(map[string][]Reference),
	}
}

// ─── Building ─────────────────────────────────────────────────

// AddModule extracts symbols from a Module and merges them in.
// Replaces any prior symbols for that file.
func (idx *Index) AddModule(m *ast.Module) {
	idx.addModule(m, ProvProject)
}

// AddModuleWithProvenance is AddModule but stamps every symbol from the module
// with the given origin tier. Used by the stdlib loader so standard-library
// symbols are distinguishable from project symbols everywhere downstream.
// AddModule keeps its exact prior behaviour (ProvProject) for all callers.
func (idx *Index) AddModuleWithProvenance(m *ast.Module, prov Provenance) {
	idx.addModule(m, prov)
}

func (idx *Index) addModule(m *ast.Module, prov Provenance) {
	t := BuildTable(m)
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Remove stale entries for this file (symbols, refs, call edges)
	idx.removeFileLocked(m.FilePath)

	idx.tables[m.FilePath] = t
	for id, sym := range t.Symbols {
		if prov != ProvProject {
			sym.Provenance = prov
		}
		idx.allSymbols[id] = sym
	}

	// Extract call edges from function bodies
	for _, n := range m.Nodes {
		idx.extractCallEdges(n, m.FilePath, m.Language, "", "")
	}
}

// extractCallEdges records one call edge per call node under n.
//
// ownerName is the name of the enclosing class, function or procedure, the same
// owner extractNode passes down, so a method with no Receiver gets the caller ID
// its symbol has.
func (idx *Index) extractCallEdges(n *ast.Node, filePath string, lang ast.Language, ownerID, ownerName string) {
	if n == nil {
		return
	}
	var nodeID string
	childOwner := ownerName
	switch n.Kind {
	case ast.NodeFunction:
		nodeID = SymbolID(filePath, SymFunc, n.Name)
		childOwner = n.Name
	case ast.NodeMethod:
		// v26.58 (D102): TS and Python methods have no Receiver, so every one got
		// the caller ID ".name" and the edges of two classes merged. Use the owner
		// name, as extractNode now does.
		recvName := ownerName
		if n.Receiver != nil {
			recvName = n.Receiver.Name
		}
		nodeID = SymbolID(filePath, SymMethod, recvName+"."+n.Name)
	case ast.NodeProcedure:
		nodeID = SymbolID(filePath, SymProcedure, n.Name)
		childOwner = n.Name
	case ast.NodeStruct, ast.NodeClass, ast.NodeInterface:
		nodeID = ownerID
		childOwner = n.Name
	default:
		nodeID = ownerID
	}

	for _, child := range n.Children {
		if child.Kind == ast.NodeCall && nodeID != "" {
			idx.callEdges = append(idx.callEdges, CallEdge{
				CallerID:   nodeID,
				CalleeName: child.Callee,
				Span:       child.Span,
			})
		}
		idx.extractCallEdges(child, filePath, lang, nodeID, childOwner)
	}
}

// BuildFromModules builds the index from a slice of modules.
func BuildFromModules(modules []*ast.Module) *Index {
	idx := NewIndex()
	for _, m := range modules {
		if m != nil {
			idx.AddModule(m)
		}
	}
	idx.resolveCallEdges()
	return idx
}

// ResolveAll sweeps every call edge and links its CalleeName to a CalleeID
// when the name resolves to a known symbol. Idempotent: edges already
// resolved are left alone, so re-running is cheap when the index has
// been mostly stable.
//
// v19.8: callers that build the index incrementally via AddModule (the
// LoadContext path, the gen-server start-up path) MUST invoke this once
// after the parse loop completes — AddModule does not resolve callees on
// its own (it can't, because subsequent files may define the callee).
// UpdateFile already performs an in-line per-call resolution sweep, so
// the watcher path is unaffected.
func (idx *Index) ResolveAll() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	for i, edge := range idx.callEdges {
		if edge.CalleeID != "" {
			continue
		}
		callee := idx.resolveEdgeLocked(edge)
		if callee != nil {
			idx.callEdges[i].CalleeID = callee.ID
		}
	}
}

// resolveEdgeLocked (v26.34) resolves one call edge. A package qualified
// name is resolved through the CALLER FILE'S IMPORTS: when the first segment
// of the callee is an import alias in that file, only symbols defined under
// that import path are candidates, and if none exists the edge stays
// unresolved. Before this, `errors.New` was stripped to the bare name `New`
// and could be captured by any project symbol called New, and a stdlib call
// that was not captured showed up as an unresolved project call. A name
// whose first segment is not an import (a receiver variable such as
// s.store.Get) falls back to bare name resolution with the tier preference,
// unchanged. A function literal called in place never resolves.
func (idx *Index) resolveEdgeLocked(e CallEdge) *Symbol {
	name := e.CalleeName
	if name == "" || strings.HasPrefix(name, ast.FuncLiteralCallee) {
		return nil
	}
	if dot := strings.Index(name, "."); dot >= 0 {
		qual := name[:dot]
		if caller, ok := idx.allSymbols[e.CallerID]; ok {
			if importPath := idx.resolveImportPath(qual, caller.DefinedIn); importPath != "" {
				return idx.resolveUnderImportLocked(importPath, name[strings.LastIndex(name, ".")+1:])
			}
		}
	}
	return idx.resolveByNameLocked(name)
}

// resolveUnderImportLocked returns the symbol named bare whose defining
// directory sits under importPath, preferring an exported one; nil when the
// package is not in the index (an external package that was not ingested).
// Candidates are sorted by ID first, so the choice is deterministic.
func (idx *Index) resolveUnderImportLocked(importPath, bare string) *Symbol {
	type scored struct {
		sym   *Symbol
		score int
	}
	var candidates []scored
	for _, s := range idx.allSymbols {
		if s.Name != bare || s.Kind == SymImport {
			continue
		}
		if n := importMatchScore(s.DefinedIn, importPath); n > 0 {
			candidates = append(candidates, scored{s, n})
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	// Longest import suffix first, then project over external, then exported,
	// then ID. The provenance tie break matters: a project package named
	// `trace` and the stdlib's `runtime/trace` both end with "/trace", and
	// without it the stdlib symbol could win on ID order once a stdlib was
	// attached, which moved internal recall between the two loads.
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.sym.Provenance.IsExternal() != b.sym.Provenance.IsExternal() {
			return !a.sym.Provenance.IsExternal()
		}
		if a.sym.Exported != b.sym.Exported {
			return a.sym.Exported
		}
		return a.sym.ID < b.sym.ID
	})
	return candidates[0].sym
}

// importMatchScore returns how many trailing segments of importPath the
// directory of a defining file ends with, or 0 when none. A project import
// "app/internal/store" lives in .../internal/store (score 2 or 3 depending
// on the checkout name); a stdlib import "net/http" lives in
// GOROOT/src/net/http (score 2); "runtime/trace" against a project dir
// .../internal/trace scores 1.
func importMatchScore(definedIn, importPath string) int {
	dir := filepath.ToSlash(filepath.Dir(definedIn))
	segs := strings.Split(strings.Trim(importPath, "/"), "/")
	for i := 0; i < len(segs); i++ {
		sfx := strings.Join(segs[i:], "/")
		if dir == sfx || strings.HasSuffix(dir, "/"+sfx) {
			return len(segs) - i
		}
	}
	return 0
}

// dirMatchesImport reports whether importMatchScore is positive.
func dirMatchesImport(definedIn, importPath string) bool {
	return importMatchScore(definedIn, importPath) > 0
}

// resolveCallEdges links CalleeName → CalleeID where possible.
//
// v26.26: this used to call a second resolver, resolveByName, which ranged over
// the symbol map in its randomized order and had no provenance tier preference
// at all. That made the BuildFromModules path (used by cmd/bench) both
// non-deterministic AND willing to let a stdlib symbol capture a project call —
// the exact defect the v23.32 note below describes as fixed, fixed in only one
// of the two paths. There is now a single resolution rule in this package.
// Takes the write lock because it mutates callEdges; no caller holds it.
func (idx *Index) resolveCallEdges() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	for i, edge := range idx.callEdges {
		if edge.CalleeID != "" {
			continue
		}
		callee := idx.resolveEdgeLocked(edge)
		if callee != nil {
			idx.callEdges[i].CalleeID = callee.ID
		}
	}
}

// ─── Incremental updates (Phase A) ────────────────────────────

// RemoveFile drops every trace of filePath from the index:
//   - the file's Table
//   - all symbols defined in that file (from allSymbols)
//   - all refs whose In == filePath
//   - all call edges whose CallerID belongs to that file
//   - any CalleeID that pointed to a symbol in that file is cleared
//     to "" (the call edge itself stays, becoming an unresolved call)
//
// Idempotent — calling RemoveFile on an unknown path is a no-op.
//
// Use this when a source file has been deleted from the project.
// To re-parse an existing file with new contents, prefer UpdateFile,
// which combines RemoveFile + AddModule under a single lock.
func (idx *Index) RemoveFile(filePath string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.removeFileLocked(filePath)
}

// removeFileLocked is the lock-held form of RemoveFile. Required so
// AddModule (which already holds the write lock) can reuse it.
func (idx *Index) removeFileLocked(filePath string) {
	// Collect IDs of symbols defined in filePath. We do this once to
	// avoid scanning allSymbols twice (once for delete, once for
	// CalleeID clearing).
	deletedIDs := map[string]struct{}{}

	if old, ok := idx.tables[filePath]; ok {
		for id := range old.Symbols {
			deletedIDs[id] = struct{}{}
			delete(idx.allSymbols, id)
		}
		delete(idx.tables, filePath)
	} else {
		// Defensive: if no table exists for filePath, still walk
		// allSymbols by SymbolID prefix — symbols persisted from a
		// Load() may exist without a Table.
		prefix := filePath + "#"
		for id := range idx.allSymbols {
			if strings.HasPrefix(id, prefix) {
				deletedIDs[id] = struct{}{}
				delete(idx.allSymbols, id)
			}
		}
	}

	// Drop refs entries owned by deleted symbols.
	for id := range deletedIDs {
		delete(idx.refs, id)
	}

	// Drop ref entries whose In points to filePath, even if the
	// symbol they reference still exists in another file.
	for id, list := range idx.refs {
		kept := list[:0]
		for _, r := range list {
			if r.In != filePath {
				kept = append(kept, r)
			}
		}
		if len(kept) == 0 {
			delete(idx.refs, id)
		} else {
			idx.refs[id] = kept
		}
	}

	// Drop call edges whose caller belongs to filePath.
	// Clear CalleeID on edges whose callee was deleted (callee
	// becomes unresolved again; CalleeName preserved for re-resolution).
	callerPrefix := filePath + "#"
	if len(idx.callEdges) > 0 {
		kept := idx.callEdges[:0]
		for _, e := range idx.callEdges {
			if strings.HasPrefix(e.CallerID, callerPrefix) {
				continue
			}
			if _, gone := deletedIDs[e.CalleeID]; gone {
				e.CalleeID = ""
			}
			kept = append(kept, e)
		}
		idx.callEdges = kept
	}
}

// UpdateFile re-ingests a single file's Module. Equivalent to
// RemoveFile(filePath) + AddModule(m), but under one lock, and with
// a call-edge re-resolution pass at the end so previously-unresolved
// edges can attach to symbols newly introduced in m.
//
// The filePath argument is the canonical key. If m is non-nil and
// m.FilePath disagrees with filePath, m.FilePath takes effect inside
// the index because BuildTable derives keys from it; callers must
// keep them consistent.
//
// Returns immediately if m is nil after dropping the file's state
// (treat nil as "the file no longer exists or failed to parse").
func (idx *Index) UpdateFile(filePath string, m *ast.Module) {
	if m == nil {
		idx.RemoveFile(filePath)
		return
	}

	// BuildTable does not touch idx state — safe outside the lock.
	t := BuildTable(m)

	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.removeFileLocked(filePath)
	// If m.FilePath differs, also clear any state under that key so
	// we don't leave duplicate symbol entries from the old m.FilePath.
	if m.FilePath != filePath {
		idx.removeFileLocked(m.FilePath)
	}

	idx.tables[m.FilePath] = t
	for id, sym := range t.Symbols {
		idx.allSymbols[id] = sym
	}
	for _, n := range m.Nodes {
		idx.extractCallEdges(n, m.FilePath, m.Language, "", "")
	}

	// Re-resolve callees that were left dangling. We don't grab the
	// lock again because we already hold it (write).
	for i, edge := range idx.callEdges {
		if edge.CalleeID != "" {
			continue
		}
		callee := idx.resolveEdgeLocked(edge)
		if callee != nil {
			idx.callEdges[i].CalleeID = callee.ID
		}
	}
}

// resolveByNameLocked is the single global name-resolution rule in this package.
// The caller must hold the lock. Used by ResolveAll, resolveCallEdges, and
// UpdateFile's re-resolution sweep.
//
// v23.32: provenance-aware tie-break. Once a stdlib is ingested, an exported
// stdlib symbol can share a bare name with a project symbol (Write, String,
// New, …). Returning whichever the map happened to yield first would let the
// stdlib symbol capture a project call, both corrupting the call graph and
// making project resolution non-deterministic. So we PREFER a project-tier
// match: a project (non-external) exported symbol wins outright; only if no
// project symbol of that name exists do we fall to a stdlib/thirdparty one.
// Within a tier the first exported match wins (unexported is the last resort).
// v26.25: "first" is taken from candidates sorted by SymbolID, not from map
// order, so the winner is a function of the input alone.
func (idx *Index) resolveByNameLocked(name string) *Symbol {
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		name = name[dot+1:]
	}
	// v26.25 (determinism fix): collect every candidate first, then choose.
	// The prior version ranged over idx.allSymbols and took the first match in
	// each tier. idx.allSymbols is a map, so Go randomizes that order: when two
	// symbols in the SAME tier shared a bare name, which one won varied between
	// runs on identical input. That broke the determinism contract that the
	// rest of the system relies on, and the call graph changed from run to run
	// with no warning. The code now sorts candidates by their unique,
	// stable SymbolID before the tier preference is applied, so the winner is a
	// function of the input alone. Tier order is unchanged.
	var candidates []*Symbol
	for _, s := range idx.allSymbols {
		if s.Name == name {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })

	var (
		projectExported  *Symbol
		projectAny       *Symbol
		externalExported *Symbol
		externalAny      *Symbol
	)
	for _, s := range candidates {
		if s.Provenance.IsExternal() {
			if s.Exported && externalExported == nil {
				externalExported = s
			}
			if externalAny == nil {
				externalAny = s
			}
			continue
		}
		if s.Exported {
			if projectExported == nil {
				projectExported = s
			}
			continue
		}
		if projectAny == nil {
			projectAny = s
		}
	}
	switch {
	case projectExported != nil:
		return projectExported
	case projectAny != nil:
		return projectAny
	case externalExported != nil:
		return externalExported
	default:
		return externalAny
	}
}

// ─── Lookup ───────────────────────────────────────────────────

// ByID returns a symbol by its full ID.
func (idx *Index) ByID(id string) (*Symbol, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	s, ok := idx.allSymbols[id]
	return s, ok
}

// ByName returns all symbols with a given name across all files.
func (idx *Index) ByName(name string) []*Symbol {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var out []*Symbol
	for _, s := range idx.allSymbols {
		if s.Name == name {
			out = append(out, s)
		}
	}
	// v26.26: sort before returning. allSymbols is a map, so without this the
	// slice order is randomized per call, and several callers treat it as
	// ordered — context.go's buildEntityUsage/buildArchSummary walk it to build
	// the derived views that are supposed to be deterministic. Returning an
	// arbitrary order from a public accessor is a determinism hazard handed to
	// every caller; SymbolID is unique and stable, so sorting on it is total.
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Resolve resolves a name in the context of a specific file.
// Resolution order:
//  1. Local file symbols
//  2. Same-package (same directory) symbols
//  3. Imported-package symbols (follow import path)
//  4. Global exported symbols
func (idx *Index) Resolve(name, fromFile string) (*Symbol, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// 1. Local
	if t, ok := idx.tables[fromFile]; ok {
		syms := t.Lookup(name)
		if len(syms) > 0 {
			return syms[0], true
		}
	}

	// 2. Same directory (= same Go package, or same conceptual module
	// for other languages). v18.4: the previous code required
	// syms[0].Exported, which was wrong for Go — package-private
	// symbols *are* visible to other files in the same package. The
	// Exported gate was conflating "cross-package visibility" with
	// "same-package visibility". Only the cross-package paths (the
	// qualified-name and global-exported branches below) need the
	// Exported check.
	//
	// v26.58 (D101): steps 2 to 4 took the first match in map order, so the
	// answer changed between runs when two files matched. They now walk the
	// files, or the symbols, sorted by path or ID and take the first match.
	files := make([]string, 0, len(idx.tables))
	for fp := range idx.tables {
		files = append(files, fp)
	}
	sort.Strings(files)

	fromDir := filepath.Dir(fromFile)
	for _, fp := range files {
		if filepath.Dir(fp) == fromDir && fp != fromFile {
			if syms := idx.tables[fp].Lookup(name); len(syms) > 0 {
				return syms[0], true
			}
		}
	}

	// 3. Qualified: pkg.Name — look up by import in fromFile
	if dot := strings.Index(name, "."); dot >= 0 {
		pkg := name[:dot]
		localName := name[dot+1:]
		importPath := idx.resolveImportPath(pkg, fromFile)
		if importPath != "" {
			for _, fp := range files {
				if strings.HasSuffix(fp, importPath) || strings.Contains(fp, importPath) {
					if syms := idx.tables[fp].Lookup(localName); len(syms) > 0 && syms[0].Exported {
						return syms[0], true
					}
				}
			}
		}
	}

	// 4. Global exported
	var global []*Symbol
	for _, s := range idx.allSymbols {
		if s.Name == name && s.Exported {
			global = append(global, s)
		}
	}
	if len(global) > 0 {
		sort.Slice(global, func(i, j int) bool { return global[i].ID < global[j].ID })
		return global[0], true
	}
	return nil, false
}

// resolveImportPath returns the import path for an alias in a file.
func (idx *Index) resolveImportPath(alias, fromFile string) string {
	t, ok := idx.tables[fromFile]
	if !ok {
		return ""
	}
	// v26.58 (D101): an aliased import is bound only to its alias, so the last
	// segment of its path must not match (htmpl "html/template" is not
	// "template"). An exact name match wins over a last segment match, and
	// t.All is sorted by ID, so the answer no longer depends on map order.
	lastSegment := ""
	for _, s := range t.All() {
		if s.Kind != SymImport {
			continue
		}
		if s.Name == alias {
			return s.QualName
		}
		if s.ImportAlias == "" && lastSegment == "" {
			parts := strings.Split(s.QualName, "/")
			if parts[len(parts)-1] == alias {
				lastSegment = s.QualName
			}
		}
	}
	return lastSegment
}

// References returns all known reference sites for a symbol.
func (idx *Index) References(symbolID string) []Reference {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.refs[symbolID]
}

// Callers returns all symbols that call the given symbolID.
func (idx *Index) Callers(symbolID string) []*Symbol {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	seen := map[string]bool{}
	var out []*Symbol
	for _, e := range idx.callEdges {
		if e.CalleeID == symbolID && !seen[e.CallerID] {
			seen[e.CallerID] = true
			if s, ok := idx.allSymbols[e.CallerID]; ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// Callees returns all symbols called by the given symbolID.
func (idx *Index) Callees(symbolID string) []*Symbol {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	seen := map[string]bool{}
	var out []*Symbol
	for _, e := range idx.callEdges {
		if e.CallerID == symbolID && !seen[e.CalleeID] && e.CalleeID != "" {
			seen[e.CalleeID] = true
			if s, ok := idx.allSymbols[e.CalleeID]; ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// ImpactSet returns the transitive set of callers — "what breaks
// if I change this symbol?"
func (idx *Index) ImpactSet(symbolID string) []*Symbol {
	visited := map[string]bool{}
	var out []*Symbol
	var walk func(id string)
	walk = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		for _, caller := range idx.Callers(id) {
			out = append(out, caller)
			walk(caller.ID)
		}
	}
	walk(symbolID)
	return out
}

// TableFor returns the symbol table for a specific file.
func (idx *Index) TableFor(filePath string) (*Table, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	t, ok := idx.tables[filePath]
	return t, ok
}

// AllSymbols returns a copy of all symbols.
func (idx *Index) AllSymbols() []*Symbol {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	out := make([]*Symbol, 0, len(idx.allSymbols))
	for _, s := range idx.allSymbols {
		out = append(out, s)
	}
	return out
}

// AllCallEdges returns all call edges (caller→callee).
func (idx *Index) AllCallEdges() []CallEdge {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	cp := make([]CallEdge, len(idx.callEdges))
	copy(cp, idx.callEdges)
	return cp
}

// ─── Persistence ─────────────────────────────────────────────

type persistedIndex struct {
	Symbols   []*Symbol  `json:"symbols"`
	CallEdges []CallEdge `json:"call_edges"`
}

// Save writes the index to a JSON file at path.
func (idx *Index) Save(path string) error {
	idx.mu.RLock()
	pi := persistedIndex{
		Symbols:   idx.AllSymbols(),
		CallEdges: idx.AllCallEdges(),
	}
	idx.mu.RUnlock()

	data, err := json.MarshalIndent(pi, "", "  ")
	if err != nil {
		return fmt.Errorf("symbols.Index.Save: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Load reads a previously saved index from path.
// Returns an empty Index if the file does not exist.
func Load(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return NewIndex(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("symbols.Load: %w", err)
	}
	var pi persistedIndex
	if err := json.Unmarshal(data, &pi); err != nil {
		return nil, fmt.Errorf("symbols.Load: unmarshal: %w", err)
	}
	idx := NewIndex()
	for _, s := range pi.Symbols {
		// Rebuild allSymbols; tables are empty (re-built on re-parse)
		idx.allSymbols[s.ID] = s
	}
	idx.callEdges = pi.CallEdges
	return idx, nil
}

// IndexPath is the default persistence location.
const IndexPath = ".gen/symbols.json"

// fileFromSymbolID extracts the file path component from a SymbolID.
// SymbolID format: "<filePath>#<kind>:<qualName>".
// Returns "" for malformed IDs (no '#' or empty path).
//
// Originally lived in symbols/index_removefile.go (now consolidated here
// to honor the "one file per package symbol" rule). Used by RemoveFile's
// call-edge pruning and by tests.
func fileFromSymbolID(id string) string {
	i := strings.IndexByte(id, '#')
	if i <= 0 {
		return ""
	}
	return id[:i]
}
