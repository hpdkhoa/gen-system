// Command studio-snapshot writes the Studio's answers for one project to JSON
// files, so the Studio can run as a static demo with no server (site/demo).
//
// It loads the project the way understand-server does and calls the real
// presentation handlers in process. It writes:
//
//	architecture.json, beliefs.json, effects.json   the fixed pages
//	graph.json                                      every symbol with its callees, callers and
//	                                                unresolved calls; the demo computes the call
//	                                                graph and search from it in the browser
//	sym/<key>.json                                  the control flow graph and source of one symbol
//
// Every absolute path under --root becomes --name (for example
// examples/payroll), with forward slashes, so no local path ships.
//
// Usage:
//
//	studio-snapshot --root examples/payroll --copybooks examples/payroll/copybooks \
//	  --name examples/payroll --out site/public/demo/data/payroll
package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/presentation"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func main() {
	root := flag.String("root", ".", "project to snapshot")
	copybooks := flag.String("copybooks", "", "COBOL copybook search path, separated like PATH")
	stdlib := flag.String("stdlib", "none", "standard library roots, as for understand-server")
	name := flag.String("name", "", "the path to show instead of the absolute root (default: the folder name)")
	out := flag.String("out", "", "output folder (required)")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "studio-snapshot: --out is required")
		os.Exit(2)
	}
	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	if *name == "" {
		*name = filepath.Base(absRoot)
	}
	s := newSnapshot(absRoot, *copybooks, *stdlib, *name)
	if err := s.write(*out); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "studio-snapshot:", err)
	os.Exit(1)
}

type snapshot struct {
	root, name string
	ctx        agentctx.Context
	mux        *http.ServeMux
	rootRe     *regexp.Regexp
}

func newSnapshot(root, copybooks, stdlib, name string) *snapshot {
	opts := agentctx.LoadContextOpts{}
	if copybooks != "" {
		var abs []string
		for _, p := range filepath.SplitList(copybooks) {
			if a, err := filepath.Abs(p); err == nil {
				abs = append(abs, a)
			}
		}
		opts.CopybookPath = abs
	}
	opts.StdlibRoots, opts.StdlibAuto = agentctx.ParseStdlibSpec(stdlib, "")
	ctx := agentctx.LoadContextWithOpts(root, opts)

	pres := presentation.New(ctx.Index)
	pres.LoadAllCFGs(ctx.CFGs)
	pres.LoadEffects(ctx.Effects)
	pres.SetRoot(root, copybooks, root)
	report := presentation.StdlibReportView{Mode: ctx.StdlibReport.Mode, Warnings: ctx.StdlibReport.Warnings}
	for _, rt := range ctx.StdlibReport.Roots {
		report.Roots = append(report.Roots, presentation.StdlibRootView{
			Lang: rt.Lang, Path: rt.Path, Status: string(rt.Status), Detail: rt.Detail,
			Packages: rt.Packages, Symbols: rt.Symbols, Auto: rt.Auto, LangBreakdown: rt.LangBreakdown,
		})
	}
	pres.SetStdlibReport(report)
	mux := http.NewServeMux()
	pres.RegisterRoutes(mux)
	pres.RegisterV23Routes(mux)
	// The root, then the rest of the path after it, up to '#' (a symbol ID's
	// kind and name), a quote or whitespace.
	re := regexp.MustCompile(regexp.QuoteMeta(root) + `[^#"\s]*`)
	return &snapshot{root: root, name: name, ctx: ctx, mux: mux, rootRe: re}
}

