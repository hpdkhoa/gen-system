// generator/cmd/understand-mcp/main.go: the Understand half of gen-system as an
// MCP server. An agent (Claude Code, Codex, Cursor and others) asks it about a
// codebase and gets parser facts back: the architecture, symbols, call graphs,
// control flow graphs, effects, source and the beliefs gen-system holds.
//
// It loads one project at start, exactly as understand-server does, and its
// tools call the same presentation handlers the Studio uses, so both give the
// same answers. Like understand-server it links no generation package, no model
// client and no network code that leaves the machine. internal/archtest holds
// that boundary. It speaks MCP over stdio and writes nothing else to stdout; all
// logs go to stderr.
//
// Every tool is read only, except correct_belief, which only exists when the
// server starts with --allow-corrections.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/mcp"
	"github.com/hpdkhoa/gen-system/generator/internal/presentation"
	"github.com/hpdkhoa/gen-system/generator/internal/review"
	"github.com/hpdkhoa/gen-system/generator/internal/watch"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	root := flag.String("root", ".", "project root to understand")
	copybooks := flag.String("copybooks", "", "COBOL copybook search path (list separated like PATH)")
	stdlibSpec := flag.String("stdlib", "", "standard library roots: 'auto' (default), 'none', or a list such as 'go,java=/opt/jdk/java.base'")
	allowCorrect := flag.Bool("allow-corrections", false, "add the correct_belief tool, which writes .gen/beliefs.json in the project")
	noWatch := flag.Bool("no-watch", false, "do not re-index files that change while the server runs")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("understand-mcp", version)
		return
	}

	// The protocol owns stdout. Packages that print progress write to
	// os.Stdout, so point it at stderr and keep the real stdout for MCP.
	protocolOut := os.Stdout
	os.Stdout = os.Stderr

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "understand-mcp:", err)
		os.Exit(2)
	}
	if fi, err := os.Stat(absRoot); err != nil || !fi.IsDir() {
		fmt.Fprintln(os.Stderr, "understand-mcp: --root is not a folder:", absRoot)
		os.Exit(2)
	}

	app := load(absRoot, *copybooks, *stdlibSpec)
	if !*noWatch {
		app.watch(absRoot)
	}
	srv := app.mcpServer(*allowCorrect)
	fmt.Fprintf(os.Stderr, "understand-mcp %s: %s, %d symbols indexed, read only%s\n",
		version, absRoot, app.symbols, map[bool]string{true: ", corrections allowed", false: ""}[*allowCorrect])
	if err := srv.Serve(os.Stdin, protocolOut); err != nil {
		fmt.Fprintln(os.Stderr, "understand-mcp:", err)
		os.Exit(1)
	}
}

// app holds the loaded project and the handlers the tools call.
type app struct {
	root    string
	pres    *presentation.Server
	mux     *http.ServeMux
	symbols int
	ctx     agentctx.Context
}

func load(root, copybooks, stdlibSpec string) *app {
	opts := agentctx.LoadContextOpts{}
	if copybooks != "" {
		opts.CopybookPath = filepath.SplitList(copybooks)
	}
	opts.StdlibRoots, opts.StdlibAuto = agentctx.ParseStdlibSpec(stdlibSpec, "")
	ctx := agentctx.LoadContextWithOpts(root, opts)

	pres := presentation.New(ctx.Index)
	pres.LoadAllCFGs(ctx.CFGs)
	pres.LoadEffects(ctx.Effects)
	pres.SetRoot(root, copybooks, root)
	pres.SetStdlibReport(stdlibReportView(ctx.StdlibReport))
	mux := http.NewServeMux()
	pres.RegisterRoutes(mux)
	pres.RegisterV23Routes(mux)
	for _, line := range agentctx.FormatStdlibReport(ctx.StdlibReport) {
		fmt.Fprintln(os.Stderr, line)
	}
	n := 0
	if ctx.Index != nil {
		n = len(ctx.Index.AllSymbols())
	}
	return &app{root: root, pres: pres, mux: mux, symbols: n, ctx: ctx}
}

func (a *app) watch(root string) {
	w := watch.New(a.ctx.Index, root, 500*time.Millisecond)
	go w.Run()
	go func() {
		for ev := range w.Events {
			a.pres.OnFileChanged(ev.Path, ev.CFGs, ev.Effects, ev.Kind == watch.EventDeleted)
		}
	}()
}

