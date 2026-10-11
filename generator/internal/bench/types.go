// generator/internal/bench/types.go, v19.7 (genbench): shared bench types.
// New package. Defines the JSON shape every scenario emits so the
// `regression` subcommand can diff runs and the report writer can render them
// without knowing which scenario produced which metric.
package bench

import "time"

// Run is the top-level artefact written by `genbench`. One run per invocation.
type Run struct {
	Schema      string    `json:"schema"` // always "genbench/v1"
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	HostInfo    HostInfo  `json:"host"`
	ModelInfo   ModelInfo `json:"model"`
	ProjectRoot string    `json:"project_root"`
	Suites      []Suite   `json:"suites"`
}

// HostInfo records environment so two runs are only compared when comparable.
type HostInfo struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	NumCPU    int    `json:"num_cpu"`
	GoVersion string `json:"go_version"`
	GitCommit string `json:"git_commit,omitempty"`
}

// ModelInfo records which LLM was queried (or "n/a" for non-AI suites).
type ModelInfo struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
}

// Suite groups Cases under a name (one suite per subcommand).
type Suite struct {
	Name     string        `json:"name"`
	Cases    []Case        `json:"cases"`
	Metrics  []Metric      `json:"metrics"`
	Duration time.Duration `json:"duration_ns"`
	Notes    string        `json:"notes,omitempty"`
}

// Case is a pass/fail check. Use for reliability: assertions about
// what the engine produced.
type Case struct {
	Name string `json:"name"`
	Pass bool   `json:"pass"`
	Got  string `json:"got,omitempty"`
	Want string `json:"want,omitempty"`
	Note string `json:"note,omitempty"`
}

// Metric is a single numeric measurement. Use for performance.
// Regression diff treats higher==worse unless HigherIsBetter is true.
type Metric struct {
	Name           string  `json:"name"`
	Value          float64 `json:"value"`
	Unit           string  `json:"unit"`
	HigherIsBetter bool    `json:"higher_is_better,omitempty"`
}

// PassFailSummary counts passes and fails across all cases in a suite.
func (s Suite) PassFailSummary() (pass, fail int) {
	for _, c := range s.Cases {
		if c.Pass {
			pass++
		} else {
			fail++
		}
	}
	return
}
