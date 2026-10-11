// ollama_keepalive_test.go (v26.32): OLLAMA_KEEP_ALIVE encoding and the
// request error counter. Both came out of reading the 2026-09-04 campaign
// results: strategy B set OLLAMA_KEEP_ALIVE=-1, the door sent it as the string
// "-1", Ollama rejected every request with HTTP 400, and nothing counted the
// failures. Each test runs against a local httptest server via OLLAMA_HOST.
package ai

import (
	"encoding/json"
	"net/http"
	"testing"
)

func keepAliveField(t *testing.T, body []byte) (any, bool) {
	t.Helper()
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	v, ok := req["keep_alive"]
	return v, ok
}

// A bare integer must be sent as a JSON number, because Ollama parses string
// values with time.ParseDuration and "-1" has no unit.
func TestKeepAliveBareIntegerIsNumber(t *testing.T) {
	t.Setenv("OLLAMA_STREAM", "")
	t.Setenv("OLLAMA_KEEP_ALIVE", "-1")
	srv, last := captureServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(OllamaResponse{Response: "ok", Done: true})
	})
	t.Setenv("OLLAMA_HOST", srv.URL)
	if _, err := AskLLMWithModelRaw("p", "m"); err != nil {
		t.Fatal(err)
	}
	v, ok := keepAliveField(t, *last)
	if !ok {
		t.Fatal("keep_alive missing from request")
	}
	f, isNum := v.(float64)
	if !isNum || f != -1 {
		t.Fatalf("keep_alive = %#v, want JSON number -1", v)
	}
}

// A duration string stays a string.
func TestKeepAliveDurationIsString(t *testing.T) {
	t.Setenv("OLLAMA_STREAM", "")
	t.Setenv("OLLAMA_KEEP_ALIVE", "30m")
	srv, last := captureServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(OllamaResponse{Response: "ok", Done: true})
	})
	t.Setenv("OLLAMA_HOST", srv.URL)
	if _, err := AskLLMWithModelRaw("p", "m"); err != nil {
		t.Fatal(err)
	}
	v, ok := keepAliveField(t, *last)
	if !ok || v != "30m" {
		t.Fatalf("keep_alive = %#v, want \"30m\"", v)
	}
}

// Unset means omitted, so Ollama's own default applies and the request bytes
// match the pre-v26.32 door.
func TestKeepAliveUnsetIsOmitted(t *testing.T) {
	t.Setenv("OLLAMA_STREAM", "")
	t.Setenv("OLLAMA_KEEP_ALIVE", "")
	srv, last := captureServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(OllamaResponse{Response: "ok", Done: true})
	})
	t.Setenv("OLLAMA_HOST", srv.URL)
	if _, err := AskLLMWithModelRaw("p", "m"); err != nil {
		t.Fatal(err)
	}
	if _, ok := keepAliveField(t, *last); ok {
		t.Fatal("keep_alive should be omitted when OLLAMA_KEEP_ALIVE is unset")
	}
}

// Every failed attempt is counted, and a run with zero completed requests and
// non-zero errors is visible to the caller.
func TestUsageCountsRequestErrors(t *testing.T) {
	t.Setenv("OLLAMA_STREAM", "")
	t.Setenv("OLLAMA_KEEP_ALIVE", "")
	srv, _ := captureServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid duration"}`, http.StatusBadRequest)
	})
	t.Setenv("OLLAMA_HOST", srv.URL)
	ResetUsage()
	if _, err := AskLLMWithModelRaw("p", "m"); err == nil {
		t.Fatal("expected an error from a 400 response")
	}
	u := UsageSnapshot()
	if u.Requests != 0 {
		t.Fatalf("Requests = %d, want 0 (nothing completed)", u.Requests)
	}
	if u.Errors != 3 {
		t.Fatalf("Errors = %d, want 3 (one per retry)", u.Errors)
	}
}
