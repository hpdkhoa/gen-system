// generator/internal/bench/report.go — v19.7: render a Run as terminal
// output, write it to JSON, and diff two runs for the regression command.
package bench

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// WriteJSON persists a run to disk.
func WriteJSON(r Run, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// ReadJSON loads a previously written run.
func ReadJSON(path string) (Run, error) {
	var r Run
	data, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(data, &r)
}

// RenderTerminal writes a human-readable summary to w. Uses ANSI when
// stdout is a TTY (best-effort — we don't import golang.org/x/term).
func RenderTerminal(w io.Writer, r Run) {
	dur := r.FinishedAt.Sub(r.StartedAt).Round(time.Millisecond)
	fmt.Fprintln(w, strings.Repeat("─", 70))
	fmt.Fprintf(w, "genbench — %s on %s/%s (Go %s)\n", r.Schema, r.HostInfo.OS, r.HostInfo.Arch, r.HostInfo.GoVersion)
	fmt.Fprintf(w, "model: %s @ %s\n", r.ModelInfo.Name, r.ModelInfo.Endpoint)
	fmt.Fprintf(w, "root:  %s\n", r.ProjectRoot)
	fmt.Fprintf(w, "took:  %s\n", dur)
	fmt.Fprintln(w, strings.Repeat("─", 70))

	totalPass, totalFail := 0, 0
	for _, s := range r.Suites {
		p, f := s.PassFailSummary()
		totalPass += p
		totalFail += f
		mark := "✓"
		if f > 0 {
			mark = "✗"
		}
		fmt.Fprintf(w, "\n%s  %s   (%d/%d cases pass)   %s\n", mark, s.Name, p, p+f, s.Duration.Round(time.Microsecond))
		// Cases — failures first.
		sorted := make([]Case, len(s.Cases))
		copy(sorted, s.Cases)
		sort.SliceStable(sorted, func(i, j int) bool { return !sorted[i].Pass && sorted[j].Pass })
		for _, c := range sorted {
			sym := "  ✓ "
			if !c.Pass {
				sym = "  ✗ "
			}
			gotWant := ""
			if c.Got != "" || c.Want != "" {
				gotWant = fmt.Sprintf("   got=%s want=%s", c.Got, c.Want)
			}
			note := ""
			if c.Note != "" {
				note = "   // " + c.Note
			}
			fmt.Fprintf(w, "%s%-40s%s%s\n", sym, c.Name, gotWant, note)
		}
		if len(s.Metrics) > 0 {
			fmt.Fprintln(w, "    metrics:")
			for _, m := range s.Metrics {
				fmt.Fprintf(w, "      %-32s %12.3f %s\n", m.Name, m.Value, m.Unit)
			}
		}
		if s.Notes != "" {
			fmt.Fprintf(w, "    notes: %s\n", s.Notes)
		}
	}
	fmt.Fprintln(w, strings.Repeat("─", 70))
	fmt.Fprintf(w, "TOTAL: %d pass, %d fail\n", totalPass, totalFail)
}

// Regression compares two runs and returns a non-zero exit code if any
// metric in `new` is worse than `old` by more than tolerance (e.g. 0.05).
// "Worse" is relative to Metric.HigherIsBetter.
//
// Returns (delta lines for printing, ok bool).
func Regression(oldRun, newRun Run, tolerance float64) ([]string, bool) {
	if tolerance < 0 {
		tolerance = 0
	}
	// Index old metrics by suite.metric so lookup is O(1).
	type key struct{ suite, metric string }
	oldMetrics := map[key]Metric{}
	for _, s := range oldRun.Suites {
		for _, m := range s.Metrics {
			oldMetrics[key{s.Name, m.Name}] = m
		}
	}

	var lines []string
	ok := true
	for _, s := range newRun.Suites {
		for _, m := range s.Metrics {
			om, found := oldMetrics[key{s.Name, m.Name}]
			if !found {
				lines = append(lines, fmt.Sprintf("  +  %s.%s = %.3f %s (new)", s.Name, m.Name, m.Value, m.Unit))
				continue
			}
			delta := m.Value - om.Value
			rel := 0.0
			if math.Abs(om.Value) > 1e-9 {
				rel = delta / math.Abs(om.Value)
			}
			worse := false
			if m.HigherIsBetter {
				worse = rel < -tolerance
			} else {
				worse = rel > tolerance
			}
			sign := "Δ"
			if worse {
				sign = "✗"
				ok = false
			}
			lines = append(lines, fmt.Sprintf("  %s  %s.%s: %.3f → %.3f (%+.1f%%)  %s",
				sign, s.Name, m.Name, om.Value, m.Value, rel*100, m.Unit))
		}
	}
	sort.Strings(lines)
	return lines, ok
}