// get runs one Studio handler in process and decodes its JSON answer.
func (a *app) get(path string, q url.Values) (any, error) {
	return a.do(http.MethodGet, path+"?"+q.Encode(), nil)
}

func (a *app) do(method, target string, body []byte) (any, error) {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, req)
	raw, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK {
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(raw)))
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("unexpected answer: %v", err)
	}
	return out, nil
}

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func boolean(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func integer(desc string, min, max int) map[string]any {
	return map[string]any{"type": "integer", "description": desc, "minimum": min, "maximum": max}
}

var readOnly = map[string]any{"readOnlyHint": true, "openWorldHint": false}

func (a *app) mcpServer(allowCorrect bool) *mcp.Server {
	s := &mcp.Server{
		Name:    "gen-understand",
		Version: version,
		Instructions: "Parser facts about one codebase, from gen-system's deterministic Understand engine. " +
			"No model wrote these answers. Start with architecture, find symbols with search_symbols, " +
			"then pass a symbol id to call_graph, control_flow, source or beliefs. " +
			"A symbol id looks like <file>#<kind>:<qualified name>.",
	}

	s.AddTool(mcp.Tool{
		Name:  "architecture",
		Title: "Architecture overview",
		Description: "Languages, top packages, entry point candidates, constructs the parsers could not model " +
			"(grouped by reason), call edge resolution, and which standard library roots attached.",
		InputSchema: obj(map[string]any{}),
		Annotations: readOnly,
		Handler: func(map[string]any) (any, error) {
			return a.get("/api/present/v23/architecture", url.Values{})
		},
	})

	s.AddTool(mcp.Tool{
		Name:  "search_symbols",
		Title: "Search symbols",
		Description: "Ranked prefix, substring and subsequence matches over symbol names. " +
			"Returns symbol ids for the other tools.",
		InputSchema: obj(map[string]any{
			"query":          str("part of a symbol name, for example 'Checkout' or 'order.save'"),
			"effect":         str("keep only functions with this effect: db, fs, network or pure"),
			"limit":          integer("most results to return (default 50)", 1, 500),
			"include_stdlib": boolean("also search standard library and third party symbols"),
		}, "query"),
		Annotations: readOnly,
		Handler: func(args map[string]any) (any, error) {
			q, err := mcp.String(args, "query")
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(q) == "" {
				return nil, errors.New("query must not be empty")
			}
			v := url.Values{"q": {q}}
			if e, err := mcp.String(args, "effect"); err != nil {
				return nil, err
			} else if e != "" {
				v.Set("effect", e)
			}
			limit, err := mcp.Int(args, "limit")
			if err != nil {
				return nil, err
			}
			if limit <= 0 {
				limit = 50
			}
			v.Set("limit", fmt.Sprint(limit))
			if b, err := mcp.Bool(args, "include_stdlib"); err != nil {
				return nil, err
			} else if b {
				v.Set("include_stdlib", "1")
			}
			return a.get("/api/present/v23/search", v)
		},
	})

	s.AddTool(mcp.Tool{
		Name:  "call_graph",
		Title: "Call graph around a symbol",
		Description: "Breadth first walk over resolved call edges from one symbol. Unresolved callees come back " +
			"as external nodes, so a gap in the graph is visible, never hidden.",
		InputSchema: obj(map[string]any{
			"symbol":         str("symbol id from search_symbols"),
			"depth":          integer("hops to walk (default 2)", 1, 4),
			"direction":      map[string]any{"type": "string", "enum": []string{"callees", "callers", "both"}, "description": "which edges to follow (default callees)"},
			"include_stdlib": boolean("keep standard library and third party nodes"),
		}, "symbol"),
		Annotations: readOnly,
		Handler: func(args map[string]any) (any, error) {
			sym, err := requireString(args, "symbol")
			if err != nil {
				return nil, err
			}
			v := url.Values{"root": {sym}}
			depth, err := mcp.Int(args, "depth")
			if err != nil {
				return nil, err
			}
			if depth <= 0 {
				depth = 2
			}
			v.Set("depth", fmt.Sprint(depth))
			dir, err := mcp.String(args, "direction")
			if err != nil {
				return nil, err
			}
			if dir == "" {
				dir = "callees"
			}
			v.Set("direction", dir)
			if b, err := mcp.Bool(args, "include_stdlib"); err != nil {
				return nil, err
			} else if b {
				v.Set("include_stdlib", "1")
			}
			return a.get("/api/present/v23/callgraph", v)
		},
	})

	s.AddTool(mcp.Tool{
		Name:  "control_flow",
		Title: "Control flow graph of a function",
		Description: "The function's basic blocks, their edges, and the source lines in each block. " +
			"A type, variable or import gets an explanation instead of a graph.",
		InputSchema: obj(map[string]any{"symbol": str("symbol id of a function, method or procedure")}, "symbol"),
		Annotations: readOnly,
		Handler: func(args map[string]any) (any, error) {
			sym, err := requireString(args, "symbol")
			if err != nil {
				return nil, err
			}
			return a.get("/api/present/flow", url.Values{"func": {sym}})
		},
	})

	s.AddTool(mcp.Tool{
		Name:        "source",
		Title:       "Source of a symbol",
		Description: "The source lines of exactly one symbol, never the whole file.",
		InputSchema: obj(map[string]any{"symbol": str("symbol id")}, "symbol"),
		Annotations: readOnly,
		Handler: func(args map[string]any) (any, error) {
			sym, err := requireString(args, "symbol")
			if err != nil {
				return nil, err
			}
			return a.get("/api/present/source", url.Values{"sym": {sym}})
		},
	})

	s.AddTool(mcp.Tool{
		Name:        "file_symbols",
		Title:       "Symbols in a file",
		Description: "Every symbol the parsers found in one file of the project, with constructs they could not model.",
		InputSchema: obj(map[string]any{"file": str("file path, absolute or relative to the project root")}, "file"),
		Annotations: readOnly,
		Handler: func(args map[string]any) (any, error) {
			f, err := requireString(args, "file")
			if err != nil {
				return nil, err
			}
			// A relative path is relative to the project root.
			if !filepath.IsAbs(f) {
				f = filepath.Join(a.root, filepath.FromSlash(f))
			}
			return a.get("/api/present/symbols", url.Values{"file": {f}})
		},
	})

	s.AddTool(mcp.Tool{
		Name:  "effects",
		Title: "Functions by effect",
		Description: "Functions grouped by what they touch: db, fs, network, pure or unknown, " +
			"inferred from the calls the parser saw.",
		InputSchema: obj(map[string]any{
			"effect": str("return only this group: db, fs, network, pure or unknown"),
			"limit":  integer("most functions per group (default 100)", 1, 2000),
		}),
		Annotations: readOnly,
		Handler: func(args map[string]any) (any, error) {
			effect, err := mcp.String(args, "effect")
			if err != nil {
				return nil, err
			}
			limit, err := mcp.Int(args, "limit")
			if err != nil {
				return nil, err
			}
			if limit <= 0 {
				limit = 100
			}
			out, err := a.get("/api/present/v23/effects", url.Values{})
			if err != nil {
				return nil, err
			}
			return filterEffects(out, strings.ToLower(effect), limit), nil
		},
	})

	s.AddTool(mcp.Tool{
		Name:  "beliefs",
		Title: "Beliefs about the code",
		Description: "What gen-system holds about each routine: inputs, calls, effect, data changes and action, " +
			"with the source of each (parser, model or human), whether a human verified it, and whether a " +
			"correction went stale because the code changed under it.",
		InputSchema: obj(map[string]any{
			"subject":         str("keep beliefs whose subject contains this text, for example a function name"),
			"kind":            str("keep one kind, for example inputs, calls, effect, data-change or action"),
			"unverified_only": boolean("keep only beliefs no human has verified"),
			"limit":           integer("most beliefs to return (default 100)", 1, 2000),
		}),
		Annotations: readOnly,
		Handler: func(args map[string]any) (any, error) {
			subject, err := mcp.String(args, "subject")
			if err != nil {
				return nil, err
			}
			kind, err := mcp.String(args, "kind")
			if err != nil {
				return nil, err
			}
			unverified, err := mcp.Bool(args, "unverified_only")
			if err != nil {
				return nil, err
			}
			limit, err := mcp.Int(args, "limit")
			if err != nil {
				return nil, err
			}
			if limit <= 0 {
				limit = 100
			}
			out, err := a.get("/api/present/beliefs", url.Values{})
			if err != nil {
				return nil, err
			}
			return filterBeliefs(out, subject, kind, unverified, limit), nil
		},
	})

	s.AddTool(mcp.Tool{
		Name:  "review_diff",
		Title: "Review a change",
		Description: "Parser facts about a change: removed functions that are still called, changed signatures with " +
			"callers outside the change, new database, file or network effects, functions no test reaches, and new " +
			"branches. Compares the working tree with a git base (default HEAD, so uncommitted work), or reads a diff.",
		InputSchema: obj(map[string]any{
			"base":   str("git revision to compare with, for example HEAD, main or origin/main (default HEAD)"),
			"diff":   str("a unified diff to review instead of asking git; then only callers and tests are checked"),
			"hops":   integer("how many calls up to look for a test (default 3)", 1, 6),
			"format": map[string]any{"type": "string", "enum": []string{"json", "markdown"}, "description": "json (default) or markdown"},
		}),
		Annotations: readOnly,
		Handler: func(args map[string]any) (any, error) {
			base, err := mcp.String(args, "base")
			if err != nil {
				return nil, err
			}
			diff, err := mcp.String(args, "diff")
			if err != nil {
				return nil, err
			}
			hops, err := mcp.Int(args, "hops")
			if err != nil {
				return nil, err
			}
			format, err := mcp.String(args, "format")
			if err != nil {
				return nil, err
			}
			rep, err := review.Run(a.ctx.Index, review.Options{Root: a.root, Base: base, Diff: diff, Hops: hops})
			if err != nil {
				return nil, err
			}
			if format == "markdown" {
				return map[string]any{"markdown": review.Markdown(rep)}, nil
			}
			return rep, nil
		},
	})

	if allowCorrect {
		s.AddTool(mcp.Tool{
			Name:  "correct_belief",
			Title: "Correct a belief",
			Description: "Record a human correction for one belief. The correction wins on every rebuild, and if the " +
				"code later changes under it, the belief is flagged stale with both values. Writes .gen/beliefs.json.",
			InputSchema: obj(map[string]any{
				"belief_id":    str("id from the beliefs tool"),
				"corrected_to": str("the value the belief should hold"),
				"note":         str("why, in a few words"),
			}, "belief_id", "corrected_to"),
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
			Handler: func(args map[string]any) (any, error) {
				id, err := requireString(args, "belief_id")
				if err != nil {
					return nil, err
				}
				to, err := requireString(args, "corrected_to")
				if err != nil {
					return nil, err
				}
				note, err := mcp.String(args, "note")
				if err != nil {
					return nil, err
				}
				body, _ := json.Marshal(map[string]string{"belief_id": id, "corrected_to": to, "human_note": note})
				return a.do(http.MethodPost, "/api/present/beliefs/correct", body)
			},
		})
	}
	return s
}

func requireString(args map[string]any, name string) (string, error) {
	v, err := mcp.String(args, name)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return v, nil
}

// filterBeliefs keeps the beliefs that match and the counts a model needs.
func filterBeliefs(out any, subject, kind string, unverified bool, limit int) any {
	m, ok := out.(map[string]any)
	if !ok {
		return out
	}
	all, _ := m["beliefs"].([]any)
	var kept []any
	matched := 0
	for _, b := range all {
		bm, ok := b.(map[string]any)
		if !ok {
			continue
		}
		if subject != "" && !strings.Contains(strings.ToLower(fmt.Sprint(bm["subject"])), strings.ToLower(subject)) {
			continue
		}
		if kind != "" && !strings.EqualFold(fmt.Sprint(bm["kind"]), kind) {
			continue
		}
		if unverified {
			if v, _ := bm["human_verified"].(bool); v {
				continue
			}
		}
		matched++
		if len(kept) < limit {
			kept = append(kept, bm)
		}
	}
	return map[string]any{
		"beliefs":        kept,
		"matched":        matched,
		"returned":       len(kept),
		"total_beliefs":  m["total_beliefs"],
		"pending_review": m["pending_review"],
		"stale_count":    m["stale_count"],
		"warnings":       m["warnings"],
	}
}

// filterEffects keeps one effect group, or all of them, each cut to limit.
func filterEffects(out any, effect string, limit int) any {
	m, ok := out.(map[string]any)
	if !ok {
		return out
	}
	groups, _ := m["by_effect"].(map[string]any)
	if groups == nil {
		return out
	}
	res := map[string]any{}
	counts := map[string]int{}
	names := make([]string, 0, len(groups))
	for k := range groups {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		list, _ := groups[k].([]any)
		counts[k] = len(list)
		if effect != "" && k != effect {
			continue
		}
		if len(list) > limit {
			list = list[:limit]
		}
		res[k] = list
	}
	return map[string]any{"by_effect": res, "counts": counts}
}

// stdlibReportView mirrors the loader's report into the presentation DTO, as
// understand-server does, so presentation stays free of the loader package.
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
