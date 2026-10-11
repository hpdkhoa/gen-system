// save_v2658_test.go (v26.58, follow up of D020): Run ignored the result of
// UpsertEnrichBeliefs, so a routine whose beliefs could not be saved still
// counted as enriched and gen-enrich reported them as written.
package enrich

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/belief"
)

func TestRun_CountsFailedSaves(t *testing.T) {
	answer := `{"observations":[{"kind":"action","description":"places an order"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"response": answer, "done": true})
	}))
	defer srv.Close()
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("OLLAMA_STREAM", "")

	root := t.TempDir()
	store := belief.NewStore(root)
	// A file where the .gen folder should be makes every save fail.
	if err := os.RemoveAll(filepath.Join(root, ".gen")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gen"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := New().Run(store, []Routine{{SymbolID: "a.go#func:PlaceOrder", Subject: "PlaceOrder", Name: "PlaceOrder", Language: "go"}})
	if res.SaveErrors != 1 || res.Enriched != 0 || res.Beliefs != 0 {
		t.Fatalf("a lost save must not count as enriched: %+v", res)
	}
}
