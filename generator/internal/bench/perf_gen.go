// generator/internal/bench/perf_gen.go — v19.7: end-to-end generation
// latency, determinism check, and Ollama doctor.
//
// We bypass the project's ai.AskLLM wrapper so the bench can:
//   - target an arbitrary model (deepseek-coder-v2:16b-lite-instruct-q5_K_M)
//   - measure each call independently (cold vs warm)
//   - record token counts and timings from Ollama's own response (eval_count,
//     eval_duration, prompt_eval_count, prompt_eval_duration)
//   - confirm the model is actually loaded before we start counting
//
// All HTTP traffic is to OLLAMA_HOST (default http://localhost:11434).
package bench

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultOllamaHost = "http://localhost:11434"

// ollamaGenerate is one /api/generate call. Returns the response text plus
// the raw timings reported by Ollama.
type ollamaTimings struct {
	Response           string
	EvalCount          int
	EvalDuration       int64 // nanoseconds
	PromptEvalCount    int
	PromptEvalDuration int64 // nanoseconds
	TotalDuration      int64 // nanoseconds (wall, reported by ollama)
	LoadDuration       int64 // nanoseconds
}

type ollamaReq struct {
	Model   string      `json:"model"`
	Prompt  string      `json:"prompt"`
	Stream  bool        `json:"stream"`
	Options *ollamaOpts `json:"options,omitempty"`
	Keep    interface{} `json:"keep_alive,omitempty"`
}

// ollamaOpts mirrors ai.OllamaOptions: deterministic pins plus the optional
// GPU/context knobs. Duplicated here on purpose — the bench deliberately does
// not import internal/ai (see the file header) but MUST send the same options
// the production door sends, or the offload curve would measure a different
// request shape than the one users get. v26.24 (WP2).
type ollamaOpts struct {
	Temperature float64 `json:"temperature"`
	Seed        int     `json:"seed"`
	TopP        float64 `json:"top_p"`
	NumGPU      *int    `json:"num_gpu,omitempty"`   // OLLAMA_NUM_GPU
	NumCtx      *int    `json:"num_ctx,omitempty"`   // OLLAMA_NUM_CTX
	NumBatch    *int    `json:"num_batch,omitempty"` // OLLAMA_NUM_BATCH
}

