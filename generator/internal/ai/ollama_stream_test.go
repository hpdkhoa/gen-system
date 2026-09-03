// ollama_stream_test.go — v26.24 (WP2): the env-driven request options
// (OLLAMA_NUM_GPU / OLLAMA_NUM_CTX / OLLAMA_NUM_BATCH), the OLLAMA_STREAM
// streaming reader, and the usage counters. Every test runs against a local
// httptest server via OLLAMA_HOST — no Ollama needed.
package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureServer records the last request body it received and replies with fn.
func captureServer(t *testing.T, fn http.HandlerFunc) (*httptest.Server, *[]byte) {
	t.Helper()
	var last []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		last = body
		fn(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &last
}

func TestOptionsOmittedWhenEnvUnset(t *testing.T) {
	t.Setenv("OLLAMA_NUM_GPU", "")
	t.Setenv("OLLAMA_NUM_CTX", "")
	t.Setenv("OLLAMA_NUM_BATCH", "")
	t.Setenv("OLLAMA_STREAM", "")
	srv, last := captureServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(OllamaResponse{Response: "ok", Done: true})
	})
	t.Setenv("OLLAMA_HOST", srv.URL)

	if _, err := AskLLMWithModelRaw("p", "m"); err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	if err := json.Unmarshal(*last, &req); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	opts := req["options"].(map[string]any)
	for _, k := range []string{"num_gpu", "num_ctx", "num_batch"} {
		if _, present := opts[k]; present {
			t.Errorf("options.%s should be omitted when env is unset", k)
		}
	}
	if req["stream"] != false {
		t.Errorf("stream should default to false, got %v", req["stream"])
	}
	// The deterministic pins must always be present.
	if opts["temperature"] != 0.0 || opts["seed"] != 42.0 || opts["top_p"] != 1.0 {
		t.Errorf("determinism pins changed: %v", opts)
	}
}

func TestOptionsFromEnv(t *testing.T) {
	t.Setenv("OLLAMA_NUM_GPU", "32")
	t.Setenv("OLLAMA_NUM_CTX", "8192")
	t.Setenv("OLLAMA_NUM_BATCH", "256")
	t.Setenv("OLLAMA_STREAM", "")
	srv, last := captureServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(OllamaResponse{Response: "ok", Done: true})
	})
	t.Setenv("OLLAMA_HOST", srv.URL)

	if _, err := AskLLMWithModelRaw("p", "m"); err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	_ = json.Unmarshal(*last, &req)
	opts := req["options"].(map[string]any)
	if opts["num_gpu"] != 32.0 || opts["num_ctx"] != 8192.0 || opts["num_batch"] != 256.0 {
		t.Errorf("env knobs not passed through: %v", opts)
	}
}

func TestUnparseableEnvKnobIgnored(t *testing.T) {
	t.Setenv("OLLAMA_NUM_GPU", "all") // not an int — must be ignored, not guessed
	if got := envOptInt("OLLAMA_NUM_GPU"); got != nil {
		t.Errorf("unparseable env value should yield nil, got %d", *got)
	}
}

// streamChunks writes the canonical NDJSON stream for "hello world".
func streamChunks(w http.ResponseWriter) {
	for _, frag := range []string{"hel", "lo ", "wor", "ld"} {
		_ = json.NewEncoder(w).Encode(OllamaResponse{Response: frag})
	}
	_ = json.NewEncoder(w).Encode(OllamaResponse{Done: true, PromptEvalCount: 11, EvalCount: 4})
}

func TestStreamingReassembly(t *testing.T) {
	t.Setenv("OLLAMA_STREAM", "1")
	srv, last := captureServer(t, func(w http.ResponseWriter, r *http.Request) {
		streamChunks(w)
	})
	t.Setenv("OLLAMA_HOST", srv.URL)

	got, err := AskLLMWithModelRaw("p", "m")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello world" {
		t.Errorf("reassembled %q, want %q", got, "hello world")
	}
	var req map[string]any
	_ = json.Unmarshal(*last, &req)
	if req["stream"] != true {
		t.Errorf("OLLAMA_STREAM=1 should send stream:true, got %v", req["stream"])
	}
}

func TestStreamingDeterminism(t *testing.T) {
	// Identical streams must reassemble to one unique output across 5 runs —
	// the same invariant the live determinism_check suite asserts on-box.
	t.Setenv("OLLAMA_STREAM", "1")
	srv, _ := captureServer(t, func(w http.ResponseWriter, r *http.Request) {
		streamChunks(w)
	})
	t.Setenv("OLLAMA_HOST", srv.URL)

	outputs := map[string]bool{}
	for i := 0; i < 5; i++ {
		got, err := AskLLMWithModelRaw("p", "m")
		if err != nil {
			t.Fatal(err)
		}
		outputs[got] = true
	}
	if len(outputs) != 1 {
		t.Errorf("5 identical streams produced %d unique outputs, want 1", len(outputs))
	}
}

func TestStreamingModelError(t *testing.T) {
	t.Setenv("OLLAMA_STREAM", "1")
	srv, _ := captureServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(OllamaResponse{Response: "par"})
		_ = json.NewEncoder(w).Encode(OllamaResponse{Error: "model exploded"})
	})
	t.Setenv("OLLAMA_HOST", srv.URL)

	if _, err := AskLLMWithModelRaw("p", "m"); err == nil {
		t.Fatal("mid-stream model error must surface as an error")
	}
}

func TestUsageCounters(t *testing.T) {
	t.Setenv("OLLAMA_STREAM", "")
	srv, _ := captureServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(OllamaResponse{Response: "ok", Done: true, PromptEvalCount: 100, EvalCount: 7})
	})
	t.Setenv("OLLAMA_HOST", srv.URL)

	ResetUsage()
	for i := 0; i < 3; i++ {
		if _, err := AskLLMWithModelRaw(fmt.Sprintf("p%d", i), "m"); err != nil {
			t.Fatal(err)
		}
	}
	u := UsageSnapshot()
	if u.Requests != 3 || u.PromptTokens != 300 || u.EvalTokens != 21 {
		t.Errorf("usage = %+v, want Requests=3 PromptTokens=300 EvalTokens=21", u)
	}
}
