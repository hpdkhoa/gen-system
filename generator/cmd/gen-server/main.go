// v23.44: gen-server resolves the web UI dir to an absolute path (--web / binary-dir/web / ./web) so studio-v23.html is served deterministically regardless of launch dir.
// gen-server/main.go — v23.32 (stdlib provenance): --stdlib / --stdlib-lang
// flags; initial load goes through LoadContextWithOpts so the Understand views
// can resolve and display calls that cross into the standard library.
// gen-server/main.go — v23: banner now points at /studio-v23.html (redesigned UI).
// gen-server/main.go — TASK 1 (v19): start incremental file watcher after
// gen-server/main.go — v20: --copybooks flag; SetRoot wiring; banner points at studio.html.
// LoadContext; drain watcher Events channel to update presServer + push SSE.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/api"
	"github.com/hpdkhoa/gen-system/generator/internal/api/sse"
	"github.com/hpdkhoa/gen-system/generator/internal/belief"
	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/enrich"
	"github.com/hpdkhoa/gen-system/generator/internal/handlers"
	"github.com/hpdkhoa/gen-system/generator/internal/presentation"
	"github.com/hpdkhoa/gen-system/generator/internal/services"
	"github.com/hpdkhoa/gen-system/generator/internal/store"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
	"github.com/hpdkhoa/gen-system/generator/internal/watch"
)

