// v23.37: added GET /api/present/source?sym=<id> — full contiguous source of a
// symbol (read-only, bounded to its span) so the Studio can show real code
// behind a graph node / CFG. See handleSource.
// v23.7: collapse consecutive duplicate source lines (assign+call on one line).
// v23.4: GET /api/present/flow now returns BlockEntry.Lines — each statement as
// its real source line (read from the func's file via Span), so the CFG shows
// actual Go code instead of coarse "call X" categories. Legacy Stmts kept.
// v20.3: file browser uses browseRoot; expandable tree support unchanged server-side. (v20: /api/present/files, SetRoot, copybook parse, dir guard.)
// presentation.go — TASK 1 (v19): added OnFileChanged hook, sync.RWMutex on
// cfgs/efx maps, and broker field so SSE push works from the watcher goroutine.
package presentation

// ─────────────────────────────────────────────────────────────
// presentation — HTTP API for human-in-the-loop code review
//
// This is Phase 4: the "present layer" that makes the system's
// understanding visible and correctable by a human BEFORE any
// code changes are made.
//
// Endpoints:
//   GET  /api/present/symbols?file=<path>
//        → SymbolMapResponse: all symbols in that file + unknowns
//
//   GET  /api/present/flow?func=<symbolID>
//        → FlowViewResponse: CFG + dataflow issues + effects
//
//   GET  /api/present/beliefs
//        → BeliefViewResponse: architectural beliefs + corrections
//
//   POST /api/present/beliefs/correct
//        → Accepts a BeliefCorrection, persists it
//
//   GET  /api/present/diff?since=<checksum>
//        → DiffViewResponse: what understanding changed since
// ─────────────────────────────────────────────────────────────

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gen-system/generator/internal/ast"
	"gen-system/generator/internal/flow"
	"gen-system/generator/internal/symbols"
)

// ─── Response Types ───────────────────────────────────────────

// SymbolEntry is one symbol in the symbol map view.
type SymbolEntry struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Language   string `json:"language"`
	Layer      string `json:"layer"`
	Exported   bool   `json:"exported"`
	Line       int    `json:"line"`
	DocComment string `json:"doc,omitempty"`
}

// UnknownEntry is a region the parser could not fully understand.
type UnknownEntry struct {
	Reason    string `json:"reason"`
	Line      int    `json:"line"`
	RawSource string `json:"raw_source,omitempty"`
}

// SymbolMapResponse is returned by GET /api/present/symbols.
type SymbolMapResponse struct {
	File     string         `json:"file"`
	Language string         `json:"language"`
	Symbols  []SymbolEntry  `json:"symbols"`
	Unknowns []UnknownEntry `json:"unknowns"`
}

// StmtLine is one statement rendered with its real source text and line number,
// so the CFG shows actual code (v23.4) rather than a coarse "call X" category.
type StmtLine struct {
	Line int    `json:"line"`           // 1-based source line (0 if unknown)
	Text string `json:"text"`           // the real source line, trimmed
	Kind string `json:"kind,omitempty"` // node kind, for optional styling
}

// BlockEntry is one basic block in a CFG view.
type BlockEntry struct {
	ID    int        `json:"id"`
	Label string     `json:"label"`
	Cond  string     `json:"cond,omitempty"` // v23.39 branch condition text (if/switch blocks)
	Succs []int      `json:"succs"`
	Preds []int      `json:"preds"`
	Stmts []string   `json:"stmts"`           // legacy category descriptions
	Lines []StmtLine `json:"lines,omitempty"` // v23.4 real source per statement
}

// FlowViewResponse is returned by GET /api/present/flow.
type FlowViewResponse struct {
	FuncID  string              `json:"func_id"`
	Blocks  []BlockEntry        `json:"blocks"`
	Entry   int                 `json:"entry"`
	Exits   []int               `json:"exits"`
	Issues  []DataFlowIssueView `json:"issues"`
	Effects []string            `json:"effects"`
}

// DataFlowIssueView is a human-readable issue.
type DataFlowIssueView struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Line    int    `json:"line"`
}

// BeliefKind classifies the type of architectural belief.
type BeliefKind string

const (
	BeliefLayer     BeliefKind = "layer"      // "I believe X is in layer Y"
	BeliefOwnership BeliefKind = "ownership"  // "I believe entity X owns table Y"
	BeliefContract  BeliefKind = "contract"   // "I believe function X has effect Y"
	BeliefDependency BeliefKind = "dependency" // "I believe X depends on Y"
)

