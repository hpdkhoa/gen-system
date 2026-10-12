// effects_stdlib_v2666_test.go (v26.66): the Effects list showed standard
// library functions with the project's own; on examples/shop 27 of 29 "db"
// entries came from GOROOT. It now hides them unless include_stdlib=1, like
// search and the call graph.
package presentation

import (
	"encoding/json"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func TestEffectsListHidesStdlibUnlessAsked(t *testing.T) {
	t.Chdir(t.TempDir()) // the store of an unset root writes ./.gen
	fn := func(name string) *ast.Node {
		return &ast.Node{Kind: ast.NodeFunction, Name: name, Language: ast.LangGo, Exported: true,
			Span: ast.Span{Start: ast.Pos{Line: 1}}}
	}
	idx := symbols.NewIndex()
	idx.AddModule(&ast.Module{Language: ast.LangGo, FilePath: "/app/store.go", PackageName: "shop",
		Nodes: []*ast.Node{fn("SaveOrder")}})
	idx.AddModuleWithProvenance(&ast.Module{Language: ast.LangGo, FilePath: "/goroot/src/database/sql/ctxutil.go",
		PackageName: "sql", Nodes: []*ast.Node{fn("CtxDriverExec")}}, symbols.ProvStdlib)
	project := symbols.SymbolID("/app/store.go", symbols.SymFunc, "SaveOrder")
	std := symbols.SymbolID("/goroot/src/database/sql/ctxutil.go", symbols.SymFunc, "CtxDriverExec")
	db := map[flow.EffectKind]bool{flow.EffectDB: true}

	s := New(idx)
	s.ReplaceContext(idx, nil, map[string]*flow.FuncEffects{
		project: {SymbolID: project, Effects: db},
		std:     {SymbolID: std, Effects: db},
	})

	dbNames := func(path string) []string {
		var resp EffectsListResponse
		if err := json.Unmarshal(serveV23(t, s, path).Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range resp.ByEffect["db"] {
			names = append(names, e.Name)
		}
		return names
	}
	if got := dbNames("/api/present/v23/effects"); len(got) != 1 || got[0] != "SaveOrder" {
		t.Fatalf("default list = %v, want only the project's SaveOrder", got)
	}
	if got := dbNames("/api/present/v23/effects?include_stdlib=1"); len(got) != 2 {
		t.Fatalf("include_stdlib=1 list = %v, want both functions", got)
	}
}