func main() {
	port := flag.String("port", "28080", "HTTP server port")
	root := flag.String("root", ".", "Project root path")
	copybooks := flag.String("copybooks", "", "COBOL copybook search path (for COPY resolution in the Understand tab)")
	browseRoot := flag.String("browse-root", "", "wider directory the Understand-tab file browser may reach (default: --root). Use a parent dir to browse sibling projects like a COBOL demo.")
	stdlibSpec := flag.String("stdlib", "", "v26.5: comma-separated stdlib roots, language PER ROOT. 'auto' (default when omitted) detects a runtime for every language in the project; 'none' opts out. Shorthands: 'go' → GOROOT/src, 'cobol' → built-in runtime catalog. Explicit form: 'java=/opt/jdk/java.base'. Mixable: 'go,java=/opt/jdk/java.base'. All roots are import-scoped.")
	stdlibLang := flag.String("stdlib-lang", "", "DEPRECATED (v26.5): legacy single language label applied to bare --stdlib paths. Prefer the per-root 'lang=path' form, which is the only way to express a multi-language project.")
	githubEnabled := flag.Bool("github-enabled", false,
		"mount /api/github/* routes; default off — keeps the studio offline")
	webDir := flag.String("web", "",
		"directory holding the Studio UI (studio-v23.html). Default: <binary-dir>/web, else ./web. v23.44: pin this so the UI is served from a known place regardless of launch dir.")
	projectsBase := flag.String("projects-base", "",
		"v24: directory under which the Studio may dynamically load projects (/api/project/load). Default: parent of --root. Loads outside this tree are rejected.")
	enrichInline := flag.Bool("enrich-inline", false,
		"v26.22: enrich beliefs with the local model DURING each rebuild, instead of out-of-band via gen-enrich. "+
			"Costs a model call per routine on every re-parse, so it suits small projects and demos; prefer gen-enrich for real codebases. "+
			"A missing/unreachable model is not an error — the parser and human belief tiers carry on.")
	watchLive := flag.Bool("watch", false,
		"v24: start the file watcher live at boot (default STATIC: no live watching until switched on per-project in the Studio).")
	flag.Parse()

	broker := sse.NewBroker()
	snapStore := store.NewSnapshotStore(*root)
	snapSvc := services.NewSnapshotService(snapStore, broker, *root)
	buildSvc := services.NewBuildService(broker, snapSvc, *root)
	schemaSvc := services.NewSchemaService(*root)
	stagingSvc := services.NewStagingService(broker, snapSvc, *root)
	h := handlers.New(broker, snapSvc, buildSvc, schemaSvc, stagingSvc)

	// Full initial parse (idempotent, remains the authoritative first load).
	// v23.32: optional read-only stdlib ingest for cross-boundary resolution.
	loadOpts := agentctx.LoadContextOpts{}
	if *copybooks != "" {
		loadOpts.CopybookPath = filepath.SplitList(*copybooks)
	}
	// v26.5: one shared spec parser (context.ParseStdlibSpec) replaces the inline
	// per-binary special-casing of "go"/"cobol" that gen-server, understand-server
	// and genbench each carried a copy of (and could drift on). The language is now
	// a property of each ROOT ("java=/opt/jdk/java.base"), not of the flag, so a
	// multi-language project is finally expressible. An omitted --stdlib means
	// "auto": detect a runtime for every language actually in the project.
	roots, auto := agentctx.ParseStdlibSpec(*stdlibSpec, *stdlibLang)
	loadOpts.StdlibRoots = roots
	loadOpts.StdlibAuto = auto

	ctx := agentctx.LoadContextWithOpts(*root, loadOpts)
	for _, line := range agentctx.FormatStdlibReport(ctx.StdlibReport) {
		fmt.Println(line)
	}
	presServer := presentation.New(ctx.Index)
	presServer.LoadAllCFGs(ctx.CFGs)
	presServer.LoadEffects(ctx.Effects)
	presServer.SetBroker(broker)
	presServer.SetRoot(*root, *copybooks, *browseRoot)
	presServer.SetStdlibReport(stdlibReportView(ctx.StdlibReport))

	// v26.22: optional INLINE enrichment. gen-server is the generation-side binary,
	// so it may legitimately link internal/enrich; presentation may not (archtest
	// forbids it), which is why the wiring is inverted — we reach into the store and
	// inject rather than presentation importing the model. Off by default: enrichment
	// costs a model call per routine on every rebuild, so the out-of-band gen-enrich
	// producer remains the recommended path for anything but a small project.
	attachEnricher(presServer, ctx.Index, *enrichInline)

	// v24: re-pointable watcher manager. Default is STATIC (no live watching)
	// unless --watch is passed; the Studio can switch a loaded project to live
	// per-load. wm.repoint(root, live, idx) stops the current watcher and, only
	// when live, starts a fresh one bound to the given index + root, re-attaching
	// the event drain to the presentation server.
	wm := &watchManager{pres: presServer, interval: 500 * time.Millisecond}
	if *watchLive {
		wm.repoint(*root, true, ctx.Index)
	}

	// v24: projects base for the dynamic-load security check. Default: parent of
	// --root, so the UI can load sibling projects but nothing outside that tree.
	base := *projectsBase
	if base == "" {
		if abs, err := filepath.Abs(*root); err == nil {
			base = filepath.Dir(abs)
		} else {
			base = "."
		}
	}

	// v24: loader closure over LoadContextWithOpts. Returns a swap() that applies
	// the freshly parsed context to the presentation server AND re-binds the
	// watcher to the new index (so live watching, if on, tracks the new project).
	loader := func(r string, o presentation.ProjectLoadOpts) (string, func(*presentation.Server), error) {
		lo := agentctx.LoadContextOpts{}
		if o.CopybookPath != "" {
			lo.CopybookPath = filepath.SplitList(o.CopybookPath)
		}
		// v26.5: the SAME shared spec parser as the boot path. This copy had
		// already drifted — it never handled the 'cobol' synthetic catalog, so a
		// COBOL project loaded through the Studio's 📂 picker silently got no
		// runtime while the same project passed via --stdlib on the command line
		// did. One parser, one behaviour. A dynamically loaded project with no
		// stdlib spec now AUTO-detects, matching the boot default.
		sroots, sauto := agentctx.ParseStdlibSpec(o.Stdlib, o.StdlibLang)
		lo.StdlibRoots = sroots
		lo.StdlibAuto = sauto
		newCtx := agentctx.LoadContextWithOpts(r, lo)
		swap := func(ps *presentation.Server) {
			ps.ReplaceContext(newCtx.Index, newCtx.CFGs, newCtx.Effects)
			// v26.5: the newly loaded project has its OWN stdlib outcome — refresh
			// the report too, or Architecture would keep describing the previous
			// project's runtimes.
			ps.SetStdlibReport(stdlibReportView(newCtx.StdlibReport))
			// v26.22: ReplaceContext rebinds the belief store to the new project, so
			// the enricher must be re-attached or inline enrichment silently stops
			// after the first project swap.
			attachEnricher(ps, newCtx.Index, *enrichInline)
			// re-bind the watcher to the new index at its (already updated) live state
			wm.rebind(newCtx.Index)
		}
		return r, swap, nil
	}

	pm := presentation.NewProjectManager(presServer, base, loader, wm)
	pm.SetCurrent(*root, *watchLive)

	// v25.6: give the build service the same projects-base boundary so the
	// generate UI can pick/create an output folder inside it (but never the
	// current root).
	buildSvc.SetProjectsBase(base)

	// v23.44: resolve the Studio UI directory to an ABSOLUTE path so it's served
	// from a known place no matter which directory gen-server was launched from.
	resolvedWeb := resolveWebDir(*webDir)
	srv := api.NewServerWithProject(h, broker, presServer, *githubEnabled, resolvedWeb, pm)

	addr := ":" + *port
	fmt.Printf("\n🚀 gen-system studio\n")
	fmt.Printf("   URL:   http://localhost%s\n", addr)
	fmt.Printf("   Root:  %s\n", *root)
	fmt.Printf("   Studio (v23): http://localhost%s/studio-v23.html (%d symbols indexed)\n",
		addr, len(ctx.Index.AllSymbols()))
	fmt.Printf("   Studio (v20, legacy): http://localhost%s/legacy/studio.html\n", addr)
	fmt.Printf("   Web UI dir: %s\n", resolvedWeb)
	fmt.Printf("   Projects base (dynamic load): %s\n", base)
	if *watchLive {
		fmt.Printf("   Watch: LIVE (poll 500ms, debounce 200ms)\n")
	} else {
		fmt.Printf("   Watch: static (switch a project to live in the Studio)\n")
	}
	if *githubEnabled {
		fmt.Printf("   GitHub routes: ENABLED\n")
	} else {
		fmt.Printf("   GitHub routes: disabled (--github-enabled to mount)\n")
	}
	fmt.Println()
	log.Fatal(http.ListenAndServe(addr, srv))
}

