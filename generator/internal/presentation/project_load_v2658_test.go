// project_load_v2658_test.go — v26.58 (D081): handleLoad ran the swap closure
// first and SetRoot after it. The swap rebuilt and saved the beliefs of the new
// project in the store of the old root, and gen-server attached the enricher to
// that old store. SetRoot then made a fresh store with no enricher, so inline
// enrichment stopped after the first Studio load.
package presentation

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/belief"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func TestHandleLoad_SwapSeesStoreOfNewRoot(t *testing.T) {
	base := t.TempDir()
	projA := filepath.Join(base, "a")
	projB := filepath.Join(base, "b")
	for _, d := range []string{projA, projB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	srv := New(symbols.NewIndex())
	srv.SetRoot(projA, "", "")

	var seen *belief.Store
	loader := func(root string, _ ProjectLoadOpts) (string, func(*Server), error) {
		return root, func(ps *Server) {
			ps.ReplaceContext(symbols.NewIndex(), nil, nil)
			// gen-server attaches the enricher to this store here.
			seen = ps.BeliefStore()
		}, nil
	}
	pm := NewProjectManager(srv, base, loader, nil)
	mux := http.NewServeMux()
	pm.Register(mux)

	body, _ := json.Marshal(map[string]string{"root": projB})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/project/load", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("load: status %d; body: %s", rec.Code, rec.Body.String())
	}
	if seen == nil || seen != srv.BeliefStore() {
		t.Errorf("the swap saw a store that the server no longer uses; an enricher attached there is lost")
	}
	if _, err := os.Stat(filepath.Join(projA, ".gen", "beliefs.json")); err == nil {
		t.Errorf("loading project b wrote the beliefs file of project a")
	}
}
