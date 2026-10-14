package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// v26.68: the read only server hides "Build from idea" and the Self-healing
// panel, which belong to the generation half.
func TestHandlerServesTheStudioReadOnly(t *testing.T) {
	h := Handler()
	for _, path := range []string{"/", "/index.html", "/studio-v23.html"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, "Belief inbox") {
			t.Fatalf("%s: status %d, want the Studio page", path, rec.Code)
		}
		if strings.Count(body, `id="read-only-studio"`) != 1 {
			t.Fatalf("%s: want the read only style exactly once", path)
		}
		if strings.Index(body, `id="read-only-studio"`) > strings.Index(body, "</head>") {
			t.Fatalf("%s: the read only style must sit in the head", path)
		}
	}
}