// Belief is one architectural belief held by the system.
type Belief struct {
	ID         string     `json:"id"`
	Kind       BeliefKind `json:"kind"`
	Subject    string     `json:"subject"`    // e.g. "CreateUser"
	Predicate  string     `json:"predicate"`  // e.g. "has_layer"
	Object     string     `json:"object"`     // e.g. "handler"
	Confidence float64    `json:"confidence"` // 0.0 – 1.0
	// HumanVerified is true if a human confirmed or corrected this.
	HumanVerified bool   `json:"human_verified"`
	CorrectedTo   string `json:"corrected_to,omitempty"`
}

// BeliefViewResponse is returned by GET /api/present/beliefs.
type BeliefViewResponse struct {
	Beliefs            []Belief `json:"beliefs"`
	ArchSummary        string   `json:"arch_summary"`
	PendingReview      int      `json:"pending_review"`
	TotalBeliefs       int      `json:"total_beliefs"`
}

// BeliefCorrection is the payload for POST /api/present/beliefs/correct.
type BeliefCorrection struct {
	BeliefID    string `json:"belief_id"`
	CorrectedTo string `json:"corrected_to"`
	HumanNote   string `json:"human_note,omitempty"`
}

// DiffEntry is one changed understanding.
type DiffEntry struct {
	Kind    string `json:"kind"`    // "added", "removed", "changed"
	Subject string `json:"subject"` // symbol name or file
	Before  string `json:"before,omitempty"`
	After   string `json:"after,omitempty"`
}

// DiffViewResponse is returned by GET /api/present/diff.
type DiffViewResponse struct {
	Changes []DiffEntry `json:"changes"`
	Since   string      `json:"since"`
	Now     string      `json:"now"`
}

// ─── Server ───────────────────────────────────────────────────

// SSEPublisher is the subset of sse.Broker that presentation.Server needs.
// Accepting an interface avoids a circular import with the api/sse package.
type SSEPublisher interface {
	Publish(eventType string, payload interface{})
}

// Server holds the dependencies for all presentation endpoints.
type Server struct {
	idx     *symbols.Index
	mu      sync.RWMutex           // guards cfgs and efx
	cfgs    map[string]flow.CFGSet // filePath → CFGSet
	efx     map[string]*flow.FuncEffects
	beliefs *BeliefStore
	broker  SSEPublisher // may be nil; set via SetBroker

	// v20: project root for the read-only file browser (/api/present/files)
	// and copybook search path so on-demand COBOL parsing resolves COPY books.
	root         string
	copybookPath string
	// v20.3: browseRoot bounds the file browser. Defaults to root, but can be
	// set wider (e.g. a parent dir holding both the engine and a COBOL demo)
	// so the Understand tab can reach sibling projects without a restart.
	browseRoot string

	// v26.5: what the stdlib pass attached / skipped, surfaced on Architecture
	// so the edge-resolution % explains itself. Stored as a LOCAL DTO rather
	// than importing internal/context here: presentation is a read-only view
	// layer and must not take a dependency on the loader (the understand-server
	// boundary test walks these imports). The caller mirrors the loader's report
	// into this shape via SetStdlibReport.
	stdlibReport StdlibReportView
}

// StdlibRootView is one root's outcome as rendered by the Understand UI.
type StdlibRootView struct {
	Lang     string `json:"lang"`
	Path     string `json:"path"`
	Status   string `json:"status"` // attached | skipped | missing
	Detail   string `json:"detail"`
	Packages int    `json:"packages"`
	Symbols  int    `json:"symbols"`
	Auto     bool   `json:"auto"`
}

// StdlibReportView is the whole stdlib outcome for the loaded project.
type StdlibReportView struct {
	Mode  string           `json:"mode"` // auto | explicit | auto+explicit | none
	Roots []StdlibRootView `json:"roots"`
}

// SetStdlibReport records the stdlib outcome for the Architecture page. Safe to
// call before serving; guarded by the same lock as the rest of the hot-swappable
// state so a dynamic project load can replace it.
func (s *Server) SetStdlibReport(r StdlibReportView) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stdlibReport = r
}

// StdlibReport returns the recorded stdlib outcome.
func (s *Server) StdlibReport() StdlibReportView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stdlibReport
}

// New creates a presentation Server.
func New(idx *symbols.Index) *Server {
	return &Server{
		idx:     idx,
		cfgs:    make(map[string]flow.CFGSet),
		efx:     make(map[string]*flow.FuncEffects),
		beliefs: newBeliefStore(),
	}
}

