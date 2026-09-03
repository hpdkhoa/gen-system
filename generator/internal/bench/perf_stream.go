// generator/internal/bench/perf_stream.go — v26.24 (WP2): streaming latency.
// Measures time-to-first-token and total latency for the SAME prompt with
// stream on and stream off, n runs each, so writeup 02's streaming table comes
// from a run instead of a claim. Self-contained HTTP (bench convention: no
// internal/ai import), but the request shape — options, pins, knobs — matches
// production's exactly via benchOpts().
package bench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// streamProbe issues one /api/generate call. stream=true reads NDJSON and
// records the wall time to the first chunk that carries response text;
// stream=false reads the buffered body and TTFT equals total (the whole point
// of the comparison).
func streamProbe(host, model, prompt string, stream bool, timeoutSec int) (ttftMs, totalMs float64, err error) {
	if host == "" {
		host = defaultOllamaHost
	}
	body, _ := json.Marshal(ollamaReq{
		Model:   model,
		Prompt:  prompt,
		Stream:  stream,
		Options: benchOpts(),
	})
	cli := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	t0 := time.Now()
	resp, err := cli.Post(host+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, 0, fmt.Errorf("ollama http %d", resp.StatusCode)
	}

	if !stream {
		var r ollamaResp
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			return 0, 0, err
		}
		if r.Error != "" {
			return 0, 0, fmt.Errorf("ollama: %s", r.Error)
		}
		total := float64(time.Since(t0)) / 1e6
		return total, total, nil
	}

	var first time.Duration
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var chunk struct {
			Response string `json:"response"`
			Error    string `json:"error"`
			Done     bool   `json:"done"`
		}
		if err := json.Unmarshal(line, &chunk); err != nil {
			return 0, 0, err
		}
		if chunk.Error != "" {
			return 0, 0, fmt.Errorf("ollama: %s", chunk.Error)
		}
		if first == 0 && chunk.Response != "" {
			first = time.Since(t0)
		}
	}
	if err := sc.Err(); err != nil {
		return 0, 0, err
	}
	if first == 0 {
		first = time.Since(t0) // model produced no text; count the full wait
	}
	return float64(first) / 1e6, float64(time.Since(t0)) / 1e6, nil
}

// StreamingLatency compares stream on vs off: TTFT and total latency over the
// same fixed prompt, runs times each. Reports mean/min/max per config.
func StreamingLatency(host, model string, runs int) Suite {
	t0 := time.Now()
	s := Suite{Name: "streaming_latency"}
	if runs < 1 {
		runs = 3
	}
	const prompt = `Write a single Go function named CreateUser that takes (db *sql.DB, name string, email string) error.
It must INSERT into the users table with columns (name, email).
Return only the function body, no explanation, no markdown fences.`

	type agg struct{ ttft, total []float64 }
	results := map[string]*agg{"stream_off": {}, "stream_on": {}}
	failures := 0

	for i := 0; i < runs; i++ {
		for _, cfg := range []struct {
			key    string
			stream bool
		}{{"stream_off", false}, {"stream_on", true}} {
			ttft, total, err := streamProbe(host, model, prompt, cfg.stream, 300)
			if err != nil {
				failures++
				continue
			}
			a := results[cfg.key]
			a.ttft = append(a.ttft, ttft)
			a.total = append(a.total, total)
		}
	}

	stats := func(xs []float64) (mean, min, max float64) {
		if len(xs) == 0 {
			return 0, 0, 0
		}
		min, max = xs[0], xs[0]
		for _, x := range xs {
			mean += x
			if x < min {
				min = x
			}
			if x > max {
				max = x
			}
		}
		return mean / float64(len(xs)), min, max
	}

	for _, key := range []string{"stream_off", "stream_on"} {
		a := results[key]
		tm, tmin, tmax := stats(a.ttft)
		om, _, _ := stats(a.total)
		s.Metrics = append(s.Metrics,
			Metric{Name: key + "_ttft_ms_mean", Value: tm, Unit: "ms"},
			Metric{Name: key + "_ttft_ms_min", Value: tmin, Unit: "ms"},
			Metric{Name: key + "_ttft_ms_max", Value: tmax, Unit: "ms"},
			Metric{Name: key + "_total_ms_mean", Value: om, Unit: "ms"},
			Metric{Name: key + "_runs", Value: float64(len(a.ttft)), Unit: "runs", HigherIsBetter: true},
		)
	}
	s.Cases = []Case{
		{Name: "all_probes_succeeded", Pass: failures == 0,
			Got: fmt.Sprintf("%d failures over %d probes", failures, runs*2), Want: "0"},
	}
	s.Duration = time.Since(t0)
	return s
}
