// callgraph_both_v2658_test.go — v26.58 (D083): with direction=both the
// callgraph handler tested only direction == "callers" when it wrote an edge,
// so a caller of the root took the callee branch and came out as root -> caller.
// The graph showed the root as the caller of its own caller.
package presentation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func callChainServer() (*Server, string, string, string) {
	const file = "/project/chain/chain.go"
	fn := func(name string, calls ...string) *ast.Node {
		n := &ast.Node{ID: "func:" + name, Kind: ast.NodeFunction, Name: name, Language: ast.LangGo, Exported: true}
		for _, c := range calls {
			n.Children = append(n.Children, &ast.Node{Kind: ast.NodeCall, Callee: c})
		}
		return n
	}
	m := &ast.Module{
		Language: ast.LangGo, FilePath: file, PackageName: "chain", Checksum: "c1",
		Nodes: []*ast.Node{fn("Caller", "Mid"), fn("Mid", "Leaf"), fn("Leaf")},
	}
	idx := symbols.NewIndex()
	idx.AddModule(m)
	idx.ResolveAll()
	id := func(n string) string { return symbols.SymbolID(file, symbols.SymFunc, n) }
	return New(idx), id("Caller"), id("Mid"), id("Leaf")
}

func callgraphEdges(t *testing.T, s *Server, rootID, direction string) []CallgraphEdge {
	t.Helper()
	mux := http.NewServeMux()
	s.RegisterV23Routes(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/present/v23/callgraph?depth=1&direction="+direction+"&root="+rootID, nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp CallgraphResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Edges
}

func hasEdge(edges []CallgraphEdge, from, to string) bool {
	for _, e := range edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}

func TestCallgraph_BothKeepsEdgeDirection(t *testing.T) {
	s, caller, mid, leaf := callChainServer()

	if e := callgraphEdges(t, s, mid, "callers"); !hasEdge(e, caller, mid) {
		t.Fatalf("setup: direction=callers has no edge Caller -> Mid: %+v", e)
	}
	edges := callgraphEdges(t, s, mid, "both")
	if !hasEdge(edges, caller, mid) {
		t.Errorf("direction=both: want edge Caller -> Mid; edges: %+v", edges)
	}
	if hasEdge(edges, mid, caller) {
		t.Errorf("direction=both: edge Mid -> Caller points the wrong way; edges: %+v", edges)
	}
	if !hasEdge(edges, mid, leaf) {
		t.Errorf("direction=both: want edge Mid -> Leaf; edges: %+v", edges)
	}
}