// SetRoot records the project root used by the file-browser endpoint and
// (optionally) the copybook search path for on-demand COBOL parsing.
// v20: called by gen-server after construction. Safe to pass "" for either.
// v20.3: browseRoot widens the file browser's reach; pass "" to default it
// to root.
func (s *Server) SetRoot(root, copybookPath, browseRoot string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if abs, err := filepath.Abs(root); err == nil {
		s.root = abs
	} else {
		s.root = root
	}
	s.copybookPath = copybookPath
	if browseRoot == "" {
		s.browseRoot = s.root
	} else if abs, err := filepath.Abs(browseRoot); err == nil {
		s.browseRoot = abs
	} else {
		s.browseRoot = browseRoot
	}
}

// SetBroker wires an SSE publisher so OnFileChanged can push events to
// connected Studio clients. Call once after construction, before Run.
func (s *Server) SetBroker(b SSEPublisher) {
	s.broker = b
}

// index returns the current symbol index under the read lock. v24: all handlers
// read the index through this accessor so a live project swap (ReplaceContext)
// is race-free. The returned pointer is a snapshot; callers must not retain it
// across a lock boundary they don't hold.
func (s *Server) index() *symbols.Index {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.idx
}

// ReplaceContext atomically swaps the entire analysis context — symbol index,
// CFGs, and effects — for a freshly loaded project. v24: this is the backend of
// the Studio "load / clear project" (dynamic --root) feature. Everything is
// swapped under the write lock so in-flight reads (via index() and the cfgs/efx
// maps) see a consistent old-or-new snapshot, never a torn mix. Beliefs are
// rebuilt from the new index (corrections are keyed by symbol and simply won't
// match the new project's symbols — they are not lost, just inert until a
// matching project is loaded again). Passing a nil idx clears the project
// (empty state).
func (s *Server) ReplaceContext(idx *symbols.Index, cfgs map[string]flow.CFGSet, efx map[string]*flow.FuncEffects) {
	s.mu.Lock()
	if idx == nil {
		idx = symbols.NewIndex()
	}
	if cfgs == nil {
		cfgs = make(map[string]flow.CFGSet)
	}
	if efx == nil {
		efx = make(map[string]*flow.FuncEffects)
	}
	s.idx = idx
	s.cfgs = cfgs
	s.efx = efx
	s.mu.Unlock()
	// Rebuild beliefs from the new index outside the lock; rebuildFromIndex takes
	// its own synchronization and reads a fresh efx copy.
	s.beliefs.rebuildFromIndex(s.index(), s.safeEfxCopy())
}

// OnFileChanged is the hook called by the file watcher after it has
// updated the symbol index for path. It refreshes the Server's cached
// CFGs/effects for the file, rebuilds the belief store, and pushes an
// SSE "file_changed" event to all connected clients.
//
// It must not block (called from the watcher goroutine).
func (s *Server) OnFileChanged(path string, cfgSet flow.CFGSet, effects map[string]*flow.FuncEffects, deleted bool) {
	s.mu.Lock()
	if deleted {
		delete(s.cfgs, path)
		// Remove per-symbol effects whose key starts with path (best-effort).
		for id := range s.efx {
			if strings.HasPrefix(id, path) {
				delete(s.efx, id)
			}
		}
	} else {
		s.cfgs[path] = cfgSet
		for id, e := range effects {
			s.efx[id] = e
		}
	}
	s.mu.Unlock()

	// Rebuild beliefs from the (now-updated) index.
	s.beliefs.rebuildFromIndex(s.index(), s.safeEfxCopy())

	if s.broker != nil {
		s.broker.Publish("file_changed", map[string]interface{}{
			"path":    path,
			"deleted": deleted,
		})
	}
}

// safeEfxCopy returns a read-safe snapshot of s.efx.
func (s *Server) safeEfxCopy() map[string]*flow.FuncEffects {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]*flow.FuncEffects, len(s.efx))
	for k, v := range s.efx {
		cp[k] = v
	}
	return cp
}

// AddFlowData provides flow analysis results (call after Build).
func (s *Server) AddFlowData(filePath string, cfgSet flow.CFGSet, efx map[string]*flow.FuncEffects) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfgs[filePath] = cfgSet
	for id, e := range efx {
		s.efx[id] = e
	}
}

// RegisterRoutes wires all presentation endpoints into mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/present/symbols", s.handleSymbols)
	mux.HandleFunc("/api/present/files", s.handleFiles)
	mux.HandleFunc("/api/present/flow", s.handleFlow)
	mux.HandleFunc("/api/present/source", s.handleSource) // v23.37: full source of a symbol
	mux.HandleFunc("/api/present/beliefs", s.handleBeliefs)
	mux.HandleFunc("/api/present/beliefs/correct", s.handleCorrect)
	mux.HandleFunc("/api/present/diff", s.handleDiff)
}