// resolveWebDir picks the Studio UI directory (v23.44). Precedence:
//  1. an explicit --web value (used as-is, made absolute)
//  2. <directory-of-the-gen-server-binary>/web
//  3. ./web (legacy CWD-relative behaviour) as a last resort
//
// Returning an absolute path means the served studio-v23.html is deterministic
// regardless of where gen-server was launched from — the fix for "I replaced
// the UI file but the browser still shows the old one".
func resolveWebDir(flagVal string) string {
	abs := func(p string) string {
		if a, err := filepath.Abs(p); err == nil {
			return a
		}
		return p
	}
	if flagVal != "" {
		return abs(flagVal)
	}
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), "web")
		if st, err := os.Stat(filepath.Join(cand, "studio-v23.html")); err == nil && !st.IsDir() {
			return cand
		}
	}
	return abs("web")
}

// watchManager owns the live file watcher and makes it re-pointable (v24). The
// Studio's dynamic project load can switch which root is watched, or turn live
// watching off entirely (the default). It implements presentation.WatcherControl.
type watchManager struct {
	pres     *presentation.Server
	interval time.Duration

	mu   sync.Mutex
	cur  *watch.Watcher
	idx  *symbols.Index // index the current watcher updates
	root string
	live bool
}

// rebind points the manager at a new symbol index (after a project swap) without
// changing live/root state. If a watcher is currently live, it is restarted so
// it updates the NEW index.
func (m *watchManager) rebind(idx *symbols.Index) {
	m.mu.Lock()
	m.idx = idx
	root, live := m.root, m.live
	m.mu.Unlock()
	if live && root != "" {
		m.repoint(root, true, idx)
	}
}

// Repoint satisfies presentation.WatcherControl: stop watching the old root and,
// only when live, start watching newRoot against the current index.
func (m *watchManager) Repoint(newRoot string, live bool) {
	m.mu.Lock()
	idx := m.idx
	m.mu.Unlock()
	m.repoint(newRoot, live, idx)
}

// repoint stops any running watcher and, when live && root != "", starts a fresh
// one bound to idx and drains its events into the presentation server.
func (m *watchManager) repoint(root string, live bool, idx *symbols.Index) {
	m.mu.Lock()
	if m.cur != nil {
		m.cur.Stop()
		m.cur = nil
	}
	m.root = root
	m.live = live
	m.idx = idx
	if !live || root == "" || idx == nil {
		m.mu.Unlock()
		return
	}
	w := watch.New(idx, root, m.interval)
	m.cur = w
	pres := m.pres
	m.mu.Unlock()

	go w.Run()
	go func() {
		for ev := range w.Events {
			deleted := ev.Kind == watch.EventDeleted
			pres.OnFileChanged(ev.Path, ev.CFGs, ev.Effects, deleted)
		}
	}()
}

