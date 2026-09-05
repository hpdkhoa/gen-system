// generator/internal/ollama.go — v26.24 (WP2): GPU/context knobs and streaming
// are now configurable via env, so the offload/streaming benches can measure the
// real production door instead of a bench-only shim. OLLAMA_NUM_GPU (layers to
// offload), OLLAMA_NUM_CTX, OLLAMA_NUM_BATCH are passed through as request
// options only when set; OLLAMA_STREAM=1 turns on streaming reads. The streaming
// path reassembles the exact response text (fragment concatenation), so
// extractJSON / ExtractBodyOnly and the determinism pins see byte-identical
// input either way. Token usage (prompt_eval_count / eval_count) is accumulated
// per process and exposed via UsageSnapshot for genbench.
// Prior (v23.21): HTTP timeout, Ollama host, and keep_alive configurable via env
// (OLLAMA_TIMEOUT seconds, OLLAMA_HOST, OLLAMA_KEEP_ALIVE). Default timeout
// raised 90s→600s because running two models (a planner model and a code model)
// means a cold model load (30–90s) can happen mid-run on top of generation time;
// 90s was too tight. keep_alive keeps a model resident between calls so the
// second phase doesn't reload.
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
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

// keepAlive returns the OLLAMA_KEEP_ALIVE value for the request body, or nil
// when unset (Ollama's own default applies).
//
// v26.32: a bare integer such as "-1" (keep forever) or "0" (unload at once)
// is sent as a JSON NUMBER. Ollama parses keep_alive strings with
// time.ParseDuration, which rejects "-1" ("missing unit"), so the whole
// request came back HTTP 400. The 2026-09-04 campaign hit exactly this:
// strategy B set OLLAMA_KEEP_ALIVE=-1, every model call failed, the planner
// fell back for all five ideas, and the row measured nothing. Durations such
// as "30m" are still sent as strings.
func keepAlive() any {
	v := strings.TrimSpace(os.Getenv("OLLAMA_KEEP_ALIVE"))
	if v == "" {
		return nil
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return v
}

type OllamaRequest struct {
	Model     string          `json:"model"`
	Prompt    string          `json:"prompt"`
	Stream    bool            `json:"stream"`
	Options   *OllamaOptions  `json:"options,omitempty"`
	Format    json.RawMessage `json:"format,omitempty"`     // v23.1 round 3: constrain output
	KeepAlive any             `json:"keep_alive,omitempty"` // v23.21: keep model warm; v26.32: number or duration string
}

// OllamaOptions — deterministic guardrails, plus optional GPU/context knobs.
// The three pointer fields are nil (and omitted from the request JSON) unless
// their env var is set, so a box that never sets them sends byte-identical
// requests to the pre-v26.24 code.
type OllamaOptions struct {
	Temperature float64 `json:"temperature"`
	Seed        int     `json:"seed"`
	TopP        float64 `json:"top_p"`
	NumGPU      *int    `json:"num_gpu,omitempty"`   // OLLAMA_NUM_GPU: layers to offload to GPU
	NumCtx      *int    `json:"num_ctx,omitempty"`   // OLLAMA_NUM_CTX: context window size
	NumBatch    *int    `json:"num_batch,omitempty"` // OLLAMA_NUM_BATCH: prompt batch size
}

// OllamaResponse is one /api/generate response object. In streaming mode every
// NDJSON line is one of these; the final line has Done=true and carries the
// token counts.
type OllamaResponse struct {
	Response        string `json:"response"`
	Error           string `json:"error,omitempty"`
	Done            bool   `json:"done,omitempty"`
	PromptEvalCount int    `json:"prompt_eval_count,omitempty"`
	EvalCount       int    `json:"eval_count,omitempty"`
}

// DefaultOptions: deterministic generation
var DefaultOptions = &OllamaOptions{
	Temperature: 0.0,
	Seed:        42,
	TopP:        1.0,
}

// envOptInt reads an integer env var, returning nil when unset or unparseable
// (an unparseable value is ignored rather than guessed at).
func envOptInt(key string) *int {
	s := strings.TrimSpace(os.Getenv(key))
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &n
}

// requestOptions returns the per-request options: the deterministic pins plus
// any GPU/context knobs set in the environment.
func requestOptions() *OllamaOptions {
	o := *DefaultOptions
	o.NumGPU = envOptInt("OLLAMA_NUM_GPU")
	o.NumCtx = envOptInt("OLLAMA_NUM_CTX")
	o.NumBatch = envOptInt("OLLAMA_NUM_BATCH")
	return &o
}

// streamEnabled reports whether OLLAMA_STREAM opts into streaming reads
// ("1" or "true"). Off by default: the buffered path is the long-verified one.
func streamEnabled() bool {
	v := strings.TrimSpace(os.Getenv("OLLAMA_STREAM"))
	return v == "1" || strings.EqualFold(v, "true")
}

// ───────────────────────── token usage accounting ────────────────────────────

// Usage is the process-wide token usage accumulated across every doRequest call
// since the last ResetUsage. genbench snapshots it around a pipeline run to
// report prompt_tokens / eval_tokens per benchmark task.
type Usage struct {
	Requests     int // completed requests (a response with token counts)
	PromptTokens int
	EvalTokens   int
	Errors       int // v26.32: failed attempts (transport, HTTP status, model error)
}

// recordRequestError counts one failed attempt. genbench reads it so a run in
// which every call failed cannot pass as "measured": zero requests with
// non-zero errors is the signature of a dead door, and the 2026-09-04
// strategy B rows had exactly that shape with nothing recording it.
func recordRequestError() {
	usageMu.Lock()
	usage.Errors++
	usageMu.Unlock()
}

var (
	usageMu sync.Mutex
	usage   Usage
)

// ResetUsage zeroes the process-wide usage counters.
func ResetUsage() {
	usageMu.Lock()
	usage = Usage{}
	usageMu.Unlock()
}

// UsageSnapshot returns a copy of the usage counters.
func UsageSnapshot() Usage {
	usageMu.Lock()
	defer usageMu.Unlock()
	return usage
}

func recordUsage(promptTokens, evalTokens int) {
	usageMu.Lock()
	usage.Requests++
	usage.PromptTokens += promptTokens
	usage.EvalTokens += evalTokens
	usageMu.Unlock()
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
		recordRequestError()
		lastErr = err
		time.Sleep(time.Duration(500*(i+1)) * time.Millisecond)
	}
	return "", lastErr
}