// ─── Symbol Map ───────────────────────────────────────────────

func (s *Server) handleSymbols(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("file")
	if filePath == "" {
		http.Error(w, "file param required", http.StatusBadRequest)
		return
	}
	// v20.1: guard against a directory being sent here (e.g. pasted into the
	// manual path box). Reading a dir as a file yields a confusing
	// "permission denied" — reject it cleanly and tell the client to browse.
	if fi, statErr := os.Stat(filePath); statErr == nil && fi.IsDir() {
		http.Error(w, fmt.Sprintf("%s is a directory — open it in the browser, then pick a file", filePath), http.StatusBadRequest)
		return
	}

	idx := s.index() // v24: guarded snapshot; consistent within this request
	t, ok := idx.TableFor(filePath)
	if !ok {
		// Try re-parsing on-demand. v20: COBOL files (esp. copybooks) need a
		// copybook-aware frontend or COPY resolution fails and the module
		// comes back with no indexable symbols.
		var m *ast.Module
		var err error
		if isCobolPath(filePath) {
			fe := &ast.CobolFrontend{}
			if s.copybookPath != "" {
				fe.CopybookPath = []string{s.copybookPath}
			} else {
				// default: look beside the file for copybooks
				fe.CopybookPath = []string{filepath.Dir(filePath)}
			}
			data, rerr := os.ReadFile(filePath)
			if rerr != nil {
				http.Error(w, fmt.Sprintf("cannot read %s: %v", filePath, rerr), http.StatusNotFound)
				return
			}
			m, err = fe.Parse(filePath, data)
		} else {
			m, err = ast.DefaultCache.ParseOrCache(filePath)
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("parse error in %s: %v", filePath, err), http.StatusUnprocessableEntity)
			return
		}
		if m == nil {
			http.Error(w, fmt.Sprintf("unsupported file type: %s", filePath), http.StatusUnsupportedMediaType)
			return
		}
		idx.AddModule(m)
		t, _ = idx.TableFor(filePath)
	}

	resp := SymbolMapResponse{
		File:     filePath,
		Language: string(ast.DetectLanguage(filePath)),
	}

	if t != nil {
		syms := t.All()
		sort.Slice(syms, func(i, j int) bool {
			return syms[i].Span.Start.Line < syms[j].Span.Start.Line
		})
		for _, sym := range syms {
			if sym.Kind == symbols.SymModule {
				continue
			}
			resp.Symbols = append(resp.Symbols, SymbolEntry{
				ID:         sym.ID,
				Name:       sym.QualName,
				Kind:       string(sym.Kind),
				Language:   string(sym.Language),
				Layer:      sym.Layer,
				Exported:   sym.Exported,
				Line:       sym.Span.Start.Line,
				DocComment: sym.DocComment,
			})
		}
	}

	// Surface unknown nodes from the cached module
	if m, ok2 := ast.DefaultCache.Get(filePath, ast.ChecksumFile(filePath)); ok2 {
		for _, un := range m.UnknownNodes() {
			resp.Unknowns = append(resp.Unknowns, UnknownEntry{
				Reason:    un.UnknownReason,
				Line:      un.Span.Start.Line,
				RawSource: trimRaw(un.RawSource, 120),
			})
		}
	}

	writeJSON(w, resp)
}

// ─── Flow View ────────────────────────────────────────────────

// isCobolPath reports whether path has a COBOL/copybook extension.
func isCobolPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cob", ".cbl", ".cpy", ".cobol":
		return true
	}
	return false
}

// fileEntry is one item in a directory listing.
type fileEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"` // absolute, server-side
	IsDir   bool   `json:"is_dir"`
	Lang    string `json:"lang,omitempty"`
	Size    int64  `json:"size,omitempty"`
	Sources bool   `json:"sources,omitempty"` // dir contains parseable sources (best-effort)
}

