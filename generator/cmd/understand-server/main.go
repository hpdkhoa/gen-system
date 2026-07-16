// generator/cmd/understand-server/main.go — v23.9 (Phase 3): the ENTERPRISE
// server. Serves the read-only code-intelligence Studio (Architecture, Graph,
// Beliefs, Search) for an existing codebase, with the embedded UI. It constructs
// ONLY the presentation server + SSE broker + incremental watcher and calls
// api.RegisterUnderstandRoutes — so it links NONE of the generation stack
// (no ai/Ollama, no engine, no planner, no agent). This is enforced by
// understand_deps_test.go (acceptance test: go list -deps shows no generation
// package). The air-gapped posture is therefore structural: the shipped binary
// physically cannot generate code or call a model, because that code is absent.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"gen-system/generator/internal/api/sse"
	"gen-system/generator/internal/api/understand"
	agentctx "gen-system/generator/internal/context"
	"gen-system/generator/internal/presentation"
	"gen-system/generator/internal/watch"
	"gen-system/generator/internal/webui"
)

func main() {
	port := flag.String("port", "28080", "HTTP server port")
	root := flag.String("root", ".", "Project root path to understand")
	copybooks := flag.String("copybooks", "", "COBOL copybook search path (for COPY resolution)")
	browseRoot := flag.String("browse-root", "", "wider directory the file browser may reach (default: --root)")
	stdlibSpec := flag.String("stdlib", "", "v26.5: comma-separated stdlib roots, language PER ROOT. 'auto' (the default when omitted) detects a runtime for every language in the project; 'none' opts out. Shorthands: 'go' → GOROOT/src, 'cobol' → built-in runtime catalog. Explicit form: 'java=/opt/jdk/java.base'. Mixable: 'go,java=/opt/jdk/java.base'. All roots are import-scoped.")
	stdlibLang := flag.String("stdlib-lang", "", "DEPRECATED (v26.5): legacy single language label for bare --stdlib paths. Prefer the per-root 'lang=path' form.")
	flag.Parse()

	broker := sse.NewBroker()

	// Full initial parse (idempotent, authoritative first load).
	//
	// v26.5 — TWO REAL BUGS FIXED HERE. The enterprise binary called the bare
	// LoadContext(root), which takes NO options. That meant:
	//   1. It had no --stdlib flag at all, so cross-boundary calls (fmt.Println,
	//      os.Exit, every JDK/COBOL runtime call) could never resolve — this is
	//      what pinned the Architecture "edge resolution" number so low. There
	//      was no way to fix it from the command line because the flag did not
	//      exist.
	//   2. --copybooks was accepted but only forwarded to SetRoot (the file
	//      browser). It never reached the PARSER, so COBOL COPY resolution was
	//      silently off in the shipped binary even when the flag was passed.
	// Both now go through LoadContextWithOpts. Stdlib defaults to AUTO: the
	// languages actually present in the project decide which runtimes attach,
	// and every root is import-scoped, so this is cheap and safe. The outcome is
	// printed below rather than gated behind a prompt — a wrong guess is free,
	// an unhelpful prompt in an air-gapped/CI context is not.
	loadOpts := agentctx.LoadContextOpts{}
	if *copybooks != "" {
		loadOpts.CopybookPath = filepath.SplitList(*copybooks)
	}
	roots, auto := agentctx.ParseStdlibSpec(*stdlibSpec, *stdlibLang)
	loadOpts.StdlibRoots = roots
	loadOpts.StdlibAuto = auto

	ctx := agentctx.LoadContextWithOpts(*root, loadOpts)
	presServer := presentation.New(ctx.Index)
	presServer.LoadAllCFGs(ctx.CFGs)
	presServer.LoadEffects(ctx.Effects)
	presServer.SetBroker(broker)
	presServer.SetRoot(*root, *copybooks, *browseRoot)
	presServer.SetStdlibReport(stdlibReportView(ctx.StdlibReport))

	mux := http.NewServeMux()
	understand.RegisterRoutes(mux, broker, presServer)
	// Embedded Studio UI (no loose web/ dir on disk; single self-contained binary).
	mux.Handle("/", webui.Handler())

	// Incremental watcher — keeps the symbol index live as files change.
	w := watch.New(ctx.Index, *root, 500*time.Millisecond)
	go w.Run()
	go func() {
		for ev := range w.Events {
			deleted := ev.Kind == watch.EventDeleted
			presServer.OnFileChanged(ev.Path, ev.CFGs, ev.Effects, deleted)
		}
	}()

	addr := ":" + *port
	fmt.Printf("\n🔎 gen-system understand-server (enterprise, offline)\n")
	fmt.Printf("   URL:   http://localhost%s/\n", addr)
	fmt.Printf("   Root:  %s\n", *root)
	fmt.Printf("   Studio: http://localhost%s/  (embedded; %d symbols indexed)\n",
		addr, len(ctx.Index.AllSymbols()))
	fmt.Printf("   Mode:  understanding only — no generation, no model, no network egress\n")
	fmt.Printf("   Watch: incremental rebuild active (poll 500ms)\n")
	// v26.5: say what runtimes attached and — crucially — what did NOT and why,
	// so the Architecture edge-resolution % is self-diagnosing instead of a
	// mystery number. This is the report-not-prompt half of the design.
	for _, line := range agentctx.FormatStdlibReport(ctx.StdlibReport) {
		fmt.Println(line)
	}

	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Printf("server error: %v\n", err)
	}
}

// stdlibReportView mirrors the loader's StdlibReport into the presentation
// layer's DTO. The conversion lives HERE (in the binary, which already imports
// both) rather than in either package, so presentation keeps zero dependency on
// internal/context — the understand-server boundary test walks those imports and
// the air-gapped posture depends on the view layer staying loader-free.
func stdlibReportView(r agentctx.StdlibReport) presentation.StdlibReportView {
	out := presentation.StdlibReportView{Mode: r.Mode}
	for _, rt := range r.Roots {
		out.Roots = append(out.Roots, presentation.StdlibRootView{
			Lang:     rt.Lang,
			Path:     rt.Path,
			Status:   string(rt.Status),
			Detail:   rt.Detail,
			Packages: rt.Packages,
			Symbols:  rt.Symbols,
			Auto:     rt.Auto,
		})
	}
	return out
}
