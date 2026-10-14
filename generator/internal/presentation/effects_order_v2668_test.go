// effects_order_v2668_test.go (v26.68): routines with the same name in
// different files (common for COBOL paragraphs) came back from the Effects
// list in map order, so two loads of one project gave two answers.
package presentation

import (
	"encoding/json"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func TestEffectsListOrderIsStableForEqualNames(t *testing.T) {
	t.Chdir(t.TempDir()) // the store of an unset root writes ./.gen
	idx := symbols.NewIndex()
	efx := map[string]*flow.FuncEffects{}
	var want []string
	for _, f := range []string{"/cbl/a.cob", "/cbl/b.cob", "/cbl/c.cob", "/cbl/d.cob"} {
		idx.AddModule(&ast.Module{Language: ast.LangCOBOL, FilePath: f, PackageName: "P",
			Nodes: []*ast.Node{{Kind: ast.NodeFunction, Name: "MAIN-PARA", Language: ast.LangCOBOL}}})
		id := symbols.SymbolID(f, symbols.SymFunc, "MAIN-PARA")
		efx[id] = &flow.FuncEffects{SymbolID: id, Effects: map[flow.EffectKind]bool{flow.EffectFS: true}}
		want = append(want, id)
	}
	s := New(idx)
	s.ReplaceContext(idx, nil, efx)
	for i := 0; i < 20; i++ {
		var resp EffectsListResponse
		if err := json.Unmarshal(serveV23(t, s, "/api/present/v23/effects").Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, e := range resp.ByEffect["fs"] {
			got = append(got, e.SymbolID)
		}
		if len(got) != len(want) {
			t.Fatalf("got %d entries, want %d", len(got), len(want))
		}
		for k := range want {
			if got[k] != want[k] {
				t.Fatalf("load %d: order %v, want %v", i, got, want)
			}
		}
	}
}