// handleFiles is a read-only directory browser rooted at s.root. It powers
// the Understand-tab file picker so users click instead of typing paths.
//
//	GET /api/present/files            → list s.root
//	GET /api/present/files?dir=<abs>  → list that dir (must be inside s.root)
//
// Path traversal outside root is rejected. Hidden dirs and node_modules /
// vendor / .git are skipped to keep the tree usable.
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	base := s.browseRoot
	if base == "" {
		base = s.root
	}
	if base == "" {
		http.Error(w, "server started without a project root", http.StatusServiceUnavailable)
		return
	}
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = base
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		http.Error(w, "bad dir", http.StatusBadRequest)
		return
	}
	// Containment check: abs must equal base or be under it.
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		http.Error(w, "path outside browse root", http.StatusForbidden)
		return
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	out := struct {
		Root    string      `json:"root"`
		Dir     string      `json:"dir"`
		Parent  string      `json:"parent,omitempty"`
		Entries []fileEntry `json:"entries"`
	}{Root: base, Dir: abs}
	if abs != base {
		out.Parent = filepath.Dir(abs)
	}
	for _, de := range ents {
		name := de.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
			continue
		}
		fe := fileEntry{Name: name, Path: filepath.Join(abs, name), IsDir: de.IsDir()}
		if !de.IsDir() {
			lang := ast.DetectLanguage(fe.Path)
			if lang == ast.LangUnknown {
				// Skip non-source files entirely to keep the picker focused.
				continue
			}
			fe.Lang = string(lang)
			if info, ierr := de.Info(); ierr == nil {
				fe.Size = info.Size()
			}
		}
		out.Entries = append(out.Entries, fe)
	}
	// Dirs first, then alpha.
	sort.Slice(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return a.Name < b.Name
	})
	writeJSON(w, out)
}


// v24: methodsWithCFGFor returns the methods of a type that DO have a CFG, so the
// flow panel can offer them when a type (which has none) is selected. Each entry
// is {id, name} — the UI turns them into clickable links.
func (s *Server) methodsWithCFGFor(typeSym *symbols.Symbol) []map[string]string {
	if typeSym == nil {
		return nil
	}
	idx := s.index()
	s.mu.RLock()
	cfgs := s.cfgs
	hasCFG := func(id string) bool {
		for _, set := range cfgs {
			if _, ok := set[id]; ok {
				return true
			}
		}
		return false
	}
	var out []map[string]string
	for _, sym := range idx.AllSymbols() {
		if sym.Kind == symbols.SymMethod && sym.ReceiverType == typeSym.Name && hasCFG(sym.ID) {
			out = append(out, map[string]string{"id": sym.ID, "name": sym.Name})
		}
	}
	s.mu.RUnlock()
	return out
}