// benchEnvInt reads an integer env var; nil when unset or unparseable.
func benchEnvInt(key string) *int {
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

// benchOpts returns the deterministic pins plus any env-set GPU/context knobs —
// the exact options production's requestOptions() would send.
func benchOpts() *ollamaOpts {
	return &ollamaOpts{
		Temperature: 0.0, Seed: 42, TopP: 1.0,
		NumGPU:   benchEnvInt("OLLAMA_NUM_GPU"),
		NumCtx:   benchEnvInt("OLLAMA_NUM_CTX"),
		NumBatch: benchEnvInt("OLLAMA_NUM_BATCH"),
	}
}

type ollamaResp struct {
	Response           string `json:"response"`
	Error              string `json:"error,omitempty"`
	EvalCount          int    `json:"eval_count"`
	EvalDuration       int64  `json:"eval_duration"`
	PromptEvalCount    int    `json:"prompt_eval_count"`
	PromptEvalDuration int64  `json:"prompt_eval_duration"`
	TotalDuration      int64  `json:"total_duration"`
	LoadDuration       int64  `json:"load_duration"`
}

func ollamaGenerate(host, model, prompt string, timeoutSec int) (*ollamaTimings, error) {
	if host == "" {
		host = defaultOllamaHost
	}
	body, _ := json.Marshal(ollamaReq{
		Model:   model,
		Prompt:  prompt,
		Stream:  false,
		Options: benchOpts(), // v26.24: same knobs as production (env-driven)
	})
	cli := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := cli.Post(host+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ollama http %d: %s", resp.StatusCode, string(raw))
	}
	var r ollamaResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if r.Error != "" {
		return nil, fmt.Errorf("ollama: %s", r.Error)
	}
	return &ollamaTimings{
		Response:           r.Response,
		EvalCount:          r.EvalCount,
		EvalDuration:       r.EvalDuration,
		PromptEvalCount:    r.PromptEvalCount,
		PromptEvalDuration: r.PromptEvalDuration,
		TotalDuration:      r.TotalDuration,
		LoadDuration:       r.LoadDuration,
	}, nil
}

// OllamaDoctor verifies the daemon is reachable and the requested model is
// known to it. Runs in <1s when healthy. Returns failed cases (not errors)
// when the daemon is down so the rest of the bench can still run.
func OllamaDoctor(host, model string) Suite {
	t0 := time.Now()
	if host == "" {
		host = defaultOllamaHost
	}
	s := Suite{Name: "ollama_doctor"}

	// 1. /api/tags reachable?
	cli := &http.Client{Timeout: 5 * time.Second}
	resp, err := cli.Get(host + "/api/tags")
	if err != nil {
		s.Cases = []Case{{Name: "ollama_reachable", Pass: false, Got: err.Error(), Want: host + " responds"}}
		s.Duration = time.Since(t0)
		return s
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// 2. is the requested model present?
	var tags struct {
		Models []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"models"`
	}
	_ = json.Unmarshal(body, &tags)

	hit := false
	var modelSize int64
	for _, m := range tags.Models {
		if m.Name == model || strings.HasPrefix(m.Name, model+":") || strings.HasPrefix(model, m.Name+":") {
			hit = true
			modelSize = m.Size
			break
		}
	}
	s.Cases = append(s.Cases,
		Case{Name: "ollama_reachable", Pass: true, Got: host, Want: "200 OK"},
		Case{Name: "model_present", Pass: hit, Got: fmt.Sprintf("model=%s present=%v", model, hit), Want: model + " present"},
	)
	if hit {
		s.Metrics = append(s.Metrics, Metric{
			Name: "model_size_mb", Value: float64(modelSize) / (1024 * 1024), Unit: "MB",
		})
	}

	// 3. warm the model: a 1-token generation primes the GPU and reports
	// load_duration. If load_duration is very high on a second call,
	// the model is being unloaded between calls — flag that.
	warm1, err := ollamaGenerate(host, model, "// say only: ok\n", 60)
	if err != nil {
		s.Cases = append(s.Cases, Case{Name: "warm_call_succeeds", Pass: false, Got: err.Error()})
		s.Duration = time.Since(t0)
		return s
	}
	warm2, err := ollamaGenerate(host, model, "// say only: ok\n", 30)
	if err != nil {
		s.Cases = append(s.Cases, Case{Name: "second_warm_call_succeeds", Pass: false, Got: err.Error()})
		s.Duration = time.Since(t0)
		return s
	}

	loadDurMs := func(t *ollamaTimings) float64 { return float64(t.LoadDuration) / 1e6 }
	s.Metrics = append(s.Metrics,
		Metric{Name: "first_load_ms", Value: loadDurMs(warm1), Unit: "ms"},
		Metric{Name: "second_load_ms", Value: loadDurMs(warm2), Unit: "ms"},
	)
	// If second_load_ms is roughly the same as first_load_ms, Ollama is
	// reloading weights between calls. Tell the user to set
	// OLLAMA_KEEP_ALIVE.
	stays := warm2.LoadDuration < warm1.LoadDuration/2 || warm2.LoadDuration < 50*1e6
	s.Cases = append(s.Cases, Case{
		Name: "model_stays_resident_between_calls",
		Pass: stays,
		Got:  fmt.Sprintf("first_load=%.0fms second_load=%.0fms", loadDurMs(warm1), loadDurMs(warm2)),
		Want: "second_load << first_load (set OLLAMA_KEEP_ALIVE=-1 if not)",
	})

	s.Duration = time.Since(t0)
	return s
}

// GenerationLatency runs a small fixed prompt against the model `runs` times
// and reports tok/s, eval-only ms/token, and prompt processing ms.
func GenerationLatency(host, model string, runs int) Suite {
	t0 := time.Now()
	s := Suite{Name: "generation_latency"}
	if runs < 1 {
		runs = 3
	}
	// Small fixed CRUD-shaped prompt so the comparison across runs is meaningful.
	const prompt = `Write a single Go function named CreateUser that takes (db *sql.DB, name string, email string) error.
It must INSERT into the users table with columns (name, email).
Return only the function body, no explanation, no markdown fences.`

	var (
		sumTokPerSec float64
		sumEvalMs    float64
		sumTotalMs   float64
		evalCounts   int
		failures     int
	)
	for i := 0; i < runs; i++ {
		t, err := ollamaGenerate(host, model, prompt, 180)
		if err != nil {
			failures++
			continue
		}
		evalSec := float64(t.EvalDuration) / 1e9
		tps := 0.0
		if evalSec > 0 {
			tps = float64(t.EvalCount) / evalSec
		}
		sumTokPerSec += tps
		sumEvalMs += float64(t.EvalDuration) / 1e6
		sumTotalMs += float64(t.TotalDuration) / 1e6
		evalCounts += t.EvalCount
	}
	completed := runs - failures
	if completed > 0 {
		s.Metrics = append(s.Metrics,
			Metric{Name: "tokens_per_sec_mean", Value: sumTokPerSec / float64(completed), Unit: "tok/s", HigherIsBetter: true},
			Metric{Name: "eval_ms_mean", Value: sumEvalMs / float64(completed), Unit: "ms"},
			Metric{Name: "total_ms_mean", Value: sumTotalMs / float64(completed), Unit: "ms"},
			Metric{Name: "eval_count_mean", Value: float64(evalCounts) / float64(completed), Unit: "tokens", HigherIsBetter: true},
		)
	}
	s.Cases = []Case{
		{Name: fmt.Sprintf("at_least_one_of_%d_runs_succeeded", runs), Pass: completed > 0, Got: fmt.Sprintf("%d/%d", completed, runs), Want: ">=1"},
		{Name: "no_failures", Pass: failures == 0, Got: fmt.Sprintf("%d failures", failures), Want: "0"},
	}
	s.Duration = time.Since(t0)
	return s
}

// DeterminismCheck issues N identical generations and diffs them. With
// temperature=0 and a fixed seed, outputs must be byte-identical. They
// usually aren't (KV cache state, batch composition) — this scenario
// surfaces by how much.
func DeterminismCheck(host, model string, n int) Suite {
	t0 := time.Now()
	s := Suite{Name: "determinism_check"}
	if n < 2 {
		n = 5
	}
	const prompt = `Return only this Go function, exactly:
func Double(x int) int { return x * 2 }`

	var outputs []string
	for i := 0; i < n; i++ {
		t, err := ollamaGenerate(host, model, prompt, 60)
		if err != nil {
			s.Cases = append(s.Cases, Case{Name: fmt.Sprintf("run_%d", i), Pass: false, Note: err.Error()})
			continue
		}
		outputs = append(outputs, t.Response)
	}
	if len(outputs) < 2 {
		s.Cases = append(s.Cases, Case{Name: "enough_outputs", Pass: false, Got: fmt.Sprintf("%d", len(outputs)), Want: ">=2"})
		s.Duration = time.Since(t0)
		return s
	}

	hashes := map[string]int{}
	for _, o := range outputs {
		h := sha256.Sum256([]byte(strings.TrimSpace(o)))
		hashes[hex.EncodeToString(h[:])]++
	}
	identical := len(hashes) == 1
	dominantCount := 0
	for _, c := range hashes {
		if c > dominantCount {
			dominantCount = c
		}
	}
	dominantRatio := float64(dominantCount) / float64(len(outputs))

	s.Cases = append(s.Cases,
		Case{Name: "all_outputs_identical", Pass: identical, Got: fmt.Sprintf("%d distinct hashes from %d runs", len(hashes), len(outputs)), Want: "1 distinct hash"},
		Case{Name: "dominant_output_at_least_80%", Pass: dominantRatio >= 0.8, Got: fmt.Sprintf("%.0f%%", dominantRatio*100), Want: ">=80%"},
	)
	s.Metrics = []Metric{
		{Name: "distinct_outputs", Value: float64(len(hashes)), Unit: "hashes"},
		{Name: "dominant_ratio", Value: dominantRatio, Unit: "ratio", HigherIsBetter: true},
		{Name: "runs", Value: float64(len(outputs)), Unit: "runs", HigherIsBetter: true},
	}
	s.Duration = time.Since(t0)
	return s
}
