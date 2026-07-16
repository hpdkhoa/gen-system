// generator/internal/ollama.go — v23.21: HTTP timeout, Ollama host, and
// keep_alive are now configurable via env (OLLAMA_TIMEOUT seconds, OLLAMA_HOST,
// OLLAMA_KEEP_ALIVE). Default timeout raised 90s→600s because running two
// models (a planner model and a code model) means a cold model load (30–90s)
// can happen mid-run on top of generation time; 90s was too tight. keep_alive
// keeps a model resident between calls so the second phase doesn't reload.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ollamaTimeout returns the per-request timeout. OLLAMA_TIMEOUT is in seconds;
// default 600s (10 min) to absorb cold-loading a 14B model plus generation.
func ollamaTimeout() time.Duration {
	if s := os.Getenv("OLLAMA_TIMEOUT"); s != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 600 * time.Second
}

// ollamaURL returns the generate endpoint, honouring OLLAMA_HOST.
func ollamaURL() string {
	host := strings.TrimRight(os.Getenv("OLLAMA_HOST"), "/")
	if host == "" {
		host = "http://localhost:11434"
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}
	return host + "/api/generate"
}

// keepAlive returns the OLLAMA_KEEP_ALIVE value (e.g. "30m") or "" if unset, in
// which case Ollama's own default applies. Kept as a string because Ollama
// accepts durations or seconds.
func keepAlive() string {
	return strings.TrimSpace(os.Getenv("OLLAMA_KEEP_ALIVE"))
}

type OllamaRequest struct {
	Model     string          `json:"model"`
	Prompt    string          `json:"prompt"`
	Stream    bool            `json:"stream"`
	Options   *OllamaOptions  `json:"options,omitempty"`
	Format    json.RawMessage `json:"format,omitempty"`     // v23.1 round 3: constrain output
	KeepAlive string          `json:"keep_alive,omitempty"` // v23.21: keep model warm
}

// OllamaOptions — deterministic guardrails
type OllamaOptions struct {
	Temperature float64 `json:"temperature"`
	Seed        int     `json:"seed"`
	TopP        float64 `json:"top_p"`
}

type OllamaResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

// DefaultOptions: deterministic generation
var DefaultOptions = &OllamaOptions{
	Temperature: 0.0,
	Seed:        42,
	TopP:        1.0,
}

func AskLLM(prompt string) (string, error) {
	return AskLLMWithModelRaw(prompt, Model())
}

// AskLLMJSON calls the model with Ollama's structured-output constraint. If
// schema is non-nil it must be a JSON Schema object and the model is forced to
// produce output matching it; if schema is nil, plain JSON mode ("format":
// "json") is used. Either way the model CANNOT emit the bracket-mismatched
// garbage that free-form prompting produced. v23.1 round 3 — root-cause fix.
func AskLLMJSON(prompt string, model string, schema json.RawMessage) (string, error) {
	format := schema
	if len(format) == 0 {
		format = json.RawMessage(`"json"`)
	}
	return askWithRetryFmt(context.Background(), prompt, model, format, 3)
}

func AskLLMWithModelRaw(prompt string, model string) (string, error) {
	return askWithRetryFmt(context.Background(), prompt, model, nil, 3)
}

func askWithRetryFmt(ctx context.Context, prompt, model string, format json.RawMessage, retries int) (string, error) {
	var lastErr error
	for i := 0; i < retries; i++ {
		resp, err := doRequest(ctx, prompt, model, format)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		time.Sleep(time.Duration(500*(i+1)) * time.Millisecond)
	}
	return "", lastErr
}

func doRequest(ctx context.Context, prompt string, model string, format json.RawMessage) (string, error) {
	reqBody := OllamaRequest{
		Model:     model,
		Prompt:    prompt,
		Stream:    false,
		Options:   DefaultOptions,
		Format:    format,
		KeepAlive: keepAlive(),
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		ollamaURL(),
		bytes.NewBuffer(data),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	// Build the client from the current (post-.env) timeout so OLLAMA_TIMEOUT
	// loaded at startup is honoured regardless of package-init ordering.
	client := &http.Client{Timeout: ollamaTimeout()}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", errors.New("ollama http error: " + resp.Status + " body=" + string(body))
	}

	var result OllamaResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}

	if result.Error != "" {
		return "", errors.New("ollama model error: " + result.Error)
	}

	return result.Response, nil
}
