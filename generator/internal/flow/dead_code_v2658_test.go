// D103: after a return, processNode set b.current to nil and dropped every
// following statement, so no block without predecessors existed and
// AnalyzeDataFlow never reported dead_code for code after a return.
package flow

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func deadCodeCFG(t *testing.T, src, fn string) *CFG {
	t.Helper()
	fe := ast.ForLanguage(ast.LangGo)
	m, err := fe.Parse("pkg/dead.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	cfg := Build(m, symbols.BuildFromModules([]*ast.Module{m}))[symbols.SymbolID("pkg/dead.go", symbols.SymFunc, fn)]
	if cfg == nil {
		t.Fatalf("no CFG for %s", fn)
	}
	return cfg
}

func deadIssues(cfg *CFG) int {
	n := 0
	for _, is := range AnalyzeDataFlow(cfg, nil) {
		if is.Kind == "dead_code" {
			n++
		}
	}
	return n
}

func TestD103_CodeAfterReturnIsDeadCode(t *testing.T) {
	src := `package pkg

func AlwaysReturn() int {
	return 1
	x := 2
	return x
}

func DeadInIf(c bool) int {
	if c {
		return 1
		println("never")
	}
	return 2
}

func DeadAfterIfElse(c bool) int {
	if c {
		return 1
	} else {
		return 2
	}
	println("never")
	return 3
}

func Clean(c bool) int {
	if c {
		return 1
	}
	return 2
}

func BothReturn(c bool) int {
	if c {
		return 1
	} else {
		return 2
	}
}
`
	for fn, want := range map[string]int{"AlwaysReturn": 1, "DeadInIf": 1, "DeadAfterIfElse": 1, "Clean": 0, "BothReturn": 0} {
		cfg := deadCodeCFG(t, src, fn)
		if got := deadIssues(cfg); got != want {
			t.Errorf("%s: %d dead_code issue(s), want %d; blocks:", fn, got, want)
			for _, b := range cfg.Blocks {
				t.Logf("  %d %q stmts=%d preds=%v succs=%v", b.ID, b.Label, len(b.Stmts), b.Preds, b.Succs)
			}
		}
	}

	// The dead statements stay in the CFG, so the flow view shows them.
	cfg := deadCodeCFG(t, src, "AlwaysReturn")
	stmts := 0
	for _, b := range cfg.Blocks {
		stmts += len(b.Stmts)
	}
	if stmts < 3 {
		t.Errorf("AlwaysReturn CFG holds %d statements, want the 3 of the body", stmts)
	}
}

// Calls inside a return, defer, closures, loops and switches are not dead code.
func TestD103_NoDeadCodeInLiveGoConstructs(t *testing.T) {
	src := `package pkg

import "fmt"

func A() (int, error) { return g(h()), fmt.Errorf("x %v", k()) }
func B(c bool) int { if c { return g(1) }; defer done(); return g(2) }
func C() func() int { return func() int { return 1 } }
func D(xs []int) int { for _, x := range xs { if x > 0 { return x } }; return 0 }
func E(v any) int { switch v.(type) { case int: return 1; default: return 2 } }
func G() { go func() { return }(); x() }
`
	for _, fn := range []string{"A", "B", "C", "D", "E", "G"} {
		if n := deadIssues(deadCodeCFG(t, src, fn)); n != 0 {
			t.Errorf("%s: %d dead_code issue(s), want 0", fn, n)
		}
	}
}
