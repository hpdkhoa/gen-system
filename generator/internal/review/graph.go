package review

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// graph is the call graph of the current code, turned around: callee to callers.
type graph struct {
	idx        *symbols.Index
	root       string
	syms       map[string]*symbols.Symbol
	callers_   map[string][]string // callee id to caller ids
	unresolved map[string][]string // last name segment of an unresolved callee to caller ids
	// unresolvedEdges keeps each unresolved call with its written name, for the removed-function check.
	unresolvedEdges map[string][]unresolvedEdge
}

type unresolvedEdge struct {
	callerID, name string
	at             ast.Pos
}

func newGraph(idx *symbols.Index, root string) *graph {
	g := &graph{idx: idx, root: root, syms: map[string]*symbols.Symbol{}, callers_: map[string][]string{}, unresolved: map[string][]string{},
		unresolvedEdges: map[string][]unresolvedEdge{}}
	if idx == nil {
		return g
	}
	for _, s := range idx.AllSymbols() {
		g.syms[s.ID] = s
	}
	seen := map[[2]string]bool{}
	for _, e := range idx.AllCallEdges() {
		if e.CalleeID == "" || g.syms[e.CalleeID] == nil {
			n := lastSegment(e.CalleeName)
			if n != "" {
				g.unresolvedEdges[n] = append(g.unresolvedEdges[n], unresolvedEdge{e.CallerID, strings.TrimSuffix(e.CalleeName, "()"), e.Span.Start})
			}
			k := [2]string{"?" + n, e.CallerID}
			if n != "" && !seen[k] {
				seen[k] = true
				g.unresolved[n] = append(g.unresolved[n], e.CallerID)
			}
			continue
		}
		k := [2]string{e.CalleeID, e.CallerID}
		if !seen[k] && e.CalleeID != e.CallerID {
			seen[k] = true
			g.callers_[e.CalleeID] = append(g.callers_[e.CalleeID], e.CallerID)
		}
	}
	return g
}

func lastSegment(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexAny(s, ".:>"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSuffix(s, "()")
}

func (g *graph) ref(id string) (Ref, bool) {
	s := g.syms[id]
	if s == nil || s.Provenance.IsExternal() {
		return Ref{}, false
	}
	rel := s.DefinedIn
	if r, err := filepath.Rel(g.root, s.DefinedIn); err == nil && !strings.HasPrefix(r, "..") {
		rel = filepath.ToSlash(r)
	}
	return Ref{ID: id, Name: displayName(s), File: rel, Line: s.Span.Start.Line}, true
}

func (g *graph) refs(ids []string) []Ref {
	var out []Ref
	for _, id := range ids {
		if r, ok := g.ref(id); ok {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func (g *graph) callers(id string) []Ref { return g.refs(g.callers_[id]) }

// stillCalling lists the places that still call a removed function: calls left unresolved under its name. In Go a
// call must also fit: x.f() only when x is not declared as another type, and a plain f() only from f's own package.
func (g *graph) stillCalling(f *Func) []Ref {
	var keep []string
	for _, e := range g.unresolvedEdges[f.bare] {
		if f.Language != "go" || g.goFits(e, f) {
			keep = append(keep, e.callerID)
		}
	}
	return g.refs(dedupe(keep))
}

func (g *graph) goFits(e unresolvedEdge, f *Func) bool {
	caller := g.syms[e.callerID]
	dot := strings.Index(e.name, ".")
	if f.recvType == "" { // a function: f() from its package, or pkg.f() through its package name
		if dot < 0 {
			return caller != nil && filepath.Dir(caller.DefinedIn) == f.dir
		}
		if strings.Contains(e.name[dot+1:], ".") || caller == nil || g.idx == nil {
			return false
		}
		dir, ip, ok := g.idx.ImportDirOf(e.name[:dot], caller.DefinedIn)
		if !ok {
			return false // x.f() on a variable calls a method, not this function
		}
		if dir != "" {
			return filepath.Clean(dir) == filepath.Clean(f.dir)
		}
		return strings.HasSuffix(filepath.ToSlash(f.dir), "/"+ip[strings.LastIndex(ip, "/")+1:])
	}
	if dot < 0 || caller == nil { // a method needs a receiver
		return dot >= 0
	}
	if g.idx == nil {
		return true
	}
	// the resolver's own rule: receiver, parameters, locals, and chains through fields and method results
	typ, dir, known, outside := g.idx.GoReceiverType(e.name, caller, e.at)
	switch {
	case outside:
		return false // w.Write on an http.ResponseWriter
	case known:
		return typ == f.recvType && strings.EqualFold(filepath.Clean(dir), filepath.Clean(f.dir))
	}
	return true // the type is not shown: it may be this one
}

func dedupe(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// testsReaching walks callers up to hops calls and returns the test functions it meets.
func (g *graph) testsReaching(id string, hops int) []Ref {
	seen := map[string]bool{id: true}
	frontier := []string{id}
	var tests []string
	for h := 0; h < hops && len(frontier) > 0; h++ {
		var next []string
		for _, f := range frontier {
			// A call the resolver could not tie to one target (an interface, a local of unknown type) may still reach
			// f: count it, so "no test reaches it" is only said when no call of that name is found either.
			cs := g.callers_[f]
			if s := g.syms[f]; s != nil {
				cs = append(append([]string{}, cs...), g.unresolved[s.Name]...)
			}
			for _, c := range cs {
				if seen[c] {
					continue
				}
				seen[c] = true
				if r, ok := g.ref(c); ok && isTestFile(r.File) {
					tests = append(tests, c)
					continue
				}
				next = append(next, c)
			}
		}
		frontier = next
	}
	return g.refs(tests)
}

// TestReach returns a function that lists the tests reaching a symbol within hops calls. The benchmark uses it to
// compare the call graph with test coverage.
func TestReach(idx *symbols.Index, root string, hops int) func(id string) []Ref {
	g := newGraph(idx, root)
	return func(id string) []Ref { return g.testsReaching(id, hops) }
}
