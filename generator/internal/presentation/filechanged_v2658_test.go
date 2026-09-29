// filechanged_v2658_test.go — v26.58 (D088): OnFileChanged dropped the effects
// of every symbol whose ID started with the deleted path. A symbol ID is
// filePath + "#" + kind + ":" + name, so deleting x.ts also dropped the
// effects of x.tsx in the same folder.
package presentation

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func TestOnFileChanged_DeleteKeepsEffectsOfPrefixSibling(t *testing.T) {
	t.Chdir(t.TempDir()) // the store of an unset root writes ./.gen
	s := New(symbols.NewIndex())
	gone := "/p/x.ts#func:A"
	kept := "/p/x.tsx#func:B"
	s.LoadEffects(map[string]*flow.FuncEffects{
		gone: {SymbolID: gone, Effects: map[flow.EffectKind]bool{flow.EffectDB: true}},
		kept: {SymbolID: kept, Effects: map[flow.EffectKind]bool{flow.EffectDB: true}},
	})

	s.OnFileChanged("/p/x.ts", nil, nil, true)

	efx := s.safeEfxCopy()
	if _, ok := efx[gone]; ok {
		t.Errorf("effects of the deleted file x.ts are still present")
	}
	if _, ok := efx[kept]; !ok {
		t.Errorf("deleting x.ts dropped the effects of x.tsx; left: %v", efx)
	}
}
