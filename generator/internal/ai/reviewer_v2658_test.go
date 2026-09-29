// generator/internal/ai/reviewer_v2658_test.go — v26.58.
//
// D046: ReviewCode sliced resp[start:end+1] when the last } stood before the
// first {, so an answer such as "done} now {" panicked with slice bounds out
// of range. The run had no recover and stopped.
package ai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// fakeModel serves answer as the model reply on a local httptest server.
func fakeModel(t *testing.T, answer string) {
	t.Helper()
	t.Setenv("OLLAMA_STREAM", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(OllamaResponse{Response: answer, Done: true})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OLLAMA_HOST", srv.URL)
}

func TestReviewCode_BracesReversedDoesNotPanic(t *testing.T) {
	fakeModel(t, "done} now {")
	path := filepath.Join(t.TempDir(), "x.go")
	if err := os.WriteFile(path, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ReviewCode panicked: %v", r)
		}
	}()
	res := ReviewCode("generate_handler", "Order", path)
	if !res.Safe {
		t.Errorf("an answer with no JSON object should give Safe true, got %+v", res)
	}
}
