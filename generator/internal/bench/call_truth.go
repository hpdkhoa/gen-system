package bench

// The judge for call resolution: Go's own type checker. Every call in every module package is checked with
// bodies, and types.Info names the function each call really reaches. Each call edge of the index is then right
// (it links that function), wrong (it links another one), missed (it links nothing) or absent (the parser saw no
// call there). Calls to a function outside the project count as false links when the index linked a project
// symbol. Calls through an interface or a function value have no single target; they are counted, not scored.

import (
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"sort"
	"strings"
	"time"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
)

// CallTruthResult is the score of the index's call edges against the type checker.
type CallTruthResult struct {
	Root     string   `json:"root"`
	Calls    int      `json:"calls"`    // calls the type checker saw in module packages
	Project  int      `json:"project"`  // calls to a concrete project function or method
	Right    int      `json:"right"`    // project calls linked to the right symbol
	Wrong    int      `json:"wrong"`    // project calls linked to another symbol
	Missed   int      `json:"missed"`   // project calls left unlinked
	Absent   int      `json:"absent"`   // project calls with no edge at all
	External int      `json:"external"` // calls to a function outside the project
	False    int      `json:"false_links"`
	Dynamic  int      `json:"dynamic"`        // calls through an interface or a function value
	DynLink  int      `json:"dynamic_linked"` // of those, linked to some project symbol
	Prec     float64  `json:"precision"`      // right / (right + wrong + false links)
	Recall   float64  `json:"recall"`         // right / project
	Seconds  float64  `json:"seconds"`
	Examples []string `json:"examples,omitempty"` // a few wrong and false links, for reading
}

type truthCall struct {
	kind   string // project, external, dynamic
	file   string
	line   int    // target line, for project calls
	target string // for examples
}

func normPath(p string) string { return strings.ToLower(filepath.Clean(p)) }