func doRequest(ctx context.Context, prompt string, model string, format json.RawMessage) (string, error) {
	stream := streamEnabled()
	reqBody := OllamaRequest{
		Model:     model,
		Prompt:    prompt,
		Stream:    stream,
		Options:   requestOptions(),
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

	if stream {
		return readStream(resp)
	}

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

	recordUsage(result.PromptEvalCount, result.EvalCount)
	return result.Response, nil
}

// readStream consumes an NDJSON /api/generate stream and reassembles the full
// response text. Concatenating the fragments yields exactly the string the
// buffered path returns for the same generation, so every downstream consumer
// (extractJSON, ExtractBodyOnly, the schema-constrained parsers) is unaffected
// by the transport choice. A model-level error in any chunk aborts with that
// error; the final Done chunk carries the token counts for usage accounting.
func readStream(resp *http.Response) (string, error) {
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", errors.New("ollama http error: " + resp.Status + " body=" + string(body))
	}

	var full strings.Builder
	sc := bufio.NewScanner(resp.Body)
	// A single fragment is small, but leave generous headroom (1 MiB per line).
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var promptTokens, evalTokens int
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var chunk OllamaResponse
		if err := json.Unmarshal(line, &chunk); err != nil {
			return "", err
		}
		if chunk.Error != "" {
			return "", errors.New("ollama model error: " + chunk.Error)
		}
		full.WriteString(chunk.Response)
		if chunk.Done {
			promptTokens = chunk.PromptEvalCount
			evalTokens = chunk.EvalCount
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	recordUsage(promptTokens, evalTokens)
	return full.String(), nil
}