func (s *Server) handleFlow(w http.ResponseWriter, r *http.Request) {
	funcID := r.URL.Query().Get("func")
	if funcID == "" {
		http.Error(w, "func param required", http.StatusBadRequest)
		return
	}

	// Find the CFG — take a read lock while scanning the map.
	s.mu.RLock()
	var cfg *flow.CFG
	for _, cfgSet := range s.cfgs {
		if c, ok := cfgSet[funcID]; ok {
			cfg = c
			break
		}
	}
	s.mu.RUnlock()

	if cfg == nil {
		// v24: distinguish "no CFG because this symbol kind never has one" (a
		// type, field, const, var, import, module) from "a function whose CFG is
		// genuinely missing". Look the symbol up so the UI can show a meaningful
		// message instead of a bare 404. Types get a 200 with an explanatory
		// payload + the methods that DO have CFGs, so the panel is useful.
		sym, ok := s.index().ByID(funcID)
		if !ok || sym == nil {
			writeProjectJSON(w, http.StatusNotFound, map[string]interface{}{
				"error":  "unknown_symbol",
				"reason": "No symbol matches this id. It may have been removed, or the project was swapped since this link was made.",
				"func":   funcID,
			})
			return
		}
		switch sym.Kind {
		case symbols.SymType:
			// Gather this type's methods that DO have CFGs so the UI can offer them.
			methods := s.methodsWithCFGFor(sym)
			writeProjectJSON(w, http.StatusOK, map[string]interface{}{
				"noCFG":   true,
				"kind":    string(sym.Kind),
				"name":    sym.Name,
				"reason":  sym.Name + " is a type (struct/class/interface/enum). Control-flow graphs are built for functions and methods, not for a type itself.",
				"hint":    "Select one of its methods to see control flow.",
				"methods": methods,
			})
			return
		case symbols.SymVar, symbols.SymConst, symbols.SymField, symbols.SymParam:
			writeProjectJSON(w, http.StatusOK, map[string]interface{}{
				"noCFG":  true,
				"kind":   string(sym.Kind),
				"name":   sym.Name,
				"reason": sym.Name + " is a " + string(sym.Kind) + " — it holds data, not control flow. CFGs exist only for functions and methods.",
			})
			return
		case symbols.SymImport, symbols.SymModule:
			writeProjectJSON(w, http.StatusOK, map[string]interface{}{
				"noCFG":  true,
				"kind":   string(sym.Kind),
				"name":   sym.Name,
				"reason": sym.Name + " is a " + string(sym.Kind) + " — no control flow to show.",
			})
			return
		default:
			// A function/method that SHOULD have a CFG but doesn't — this is the
			// genuinely unexpected case (parser produced no body, or it's an
			// external/stdlib signature with no source body).
			writeProjectJSON(w, http.StatusOK, map[string]interface{}{
				"noCFG":  true,
				"kind":   string(sym.Kind),
				"name":   sym.Name,
				"reason": sym.Name + " has no control-flow graph. This usually means the body is empty, external (a signature only, e.g. an stdlib or interface method), or the parser could not model it.",
			})
			return
		}
	}

	idxCFG := s.index() // v24: guarded snapshot
	sym, _ := idxCFG.ByID(funcID)
	var table *symbols.Table
	if sym != nil {
		table, _ = idxCFG.TableFor(sym.DefinedIn)
	}

	// v23.4 — load the function's source file once so each CFG statement can be
	// rendered as its real source line (keyed by Span), instead of the coarse
	// "call X" category that made Go read like flat procedural code.
	var srcLines []string
	if sym != nil && sym.DefinedIn != "" {
		if data, err := os.ReadFile(sym.DefinedIn); err == nil {
			srcLines = strings.Split(string(data), "\n")
		}
	}
	sourceAt := func(n *ast.Node) (int, string) {
		if n == nil {
			return 0, ""
		}
		ln := n.Span.Start.Line
		if ln >= 1 && ln <= len(srcLines) {
			return ln, strings.TrimSpace(srcLines[ln-1])
		}
		return ln, ""
	}

	resp := FlowViewResponse{
		FuncID: funcID,
		Entry:  cfg.Entry,
		Exits:  cfg.Exits,
	}
	for _, b := range cfg.Blocks {
		be := BlockEntry{
			ID:    b.ID,
			Label: b.Label,
			Cond:  b.Cond,
			Succs: b.Succs,
			Preds: b.Preds,
		}
		for _, stmt := range b.Stmts {
			desc := nodeDescription(stmt)
			be.Stmts = append(be.Stmts, desc)
			ln, src := sourceAt(stmt)
			text := src
			if text == "" {
				text = desc // fall back to the category when no source is available
			}
			// v23.7 — collapse consecutive duplicate source lines (an assignment
			// emits both a NodeAssign and a NodeCall for the same RHS line).
			if k := len(be.Lines); k > 0 && be.Lines[k-1].Line == ln && be.Lines[k-1].Text == text {
				continue
			}
			be.Lines = append(be.Lines, StmtLine{Line: ln, Text: text, Kind: string(stmt.Kind)})
		}
		resp.Blocks = append(resp.Blocks, be)
	}

	// Dataflow issues
	issues := flow.AnalyzeDataFlow(cfg, table)
	for _, iss := range issues {
		resp.Issues = append(resp.Issues, DataFlowIssueView{
			Kind:    iss.Kind,
			Message: iss.Message,
			Line:    iss.Span.Start.Line,
		})
	}

	// Effects — read under lock.
	s.mu.RLock()
	if e, ok := s.efx[funcID]; ok {
		for eff := range e.Effects {
			resp.Effects = append(resp.Effects, string(eff))
		}
	}
	s.mu.RUnlock()
	sort.Strings(resp.Effects)

	writeJSON(w, resp)
}

// ─── Source View ──────────────────────────────────────────────
// v23.37: GET /api/present/source?sym=<symbolID> returns the full contiguous
// source of a symbol, read from its DefinedIn file between the symbol's span
// lines. This is what lets the Studio show the REAL code behind a graph node or
// CFG — not just the per-statement fragments the flow endpoint already emits.
// Read-only, bounded to the symbol's own span, never writes.

// SourceLine is one numbered line of source.
type SourceLine struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

// SourceResponse is returned by GET /api/present/source.
type SourceResponse struct {
	SymbolID  string       `json:"symbol_id"`
	Name      string       `json:"name"`
	Kind      string       `json:"kind"`
	Language  string       `json:"language"`
	File      string       `json:"file"`
	StartLine int          `json:"start_line"`
	EndLine   int          `json:"end_line"`
	Lines     []SourceLine `json:"lines"`
}