// clean replaces the absolute root with the display name in every string.
func (s *snapshot) clean(v string) string {
	return s.rootRe.ReplaceAllStringFunc(v, func(m string) string {
		rest := strings.TrimPrefix(m, s.root)
		return s.name + strings.ReplaceAll(rest, `\`, "/")
	})
}

// pathField names the JSON fields whose value is a file or folder path.
var pathField = map[string]bool{"path": true, "file": true, "root": true, "dir": true}

// cleanAny walks a decoded JSON value and cleans every string and map key.
func (s *snapshot) cleanAny(v any) any {
	switch t := v.(type) {
	case string:
		return s.clean(t)
	case []any:
		for i := range t {
			t[i] = s.cleanAny(t[i])
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			v := s.cleanAny(val)
			// A relative path such as a package folder keeps its Windows
			// separators; normalize them only in fields that hold a path, so
			// source text is never touched.
			if str, ok := v.(string); ok && pathField[k] {
				v = strings.ReplaceAll(str, `\`, "/")
			}
			out[s.clean(k)] = v
		}
		return out
	}
	return v
}

// get runs one handler and returns its decoded, cleaned answer.
func (s *snapshot) get(path string, q url.Values) (any, error) {
	target := path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	raw, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK {
		return nil, fmt.Errorf("%s: %d %s", path, rec.Code, strings.TrimSpace(string(raw)))
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return s.cleanAny(v), nil
}

// key names the file of one symbol: the first 16 hex digits of the SHA-1 of
// its cleaned ID. graph.json carries the key, so the browser needs no hashing.
func key(id string) string {
	h := sha1.Sum([]byte(id))
	return hex.EncodeToString(h[:])[:16]
}

type graphSymbol struct {
	ID         string   `json:"id"`
	Key        string   `json:"k,omitempty"`
	Name       string   `json:"n"`
	Qual       string   `json:"q"`
	Kind       string   `json:"kind"`
	Lang       string   `json:"lang"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Exported   bool     `json:"exp,omitempty"`
	Provenance string   `json:"prov,omitempty"`
	Init       bool     `json:"init,omitempty"`
	Recv       string   `json:"recv,omitempty"`
	Effects    []string `json:"efx,omitempty"`
	Callees    []string `json:"out,omitempty"`
	Callers    []string `json:"in,omitempty"`
	Unresolved []string `json:"unres,omitempty"`
}

func (s *snapshot) write(out string) error {
	if err := os.MkdirAll(filepath.Join(out, "sym"), 0o755); err != nil {
		return err
	}
	pages := []struct {
		file, path string
		q          url.Values
	}{
		{"architecture.json", "/api/present/v23/architecture", nil},
		{"beliefs.json", "/api/present/beliefs", nil},
		{"effects.json", "/api/present/v23/effects", nil},
		{"effects-stdlib.json", "/api/present/v23/effects", url.Values{"include_stdlib": {"1"}}},
	}
	for _, p := range pages {
		v, err := s.get(p.path, p.q)
		if err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(out, p.file), v); err != nil {
			return err
		}
	}

	idx := s.ctx.Index
	unresolved := map[string][]string{}
	for _, e := range idx.AllCallEdges() {
		if e.CalleeID == "" {
			unresolved[e.CallerID] = append(unresolved[e.CallerID], e.CalleeName)
		}
	}
	ids := func(syms []*symbols.Symbol) []string {
		var out []string
		for _, sym := range syms {
			if sym != nil {
				out = append(out, s.clean(sym.ID))
			}
		}
		return out
	}
	var graph []graphSymbol
	files := 0
	all := idx.AllSymbols()
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	for _, sym := range all {
		id := s.clean(sym.ID)
		g := graphSymbol{
			ID: id, Name: sym.Name, Qual: sym.QualName, Kind: string(sym.Kind),
			Lang: string(sym.Language), File: s.clean(sym.DefinedIn), Line: sym.Span.Start.Line,
			Exported: sym.Exported, Provenance: string(sym.Provenance), Recv: sym.ReceiverType,
			Init:    sym.Kind == symbols.SymMethod && sym.ReceiverType != "" && sym.Name == sym.ReceiverType,
			Callees: ids(idx.Callees(sym.ID)), Callers: ids(idx.Callers(sym.ID)),
		}
		for _, n := range unresolved[sym.ID] {
			g.Unresolved = append(g.Unresolved, s.clean(n))
		}
		if fe := s.ctx.Effects[sym.ID]; fe != nil {
			for k, on := range fe.Effects {
				if on {
					g.Effects = append(g.Effects, string(k))
				}
			}
			sort.Strings(g.Effects)
		}
		// One file per symbol that has a body or a definition to show.
		if sym.Kind != symbols.SymModule && sym.Kind != symbols.SymImport && sym.Kind != symbols.SymParam &&
			!sym.Provenance.IsExternal() {
			flowV, flowErr := s.get("/api/present/flow", url.Values{"func": {sym.ID}})
			srcV, srcErr := s.get("/api/present/source", url.Values{"sym": {sym.ID}})
			if flowErr == nil || srcErr == nil {
				g.Key = key(id)
				body := map[string]any{}
				if flowErr == nil {
					body["flow"] = flowV
				}
				if srcErr == nil {
					body["source"] = srcV
				}
				if err := writeJSON(filepath.Join(out, "sym", g.Key+".json"), body); err != nil {
					return err
				}
				files++
			}
		}
		graph = append(graph, g)
	}
	meta := map[string]any{"name": s.name, "symbols": graph}
	if err := writeJSON(filepath.Join(out, "graph.json"), meta); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "studio-snapshot: %s: %d symbols, %d symbol files\n", s.name, len(graph), files)
	return nil
}

func writeJSON(path string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
