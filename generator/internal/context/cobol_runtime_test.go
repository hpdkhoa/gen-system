// context/cobol_runtime_test.go — v23.33 (COBOL runtime catalog): proves the
// synthetic catalog resolves COBOL intrinsic (FUNCTION) and runtime-service
// (CALL) references to ProvStdlib symbols, while leaving a genuinely missing
// program unresolved. This is the COBOL analog of the Go/Java stdlib proofs and
// reuses the identical provenance machinery.
package context

import (
	"path/filepath"
	"testing"

	genast "gen-system/generator/internal/ast"
	"gen-system/generator/internal/symbols"
)

func TestCobolRuntimeCatalog_Resolution(t *testing.T) {
	proj := filepath.Join("testdata", "cobol_proj")
	opts := LoadContextOpts{
		StdlibRoots: []StdlibRoot{{
			Path:      "cobol",
			Lang:      genast.LangCOBOL,
			Synthetic: true,
		}},
	}
	ctx := LoadContextWithOpts(proj, opts)
	if ctx.Index == nil {
		t.Fatal("nil index")
	}

	// 1) Catalog symbols are present and tagged ProvStdlib.
	var stdlibSyms int
	wantNames := map[string]bool{"NUMVAL": false, "CURRENT-DATE": false, "MAX": false, "CEEMOUT": false, "CEE3ABD": false}
	for _, s := range ctx.Index.AllSymbols() {
		if s.Provenance == symbols.ProvStdlib {
			stdlibSyms++
			if _, ok := wantNames[s.Name]; ok {
				wantNames[s.Name] = true
			}
		}
	}
	if stdlibSyms == 0 {
		t.Fatal("no ProvStdlib symbols — catalog not loaded")
	}
	for n, seen := range wantNames {
		if !seen {
			t.Errorf("expected catalog symbol %q to be present", n)
		}
	}

	// 2) Classify the project's call edges by resolution target.
	var intoStdlib, intoProject, unresolved int
	for _, e := range ctx.Index.AllCallEdges() {
		caller, ok := ctx.Index.ByID(e.CallerID)
		if !ok || caller == nil || caller.Provenance.IsExternal() {
			continue
		}
		if e.CalleeID == "" {
			unresolved++
			continue
		}
		callee, ok := ctx.Index.ByID(e.CalleeID)
		if !ok || callee == nil {
			unresolved++
			continue
		}
		if callee.Provenance == symbols.ProvStdlib {
			intoStdlib++
		} else {
			intoProject++
		}
	}

	// FUNCTION NUMVAL / CURRENT-DATE / MAX + CALL CEEMOUT / CEE3ABD = 5 into stdlib.
	if intoStdlib < 5 {
		t.Errorf("expected >=5 edges resolved into the COBOL runtime catalog, got %d", intoStdlib)
	}
	// CALL "CALC-TAX" resolves to the internal paragraph.
	if intoProject < 1 {
		t.Errorf("expected >=1 edge into a project paragraph (CALC-TAX), got %d", intoProject)
	}
	// CALL "MISSING-PROG" must stay unresolved — the catalog never invents it.
	if unresolved < 1 {
		t.Errorf("expected the genuinely missing program to stay unresolved, got %d unresolved", unresolved)
	}
	t.Logf("COBOL catalog: stdlib_syms=%d into_stdlib=%d into_project=%d unresolved=%d",
		stdlibSyms, intoStdlib, intoProject, unresolved)
}