func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) {
	symID := r.URL.Query().Get("sym")
	if symID == "" {
		http.Error(w, "sym param required", http.StatusBadRequest)
		return
	}
	idxSrc := s.index() // v24: guarded snapshot
	if idxSrc == nil {
		http.Error(w, "no index loaded", http.StatusNotFound)
		return
	}
	sym, ok := idxSrc.ByID(symID)
	if !ok || sym == nil {
		http.Error(w, "symbol not found", http.StatusNotFound)
		return
	}
	if sym.DefinedIn == "" {
		http.Error(w, "symbol has no source file", http.StatusNotFound)
		return
	}
	data, err := os.ReadFile(sym.DefinedIn)
	if err != nil {
		http.Error(w, "cannot read source: "+err.Error(), http.StatusNotFound)
		return
	}
	fileLines := strings.Split(string(data), "\n")

	start := sym.Span.Start.Line
	end := sym.Span.End.Line
	// Defensive clamping: a symbol with a zero or inverted span (some frontends
	// emit only a start line) falls back to a small window from the start so the
	// panel still shows something useful rather than erroring.
	if start < 1 {
		start = 1
	}
	if end < start {
		end = start + 40
	}
	if end > len(fileLines) {
		end = len(fileLines)
	}
	if start > len(fileLines) {
		start = len(fileLines)
	}

	resp := SourceResponse{
		SymbolID:  symID,
		Name:      sym.Name,
		Kind:      string(sym.Kind),
		Language:  string(sym.Language),
		File:      sym.DefinedIn,
		StartLine: start,
		EndLine:   end,
	}
	for i := start; i <= end; i++ {
		resp.Lines = append(resp.Lines, SourceLine{Line: i, Text: fileLines[i-1]})
	}
	writeJSON(w, resp)
}

// ─── Belief View ──────────────────────────────────────────────

func (s *Server) handleBeliefs(w http.ResponseWriter, r *http.Request) {
	s.beliefs.rebuildFromIndex(s.index(), s.safeEfxCopy())
	beliefs := s.beliefs.All()
	pending := 0
	for _, b := range beliefs {
		if !b.HumanVerified {
			pending++
		}
	}
	resp := BeliefViewResponse{
		Beliefs:       beliefs,
		ArchSummary:   s.buildArchSummary(),
		PendingReview: pending,
		TotalBeliefs:  len(beliefs),
	}
	writeJSON(w, resp)
}

func (s *Server) handleCorrect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var correction BeliefCorrection
	if err := json.NewDecoder(r.Body).Decode(&correction); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.beliefs.Correct(correction); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// ─── Diff View ────────────────────────────────────────────────

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	changes := s.beliefs.DiffSince(since)
	resp := DiffViewResponse{
		Changes: changes,
		Since:   since,
		Now:     s.beliefs.CurrentChecksum(),
	}
	writeJSON(w, resp)
}

// ─── BeliefStore ──────────────────────────────────────────────

// BeliefStore persists and manages architectural beliefs.
type BeliefStore struct {
	beliefs     map[string]*Belief // belief ID → Belief
	corrections map[string]BeliefCorrection
	diskPath    string
	checksum    string
}

func newBeliefStore() *BeliefStore {
	bs := &BeliefStore{
		beliefs:     make(map[string]*Belief),
		corrections: make(map[string]BeliefCorrection),
		diskPath:    ".gen/beliefs.json",
	}
	bs.load()
	return bs
}

func (bs *BeliefStore) rebuildFromIndex(idx *symbols.Index, efx map[string]*flow.FuncEffects) {
	allSyms := idx.AllSymbols()

	for _, sym := range allSyms {
		// Layer belief
		if sym.Layer != "" {
			id := "layer:" + sym.ID
			b := &Belief{
				ID:         id,
				Kind:       BeliefLayer,
				Subject:    sym.QualName,
				Predicate:  "has_layer",
				Object:     sym.Layer,
				Confidence: 0.85,
			}
			if c, ok := bs.corrections[id]; ok {
				b.CorrectedTo = c.CorrectedTo
				b.HumanVerified = true
			}
			bs.beliefs[id] = b
		}

		// Effect belief
		if e, ok := efx[sym.ID]; ok {
			for eff := range e.Effects {
				id := "effect:" + sym.ID + ":" + string(eff)
				b := &Belief{
					ID:         id,
					Kind:       BeliefContract,
					Subject:    sym.QualName,
					Predicate:  "has_effect",
					Object:     string(eff),
					Confidence: 0.75,
				}
				if c, ok2 := bs.corrections[id]; ok2 {
					b.CorrectedTo = c.CorrectedTo
					b.HumanVerified = true
				}
				bs.beliefs[id] = b
			}
		}
	}

	// Dependency beliefs from call edges
	for _, edge := range idx.AllCallEdges() {
		if edge.CalleeID == "" {
			continue
		}
		id := "dep:" + edge.CallerID + "->" + edge.CalleeID
		b := &Belief{
			ID:         id,
			Kind:       BeliefDependency,
			Subject:    edge.CallerID,
			Predicate:  "calls",
			Object:     edge.CalleeID,
			Confidence: 0.95,
		}
		if c, ok := bs.corrections[id]; ok {
			b.CorrectedTo = c.CorrectedTo
			b.HumanVerified = true
		}
		bs.beliefs[id] = b
	}

	bs.computeChecksum()
	bs.save()
}