// CallTruth scores the call graph of the Go module at root.
func CallTruth(root string) (*CallTruthResult, error) {
	t0 := time.Now()
	abs, _ := filepath.Abs(root)
	j, err := newTypeJudge(abs)
	if err != nil {
		return nil, err
	}
	all := map[string]bool{}
	for p := range j.pkgs {
		all[p] = true
	}
	inProject := func(file string) bool {
		f := normPath(file)
		return strings.HasPrefix(f, normPath(abs)+string(filepath.Separator)) && !strings.Contains(f, string(filepath.Separator)+"vendor"+string(filepath.Separator))
	}

	truth := map[string][]truthCall{} // file|line|name of the call
	shared := j.importer(all, "")
	check := func(gp *goPkg, xtest bool) {
		files := append(append([]string{}, gp.GoFiles...), gp.TestGoFiles...)
		path, imp := gp.ImportPath, shared
		if xtest {
			files, path = gp.XTestGoFiles, gp.ImportPath+"_test"
			imp = j.importer(all, gp.ImportPath)
		}
		if len(files) == 0 {
			return
		}
		asts, err := imp.parse(gp.Dir, files)
		if err != nil {
			return
		}
		info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
		conf := types.Config{Importer: imp, Error: func(error) {}}
		_, _ = conf.Check(path, imp.fset, asts, info)
		for _, f := range asts {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				fun := ast.Unparen(call.Fun)
				if ix, ok := fun.(*ast.IndexExpr); ok {
					fun = ix.X
				} else if ix, ok := fun.(*ast.IndexListExpr); ok {
					fun = ix.X
				}
				var id *ast.Ident
				switch x := fun.(type) {
				case *ast.Ident:
					id = x
				case *ast.SelectorExpr:
					id = x.Sel
				default:
					return true // a call of a call or a literal: no name to match
				}
				pos := imp.fset.Position(call.Pos())
				key := fmt.Sprintf("%s|%d|%s", normPath(pos.Filename), pos.Line, id.Name)
				tc := truthCall{kind: "dynamic"}
				switch obj := info.Uses[id].(type) {
				case *types.Func:
					obj = obj.Origin()
					sig, _ := obj.Type().(*types.Signature)
					if sig != nil && sig.Recv() != nil && types.IsInterface(sig.Recv().Type()) {
						break
					}
					tp := imp.fset.Position(obj.Pos())
					if inProject(tp.Filename) {
						tc = truthCall{kind: "project", file: normPath(tp.Filename), line: tp.Line, target: obj.FullName()}
					} else {
						tc = truthCall{kind: "external", target: obj.FullName()}
					}
				case *types.Var:
				default:
					return true // a builtin or a type conversion
				}
				truth[key] = append(truth[key], tc)
				return true
			})
		}
	}
	for _, gp := range j.pkgs {
		check(gp, false)
		check(gp, true)
	}

	ctx := agentctx.LoadContextWithOpts(abs, agentctx.LoadContextOpts{})
	if ctx.Index == nil {
		return nil, fmt.Errorf("no index for %s", abs)
	}
	type edge struct {
		file     string
		from, to int
	}
	edges := map[string][]*edge{}
	for _, e := range ctx.Index.AllCallEdges() {
		caller, ok := ctx.Index.ByID(e.CallerID)
		if !ok || caller == nil || string(caller.Language) != "go" {
			continue
		}
		name := e.CalleeName
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		if i := strings.Index(name, "["); i >= 0 {
			name = name[:i]
		}
		ed := &edge{}
		if e.CalleeID != "" {
			if s, ok := ctx.Index.ByID(e.CalleeID); ok && s != nil && inProject(s.DefinedIn) {
				ed = &edge{file: normPath(s.DefinedIn), from: s.Span.Start.Line, to: s.Span.End.Line}
			}
		}
		key := fmt.Sprintf("%s|%d|%s", normPath(caller.DefinedIn), e.Span.Start.Line, name)
		edges[key] = append(edges[key], ed)
	}

	r := &CallTruthResult{Root: abs}
	keys := make([]string, 0, len(truth))
	for k := range truth {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	seen := map[string]int{}
	example := func(s string) {
		kind := s[:strings.Index(s, " ")]
		if seen[kind] < 12 {
			seen[kind]++
			r.Examples = append(r.Examples, s)
		}
	}
	for _, k := range keys {
		es := edges[k]
		for _, tc := range truth[k] {
			r.Calls++
			// take the edge that matches best: the right one, else any
			pick := -1
			for i, e := range es {
				if tc.kind == "project" && e.file == tc.file && tc.line >= e.from && tc.line <= e.to {
					pick = i
					break
				}
				if pick < 0 {
					pick = i
				}
			}
			var e *edge
			if pick >= 0 {
				e = es[pick]
				es = append(es[:pick:pick], es[pick+1:]...)
			}
			linked := e != nil && e.file != ""
			switch tc.kind {
			case "project":
				r.Project++
				switch {
				case e == nil:
					r.Absent++
				case !linked:
					r.Missed++
					example(fmt.Sprintf("missed %s: wants %s", k, tc.target))
				case e.file == tc.file && tc.line >= e.from && tc.line <= e.to:
					r.Right++
				default:
					r.Wrong++
					example(fmt.Sprintf("wrong %s: wants %s, got %s:%d", k, tc.target, filepath.Base(e.file), e.from))
				}
			case "external":
				r.External++
				if linked {
					r.False++
					example(fmt.Sprintf("false %s: is %s, got %s:%d", k, tc.target, filepath.Base(e.file), e.from))
				}
			default:
				r.Dynamic++
				if linked {
					r.DynLink++
				}
			}
		}
	}
	if n := r.Right + r.Wrong + r.False; n > 0 {
		r.Prec = float64(r.Right) / float64(n)
	}
	if r.Project > 0 {
		r.Recall = float64(r.Right) / float64(r.Project)
	}
	r.Seconds = time.Since(t0).Seconds()
	return r, nil
}

// CallTruthMarkdown is one table row per result.
func CallTruthMarkdown(rs []*CallTruthResult) string {
	var b strings.Builder
	b.WriteString("| Repo | Project calls | Right | Wrong | Missed | No edge | Outside calls linked by mistake | Precision | Recall |\n|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rs {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d of %d | %.2f | %.2f |\n", filepath.Base(r.Root), r.Project, r.Right, r.Wrong, r.Missed, r.Absent, r.False, r.External, r.Prec, r.Recall)
	}
	return b.String()
}
