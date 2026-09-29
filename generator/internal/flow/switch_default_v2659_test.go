// N03: a Go switch or type switch with no default clause got no edge from the
// deciding block to the merge block. When every case returned, the code after
// the switch sat in a block with no predecessors and AnalyzeDataFlow reported
// it as dead_code, though it runs whenever no case matches. A select with no
// default blocks until a case runs, so it keeps the old graph.
package flow

import "testing"

const switchNoDefaultSrc = `package pkg

func after() {}

func Switch(x int) int {
	switch x {
	case 1:
		return 10
	case 2:
		return 20
	}
	after()
	return 0
}

func TypeSwitch(v any) int {
	switch v.(type) {
	case int:
		return 1
	case string:
		return 2
	}
	after()
	return 0
}

func Select(a, b chan int) int {
	select {
	case v := <-a:
		return v
	case v := <-b:
		return v
	}
	after()
	return 0
}
`

// blockWithCall returns the block that holds the call to callee, or nil.
func blockWithCall(cfg *CFG, callee string) *BasicBlock {
	for _, blk := range cfg.Blocks {
		for _, s := range blk.Stmts {
			if s != nil && s.Callee == callee {
				return blk
			}
		}
	}
	return nil
}

func TestN03_SwitchWithoutDefaultFallsThrough(t *testing.T) {
	for _, fn := range []string{"Switch", "TypeSwitch"} {
		cfg := deadCodeCFG(t, switchNoDefaultSrc, fn)
		if got := deadIssues(cfg); got != 0 {
			t.Errorf("%s: %d dead_code findings, want 0 (no case matches, so the code after the switch runs)", fn, got)
		}
		blk := blockWithCall(cfg, "after")
		if blk == nil {
			t.Fatalf("%s: no block holds the call to after()", fn)
		}
		if len(blk.Preds) == 0 {
			t.Errorf("%s: block %q holding after() has no predecessor", fn, blk.Label)
		}
	}
}

func TestN03_SelectWithoutDefaultKeepsGraph(t *testing.T) {
	cfg := deadCodeCFG(t, switchNoDefaultSrc, "Select")
	blk := blockWithCall(cfg, "after")
	if blk == nil {
		t.Fatal("Select: no block holds the call to after()")
	}
	if len(blk.Preds) != 0 {
		t.Errorf("Select: block %q holding after() has predecessors %v, want none (select blocks until a case runs)", blk.Label, blk.Preds)
	}
	if got := deadIssues(cfg); got != 1 {
		t.Errorf("Select: %d dead_code findings, want 1", got)
	}
}