func (bs *BeliefStore) All() []Belief {
	out := make([]Belief, 0, len(bs.beliefs))
	for _, b := range bs.beliefs {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (bs *BeliefStore) Correct(c BeliefCorrection) error {
	if _, ok := bs.beliefs[c.BeliefID]; !ok {
		return fmt.Errorf("belief %q not found", c.BeliefID)
	}
	bs.corrections[c.BeliefID] = c
	bs.beliefs[c.BeliefID].CorrectedTo = c.CorrectedTo
	bs.beliefs[c.BeliefID].HumanVerified = true
	bs.save()
	return nil
}

func (bs *BeliefStore) DiffSince(prevChecksum string) []DiffEntry {
	if prevChecksum == bs.checksum {
		return nil
	}
	// Simple diff: report all unverified beliefs as "changed"
	var changes []DiffEntry
	for _, b := range bs.beliefs {
		if !b.HumanVerified {
			changes = append(changes, DiffEntry{
				Kind:    "changed",
				Subject: b.Subject,
				After:   b.Object,
			})
		}
	}
	return changes
}

func (bs *BeliefStore) CurrentChecksum() string { return bs.checksum }

func (bs *BeliefStore) computeChecksum() {
	keys := make([]string, 0, len(bs.beliefs))
	for k := range bs.beliefs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(bs.beliefs[k].Object)
	}
	bs.checksum = fmt.Sprintf("%x", len(bs.beliefs)) + sb.String()[:min2(len(sb.String()), 16)]
}

type persistedBeliefs struct {
	Beliefs     map[string]*Belief              `json:"beliefs"`
	Corrections map[string]BeliefCorrection     `json:"corrections"`
}

func (bs *BeliefStore) save() {
	pb := persistedBeliefs{Beliefs: bs.beliefs, Corrections: bs.corrections}
	data, err := json.MarshalIndent(pb, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(bs.diskPath), 0755)
	_ = os.WriteFile(bs.diskPath, data, 0644)
}

func (bs *BeliefStore) load() {
	data, err := os.ReadFile(bs.diskPath)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		return
	}
	var pb persistedBeliefs
	if err := json.Unmarshal(data, &pb); err != nil {
		return
	}
	if pb.Beliefs != nil {
		bs.beliefs = pb.Beliefs
	}
	if pb.Corrections != nil {
		bs.corrections = pb.Corrections
	}
}

// ─── Arch Summary ─────────────────────────────────────────────

func (s *Server) buildArchSummary() string {
	layerCounts := make(map[string]int)
	langCounts := make(map[string]int)
	for _, sym := range s.index().AllSymbols() {
		if sym.Layer != "" {
			layerCounts[sym.Layer]++
		}
		langCounts[string(sym.Language)]++
	}
	var sb strings.Builder
	sb.WriteString("Layers: ")
	for l, c := range layerCounts {
		sb.WriteString(fmt.Sprintf("%s(%d) ", l, c))
	}
	sb.WriteString("\nLanguages: ")
	for l, c := range langCounts {
		sb.WriteString(fmt.Sprintf("%s(%d) ", l, c))
	}
	return strings.TrimSpace(sb.String())
}

// ─── Helpers ──────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func nodeDescription(n *ast.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case ast.NodeCall:
		return "call " + n.Callee
	case ast.NodeReturn:
		return "return"
	case ast.NodeAssign:
		return "assign " + n.Name
	case ast.NodeBranch:
		return "branch"
	case ast.NodeLoop:
		return "loop"
	case ast.NodeUnknown:
		return "[unknown: " + n.UnknownReason + "]"
	default:
		if n.Name != "" {
			return string(n.Kind) + " " + n.Name
		}
		return string(n.Kind)
	}
}

func trimRaw(s string, max int) string {
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// LoadEffects bulk-loads a symbolID→FuncEffects map (e.g. from Context.Effects).
func (s *Server) LoadEffects(effects map[string]*flow.FuncEffects) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range effects {
		s.efx[id] = e
	}
}

// LoadAllCFGs bulk-loads all file CFGSets (e.g. from Context.CFGs).
func (s *Server) LoadAllCFGs(cfgs map[string]flow.CFGSet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for filePath, cfgSet := range cfgs {
		s.cfgs[filePath] = cfgSet
	}
}
