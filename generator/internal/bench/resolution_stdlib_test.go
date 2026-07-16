// generator/internal/bench/resolution_stdlib_test.go — v23.32 (stdlib
// provenance): behavioural test for ResolutionRecallWithStdlib. The invariant
// under test is the whole point of the provenance tier: ingesting a stdlib must
// NOT change the project's internal-recall number. It also asserts that when a
// stdlib IS ingested, stdlib symbols actually appear and project calls resolve
// into them. Skips cleanly when GOROOT/src is unavailable so CI without Go
// source still passes.
package bench

import (
	"path/filepath"
	"runtime"
	"testing"

	agentctx "gen-system/generator/internal/context"
)

// findProjectRoot walks up from this test file to the module root (the dir
// holding go.mod), then returns the generator subtree as the corpus.
func findProjectRoot(t *testing.T) string {
	t.Helper()
	// This file lives at generator/internal/bench; the generator dir is two up.
	abs, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

func TestResolutionRecall_StdlibInvariant(t *testing.T) {
	root := findProjectRoot(t)

	goSrc := filepath.Join(runtime.GOROOT(), "src")
	if resolved, err := filepath.EvalSymlinks(goSrc); err == nil {
		goSrc = resolved
	}
	if fi, err := filepath.Glob(filepath.Join(goSrc, "fmt", "*.go")); err != nil || len(fi) == 0 {
		t.Skipf("Go stdlib source not available at %s; skipping stdlib invariant test", goSrc)
	}

	// Baseline: no stdlib roots.
	base := ResolutionRecallWithStdlib(root, nil, nil, 0.90)
	baseInternal := metricValue(base, "resolution_recall_internal")

	// With stdlib, scoped to the project's imports.
	roots := []agentctx.StdlibRoot{{Path: filepath.Join(runtime.GOROOT(), "src"), AutoScope: true}}
	withStd := ResolutionRecallWithStdlib(root, nil, roots, 0.90)
	stdInternal := metricValue(withStd, "resolution_recall_internal")
	stdSyms := metricValue(withStd, "symbols_stdlib")
	intoStd := metricValue(withStd, "call_edges_into_stdlib")

	// THE invariant: project internal recall is unchanged by stdlib ingest.
	if baseInternal != stdInternal {
		t.Errorf("internal recall changed by stdlib ingest: base=%.4f with-stdlib=%.4f (must be equal)",
			baseInternal, stdInternal)
	}
	// Stdlib must actually have been ingested.
	if stdSyms <= 0 {
		t.Errorf("symbols_stdlib = %.0f, want > 0 (stdlib was supplied)", stdSyms)
	}
	// And at least some project calls must resolve into it (this is a Go
	// project that imports fmt/os/etc., so this is guaranteed in practice).
	if intoStd <= 0 {
		t.Errorf("call_edges_into_stdlib = %.0f, want > 0", intoStd)
	}
}

func metricValue(s Suite, name string) float64 {
	for _, m := range s.Metrics {
		if m.Name == name {
			return m.Value
		}
	}
	return -1
}
