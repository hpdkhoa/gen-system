// index_determinism_test.go — v26.25: bare-name resolution must be a function of
// the input alone.
//
// resolveByNameLocked used to range over idx.allSymbols (a map) and keep the
// first match in each tier. Go randomizes map order, so when two symbols in the
// SAME tier shared a bare name the winner varied between runs on identical
// input, and the call graph varied with it. The determinism contract the rest of
// the system relies on (temperature 0, fixed seed, byte-identical output) does
// not hold if the deterministic half is itself unstable.
//
// These tests fail reliably against the old implementation because the ambiguity
// is resolved many times over many freshly built indexes.
package symbols

import (
	"fmt"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

// buildAmbiguousIndex creates n files each defining an exported func with the
// SAME bare name, so resolution has to break a tie.
func buildAmbiguousIndex(n int) *Index {
	idx := NewIndex()
	for i := 0; i < n; i++ {
		path := fmt.Sprintf("pkg%02d/handler.go", i)
		idx.AddModule(makeModule(path, funcNode("Handle", true)))
	}
	return idx
}

func TestResolveByName_DeterministicAcrossFreshIndexes(t *testing.T) {
	// A fresh Index per iteration means a fresh map, so Go's per-map iteration
	// randomization gets a genuine chance to produce a different winner.
	var first string
	for i := 0; i < 50; i++ {
		idx := buildAmbiguousIndex(8)
		idx.mu.RLock()
		got := idx.resolveByNameLocked("Handle")
		idx.mu.RUnlock()
		if got == nil {
			t.Fatalf("iteration %d: no symbol resolved for %q", i, "Handle")
		}
		if i == 0 {
			first = got.ID
			continue
		}
		if got.ID != first {
			t.Fatalf("iteration %d resolved %q, iteration 0 resolved %q — "+
				"bare-name resolution is not deterministic", i, got.ID, first)
		}
	}
	t.Logf("stable winner across 50 fresh indexes: %s", first)
}

func TestResolveByName_PicksLowestSymbolID(t *testing.T) {
	// The documented tie-break: sort candidates by their unique, stable
	// SymbolID and take the first. Pinning it means a future refactor that
	// changes the rule has to say so out loud.
	idx := buildAmbiguousIndex(5)
	idx.mu.RLock()
	got := idx.resolveByNameLocked("Handle")
	idx.mu.RUnlock()
	want := SymbolID("pkg00/handler.go", SymFunc, "Handle")
	if got == nil || got.ID != want {
		t.Fatalf("resolveByNameLocked = %v, want lowest ID %q", got, want)
	}
}

func TestResolveAll_StableCallEdgesAcrossRuns(t *testing.T) {
	// End to end: the same input must produce the same resolved call graph.
	run := func() map[string]string {
		idx := NewIndex()
		for i := 0; i < 6; i++ {
			idx.AddModule(makeModule(fmt.Sprintf("pkg%02d/handler.go", i),
				funcNode("Handle", true)))
		}
		idx.AddModule(makeModule("main.go", callerWith("Run", "Handle")))
		idx.ResolveAll()

		out := map[string]string{}
		for _, e := range idx.AllCallEdges() {
			if e.CalleeName == "Handle" {
				out[e.CallerID] = e.CalleeID
			}
		}
		return out
	}

	base := run()
	if len(base) == 0 {
		t.Fatal("no Handle call edges produced; fixture is wrong")
	}
	for i := 0; i < 25; i++ {
		got := run()
		for caller, callee := range base {
			if got[caller] != callee {
				t.Fatalf("run %d: %s resolved to %q, baseline had %q — "+
					"call graph is not reproducible", i, caller, got[caller], callee)
			}
		}
	}
}

// BuildFromModules is a SECOND entry point into resolution: it calls
// resolveCallEdges, which until v26.26 used a different resolver than
// ResolveAll did (resolveByName, unsorted map order, no provenance tier). So
// the earlier determinism test passed while the identical bug survived next
// door, on the path cmd/bench actually uses. This test covers that path.
func TestBuildFromModules_StableCallGraphAcrossRuns(t *testing.T) {
	run := func() map[string]string {
		var mods []*ast.Module
		for i := 0; i < 6; i++ {
			mods = append(mods, makeModule(fmt.Sprintf("pkg%02d/handler.go", i),
				funcNode("Handle", true)))
		}
		mods = append(mods, makeModule("main.go", callerWith("Run", "Handle")))

		idx := BuildFromModules(mods)
		out := map[string]string{}
		for _, e := range idx.AllCallEdges() {
			if e.CalleeName == "Handle" {
				out[e.CallerID] = e.CalleeID
			}
		}
		return out
	}

	base := run()
	if len(base) == 0 {
		t.Fatal("no Handle call edges produced; fixture is wrong")
	}
	for i := 0; i < 25; i++ {
		got := run()
		for caller, callee := range base {
			if got[caller] != callee {
				t.Fatalf("run %d: %s resolved to %q, baseline had %q — "+
					"BuildFromModules call graph is not reproducible", i, caller, got[caller], callee)
			}
		}
	}
}

// ByName is a public accessor whose result several callers walk in order,
// including context.go's derived-view builders. It must not hand out a
// randomized slice.
func TestByName_DeterministicOrder(t *testing.T) {
	var first []string
	for i := 0; i < 30; i++ {
		idx := buildAmbiguousIndex(6)
		var ids []string
		for _, s := range idx.ByName("Handle") {
			ids = append(ids, s.ID)
		}
		if len(ids) != 6 {
			t.Fatalf("expected 6 symbols named Handle, got %d", len(ids))
		}
		if i == 0 {
			first = ids
			continue
		}
		for k := range ids {
			if ids[k] != first[k] {
				t.Fatalf("iteration %d: ByName order %v differs from %v", i, ids, first)
			}
		}
	}
}
