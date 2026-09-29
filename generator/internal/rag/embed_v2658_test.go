// D051: Embed never read the HTTP status. An error reply from Ollama (for
// example a model that is not pulled) decoded to an empty vector with a nil
// error, so IngestDir stored and counted chunks that Search never returns.
package rag

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// fakeEmbed points OLLAMA_HOST at a test server that answers every request
// with the given status and JSON body.
func fakeEmbed(t *testing.T, status int, body any) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OLLAMA_HOST", srv.URL)
}

func TestEmbed_ErrorStatusIsAnError(t *testing.T) {
	fakeEmbed(t, http.StatusNotFound, map[string]string{"error": `model "nomic-embed-text" not found, try pulling it first`})
	vec, err := Embed("hello")
	if err == nil {
		t.Fatalf("Embed returned %v with a nil error on HTTP 404", vec)
	}
}

func TestEmbed_EmptyVectorIsAnError(t *testing.T) {
	fakeEmbed(t, http.StatusOK, map[string]any{"embedding": []float64{}})
	vec, err := Embed("hello")
	if err == nil {
		t.Fatalf("Embed returned %v with a nil error for an empty embedding", vec)
	}
}

func TestEmbed_GoodReply(t *testing.T) {
	fakeEmbed(t, http.StatusOK, map[string]any{"embedding": []float64{0.1, 0.2, 0.3}})
	vec, err := Embed("hello")
	if err != nil || len(vec) != 3 {
		t.Fatalf("Embed = %v, %v; want a 3 value vector", vec, err)
	}
}

// With an error reply, IngestDir must store no chunk.
func TestIngestDir_ErrorStatusStoresNoChunk(t *testing.T) {
	t.Chdir(t.TempDir())
	fakeEmbed(t, http.StatusInternalServerError, map[string]string{"error": "boom"})
	if err := os.MkdirAll("docs", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("docs", "a.md"), []byte("Fiber routes are registered with app.Get."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := IngestDir("fiber", "docs"); err != nil {
		t.Fatal(err)
	}
	if n := len(Load().Chunks); n != 0 {
		t.Fatalf("store holds %d chunks after every embed failed; want 0", n)
	}
}
