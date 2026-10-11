// discard_v2658_test.go (v26.58, D091): DiscardStage had no method test, and
// the /api/evolve/discard route passes every method except OPTIONS. A GET, for
// example a link prefetch, removed the staging folder and the pending proposal.
// StageEvolve and CommitStage already answer 405 to anything but POST.
package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/services"
)

func TestDiscardStage_OnlyPost(t *testing.T) {
	h := New(nil, nil, nil, nil, services.NewStagingService(nil, nil, t.TempDir()))

	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodHead} {
		rec := httptest.NewRecorder()
		h.DiscardStage(rec, httptest.NewRequest(m, "/api/evolve/discard", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status %d, want 405", m, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	h.DiscardStage(rec, httptest.NewRequest(http.MethodPost, "/api/evolve/discard", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("POST: status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}
