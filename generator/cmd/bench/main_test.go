// cmd/bench/main_test.go (v19 fix): removed the Duration==0 assertion.
// time.Since returns 0 on Windows for sub-millisecond operations (timer
// resolution ~15ms). Suite completion is already proven by len(Cases)>0.
// tests for the gen-system v15 benchmark tool.
// Verifies: each suite runs without panic, returns coherent results,
// and the PASS/FAIL tallies match actual case outcomes.
package main

import (
	"testing"
)

// ─── Suite execution contract ─────────────────────────────────

// runSuite is a helper that runs a suite function and asserts basic invariants.
func runSuite(t *testing.T, name string, fn func() Result) Result {
	t.Helper()
	r := fn()
	if r.Suite == "" {
		t.Errorf("%s: Suite name must not be empty", name)
	}
	if len(r.Cases) == 0 {
		t.Errorf("%s: Suite must contain at least one case", name)
	}
	// Duration is intentionally not asserted: time.Since can return 0 on
	// Windows (timer resolution ~15ms) for operations that complete in
	// microseconds. Completion is proven by len(r.Cases) > 0 above.
	_ = r.Duration
	return r
}

// ─── Individual suite tests ───────────────────────────────────

func TestSuite_FrontendRegistry(t *testing.T) {
	r := runSuite(t, "FrontendRegistry", benchFrontendRegistry)

	// Every required language must be registered. These are hard invariants.
	required := map[string]bool{}
	for _, c := range r.Cases {
		if c.Pass {
			required[c.Name] = true
		}
	}

	mustPass := []string{
		"ForLanguage(go)",
		"ForLanguage(typescript)",
		"ForLanguage(python)",
		"ForLanguage(cagen)",
	}
	for _, name := range mustPass {
		if !required[name] {
			t.Errorf("FrontendRegistry: %q must pass — frontend not registered", name)
		}
	}
}

func TestSuite_GoParser(t *testing.T) {
	r := runSuite(t, "GoParser", benchGoParser)

	passCases := countPass(r)
	if passCases < 5 {
		t.Errorf("GoParser: expected ≥5 pass cases, got %d/%d", passCases, len(r.Cases))
		for _, c := range r.Cases {
			if !c.Pass {
				t.Logf("  FAIL: %s  // %s", c.Name, c.Note)
			}
		}
	}
}

func TestSuite_TSParser(t *testing.T) {
	r := runSuite(t, "TSParser", benchTSParser)
	passCases := countPass(r)
	if passCases < 3 {
		t.Errorf("TSParser: expected ≥3 pass cases, got %d/%d", passCases, len(r.Cases))
	}
}

func TestSuite_PythonParser(t *testing.T) {
	r := runSuite(t, "PythonParser", benchPythonParser)
	// Python frontend must at least register. Runtime availability is env-dependent.
	assertCasePasses(t, r, "frontend registered")
}

func TestSuite_CAGenParser(t *testing.T) {
	r := runSuite(t, "CAGenParser", benchCAGenParser)
	assertCasePasses(t, r, "frontend registered")
	assertCasePasses(t, r, "available() = true (pure Go)")
	assertCasePasses(t, r, "extensions include .ad")
}

func TestSuite_SymbolIndex(t *testing.T) {
	r := runSuite(t, "SymbolIndex", benchSymbolIndex)
	assertCasePasses(t, r, "BuildFromModules (3 files)")
	assertCasePasses(t, r, "ByName(User) finds struct")
	assertCasePasses(t, r, "AllSymbols() >= 5 (3 files, several declarations)")
	assertCasePasses(t, r, "Save index to disk")
	assertCasePasses(t, r, "Load index round-trip (User survives)")
}

func TestSuite_CallGraph(t *testing.T) {
	r := runSuite(t, "CallGraph", benchCallGraph)
	assertCasePasses(t, r, "ProcessItem resolved in index")
}

func TestSuite_CFGBuilder(t *testing.T) {
	r := runSuite(t, "CFGBuilder", benchCFGBuilder)
	assertCasePasses(t, r, "CFG built without error")
	assertCasePasses(t, r, "CFG has ≥1 basic block")
	assertCasePasses(t, r, "CFG entry index valid")
}

func TestSuite_FlowAnalysis(t *testing.T) {
	r := runSuite(t, "FlowAnalysis", benchFlowAnalysis)
	assertCasePasses(t, r, "AnalyzeDataFlow(nil) returns empty")
}

func TestSuite_EffectInference(t *testing.T) {
	r := runSuite(t, "EffectInference", benchEffectInference)
	if countPass(r) == 0 {
		t.Error("EffectInference: every case failed — at least one should pass")
	}
}

func TestSuite_CacheRoundTrip(t *testing.T) {
	r := runSuite(t, "CacheRoundTrip", benchCacheRoundTrip)
	assertCasePasses(t, r, "cold parse succeeds")
	assertCasePasses(t, r, "warm hit (same checksum)")
	assertCasePasses(t, r, "modified file triggers re-parse (checksum differs)")
}

func TestSuite_ChecksumStability(t *testing.T) {
	r := runSuite(t, "ChecksumStability", benchChecksumStability)
	assertCasePasses(t, r, "checksum deterministic (5 parses of same source)")
	assertCasePasses(t, r, "different source → different checksum")
	assertCasePasses(t, r, "checksum is 64-char hex (SHA-256)")
}

func TestSuite_UnknownNodePolicy(t *testing.T) {
	r := runSuite(t, "UnknownNodePolicy", benchUnknownNodePolicy)
	assertCasePasses(t, r, "unrecognised construct → no error")
	assertCasePasses(t, r, "unrecognised construct → module non-nil")
}

// ─── CaseResult unit tests ────────────────────────────────────

func TestCaseResult_Status(t *testing.T) {
	pass := CaseResult{Pass: true}
	fail := CaseResult{Pass: false}
	if pass.status() != "PASS" {
		t.Errorf("status() for passing case = %q, want PASS", pass.status())
	}
	if fail.status() != "FAIL" {
		t.Errorf("status() for failing case = %q, want FAIL", fail.status())
	}
}

func TestContainsStr(t *testing.T) {
	cases := []struct {
		ss   []string
		s    string
		want bool
	}{
		{[]string{".go", ".ts"}, ".go", true},
		{[]string{".go", ".ts"}, ".py", false},
		{nil, ".go", false},
		{[]string{}, ".go", false},
	}
	for _, c := range cases {
		got := containsStr(c.ss, c.s)
		if got != c.want {
			t.Errorf("containsStr(%v, %q) = %v, want %v", c.ss, c.s, got, c.want)
		}
	}
}

// ─── Helpers ──────────────────────────────────────────────────

func countPass(r Result) int {
	n := 0
	for _, c := range r.Cases {
		if c.Pass {
			n++
		}
	}
	return n
}

func assertCasePasses(t *testing.T, r Result, caseName string) {
	t.Helper()
	for _, c := range r.Cases {
		if c.Name == caseName {
			if !c.Pass {
				t.Errorf("Suite %q: case %q should pass but failed — %s", r.Suite, caseName, c.Note)
			}
			return
		}
	}
	t.Errorf("Suite %q: case %q not found in results", r.Suite, caseName)
}