// stdlibReportView mirrors the loader's StdlibReport into the presentation
// layer's DTO. Kept in the binary (which imports both) so presentation stays
// free of any dependency on internal/context.
func stdlibReportView(r agentctx.StdlibReport) presentation.StdlibReportView {
	out := presentation.StdlibReportView{Mode: r.Mode, Warnings: r.Warnings}
	for _, rt := range r.Roots {
		out.Roots = append(out.Roots, presentation.StdlibRootView{
			Lang:          rt.Lang,
			Path:          rt.Path,
			Status:        string(rt.Status),
			Detail:        rt.Detail,
			Packages:      rt.Packages,
			Symbols:       rt.Symbols,
			Auto:          rt.Auto,
			LangBreakdown: rt.LangBreakdown,
		})
	}
	return out
}

// attachEnricher injects the optional enrich tier into the presentation server's
// belief store (v26.22).
//
// The dependency direction matters: internal/presentation is a CORE Understand
// package and archtest forbids it from importing internal/enrich (which pulls in the
// model). So presentation exposes its store and the GENERATION-side binary — this one
// — does the injection. understand-server never calls this, and therefore never links
// enrich at all.
//
// Must be re-called after any operation that rebinds the store (SetRoot at boot,
// ReplaceContext on project swap), or inline enrichment silently stops.
func attachEnricher(ps *presentation.Server, idx *symbols.Index, on bool) {
	if !on {
		return
	}
	store := ps.BeliefStore()
	if store == nil {
		return
	}
	if !enrich.Available() {
		fmt.Println("⚠ --enrich-inline: local model not reachable — belief enrichment is OFF.")
		fmt.Println("  The parser and human belief tiers work without it; start Ollama and restart to enable.")
		return
	}
	store.SetEnricher(enrich.New())
	store.SetBodyProvider(bodyProviderFor(idx))
	fmt.Println("✓ --enrich-inline: model descriptions will be generated during each rebuild.")
}

// bodyProviderFor returns a BodyProvider that reads a routine's source on demand,
// preferring exact byte offsets and falling back to line slicing for the frontends
// that record only line numbers (Java/COBOL/TS/Python — see v26.20). Results are
// cached per file while the file keeps its mod time and size.
func bodyProviderFor(idx *symbols.Index) belief.BodyProvider {
	// v26.58 (D017): the cache kept the first bytes of a file for the life of
	// the closure and also kept a failed read, so after a watcher edit the span
	// of the new index was cut from the old bytes. Each entry now carries the
	// mod time and size it was read at; a change reads the file again, and a
	// failed stat or read is not stored.
	type cached struct {
		src  []byte
		mod  time.Time
		size int64
	}
	var mu sync.Mutex
	files := map[string]cached{}
	return func(symbolID string) string {
		sym, ok := idx.ByID(symbolID)
		if !ok || sym == nil || sym.DefinedIn == "" {
			return ""
		}
		var src []byte
		mu.Lock()
		if fi, err := os.Stat(sym.DefinedIn); err != nil {
			delete(files, sym.DefinedIn)
		} else if c, ok := files[sym.DefinedIn]; ok && c.mod.Equal(fi.ModTime()) && c.size == fi.Size() {
			src = c.src
		} else if b, err := os.ReadFile(sym.DefinedIn); err == nil {
			src = b
			files[sym.DefinedIn] = cached{src: b, mod: fi.ModTime(), size: fi.Size()}
		} else {
			delete(files, sym.DefinedIn)
		}
		mu.Unlock()
		if src == nil {
			return ""
		}
		st, en := sym.Span.Start.Offset, sym.Span.End.Offset
		if en > st && st >= 0 && en <= len(src) {
			return string(src[st:en])
		}
		// Line-based fallback.
		sl, el := sym.Span.Start.Line, sym.Span.End.Line
		if sl <= 0 {
			return ""
		}
		lines := strings.Split(string(src), "\n")
		if sl > len(lines) {
			return ""
		}
		if el <= 0 || el < sl {
			el = sl + 60
		}
		if el > len(lines) {
			el = len(lines)
		}
		return strings.Join(lines[sl-1:el], "\n")
	}
}
