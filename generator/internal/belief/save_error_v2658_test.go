// D020: saveLocked dropped every write error. With <root>/.gen not writable,
// Correct returned nil and the server answered ok, but the correction never
// reached disk and was gone after a restart.
package belief

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/flow"
)

// blockedStore returns a store whose .gen path is a plain file, so no save works.
func blockedStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gen"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bs := NewStore(root)
	bs.RebuildFromIndex(funcIndex("Checkout", nil), map[string]*flow.FuncEffects{})
	return bs
}

func TestD020_CorrectReportsSaveError(t *testing.T) {
	bs := blockedStore(t)
	id := beliefOfKind(t, bs, KindAction).ID
	if err := bs.Correct(Correction{BeliefID: id, CorrectedTo: "pays for the cart"}); err == nil {
		t.Fatal("Correct returned nil although the belief file cannot be written")
	}
}

func TestD020_UpsertsReportSaveError(t *testing.T) {
	bs := blockedStore(t)
	sid := symIDFor(bs, KindAction)
	if err := bs.UpsertEnrichBeliefs([]EnrichInput{{SymbolID: sid, Subject: "Checkout", Kind: KindAction, Object: "pays", Body: "b"}}); err == nil {
		t.Error("UpsertEnrichBeliefs returned nil although the belief file cannot be written")
	}
	if err := bs.UpsertSummary(SummaryInput{Scope: ScopeService, Name: "svc", Object: "pays"}); err == nil {
		t.Error("UpsertSummary returned nil although the belief file cannot be written")
	}
}

func TestD020_SaveWorksWhenWritable(t *testing.T) {
	root := t.TempDir()
	bs := NewStore(root)
	bs.RebuildFromIndex(funcIndex("Checkout", nil), map[string]*flow.FuncEffects{})
	id := beliefOfKind(t, bs, KindAction).ID
	if err := bs.Correct(Correction{BeliefID: id, CorrectedTo: "pays for the cart"}); err != nil {
		t.Fatalf("Correct: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gen", "beliefs.json")); err != nil {
		t.Fatalf("beliefs.json not written: %v", err)
	}
}
