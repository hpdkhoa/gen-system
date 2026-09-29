// D019: reattachEnrichLocked and reattachSummariesLocked only ever set Stale to
// true. Once a routine body changed, or the routine left the index, or a summary
// member changed, the flag stayed true after the source returned to the first
// state, and saveLocked kept it on disk.
package belief

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func TestD019_EnrichStaleClearsWhenBodyReturns(t *testing.T) {
	root := t.TempDir()
	idx := funcIndex("Checkout", nil)
	bs := NewStore(root)
	body := "func Checkout(){ v1() }"
	bs.SetBodyProvider(func(string) string { return body })
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	sid := symIDFor(bs, KindAction)
	bs.UpsertEnrichBeliefs([]EnrichInput{{SymbolID: sid, Subject: "Checkout", Kind: KindAction, Object: "does v1", Body: body}})

	body = "func Checkout(){ v2() }"
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if b := findBeliefByKindSrc(bs, KindAction, SourceEnrich); b == nil || !b.Stale {
		t.Fatalf("setup: belief should be stale on body v2: %+v", b)
	}
	body = "func Checkout(){ v1() }"
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if b := findBeliefByKindSrc(bs, KindAction, SourceEnrich); b == nil || b.Stale {
		t.Fatalf("body is back to v1 but the belief is still stale: %+v", b)
	} else if b.ValidationNote == staleSourceNote {
		t.Fatalf("stale note kept after the body came back: %+v", b)
	}
}

func TestD019_EnrichStaleClearsWhenRoutineReturns(t *testing.T) {
	root := t.TempDir()
	idx := funcIndex("Checkout", nil)
	bs := NewStore(root)
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	sid := symIDFor(bs, KindAction)
	bs.UpsertEnrichBeliefs([]EnrichInput{{SymbolID: sid, Subject: "Checkout", Kind: KindAction, Object: "does it", Body: "b"}})

	empty := symbols.NewIndex()
	empty.ResolveAll()
	bs.RebuildFromIndex(empty, map[string]*flow.FuncEffects{})
	if b := findBeliefByKindSrc(bs, KindAction, SourceEnrich); b == nil || !b.Stale {
		t.Fatalf("setup: belief should be stale while the routine is absent: %+v", b)
	}
	bs.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if b := findBeliefByKindSrc(bs, KindAction, SourceEnrich); b == nil || b.Stale {
		t.Fatalf("routine is back but the belief is still stale: %+v", b)
	}
	bs2 := NewStore(root)
	bs2.RebuildFromIndex(idx, map[string]*flow.FuncEffects{})
	if b := findBeliefByKindSrc(bs2, KindAction, SourceEnrich); b == nil || b.Stale {
		t.Fatalf("after reload the belief is still stale: %+v", b)
	}
}

func TestD019_SummaryStaleClearsWhenMemberReturns(t *testing.T) {
	root := t.TempDir()
	v1 := []ast.Parameter{{Name: "id", Type: ast.TypeRef{Name: "int"}}}
	v2 := []ast.Parameter{{Name: "total", Type: ast.TypeRef{Name: "float64"}}}
	bs := NewStore(root)
	bs.RebuildFromIndex(funcIndex("SaveOrder", v1), nil)
	var members []string
	for _, b := range bs.All() {
		members = append(members, b.ID)
	}
	bs.UpsertSummary(SummaryInput{Scope: ScopeModule, Name: "svc", Object: "saves orders", MemberIDs: members})

	bs.RebuildFromIndex(funcIndex("SaveOrder", v2), nil)
	if n := bs.StaleSummaryCount(); n != 1 {
		t.Fatalf("setup: StaleSummaryCount = %d after the member changed, want 1", n)
	}
	bs.RebuildFromIndex(funcIndex("SaveOrder", v1), nil)
	if n := bs.StaleSummaryCount(); n != 0 {
		t.Fatalf("member is back but StaleSummaryCount = %d, want 0", n)
	}
}
