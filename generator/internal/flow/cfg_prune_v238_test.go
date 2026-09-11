// generator/internal/flow/cfg_prune_v238_test.go — v23.8: regression test for
// pruneUnreachable. When both the then- and else-branch of an if/else return,
// the merge block becomes unreachable; it must be pruned and IDs must stay
// dense + index-aligned (CFG.Block(id) indexes Blocks by position).
package flow

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// branchElseReturnNode builds an if/else where the then-body and else-body each
// end in a return, so control never reaches the join.
func branchElseReturnNode() *ast.Node {
	return &ast.Node{
		Kind:     ast.NodeBranch,
		Children: []*ast.Node{callNode("logThen"), returnNode()},
		Else:     []*ast.Node{callNode("logElse"), returnNode()},
	}
}

func TestPrune_OrphanMergeRemoved(t *testing.T) {
	m := goFuncModule("AllBranchesReturn", branchElseReturnNode())
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	funcID := symbols.SymbolID("pkg/foo.go", symbols.SymFunc, "AllBranchesReturn")
	cfg := cfgs[funcID]
	if cfg == nil {
		t.Fatal("no CFG for AllBranchesReturn")
	}

	// No non-entry block may be an orphan (0 predecessors) — that was the bug.
	for _, b := range cfg.Blocks {
		if b.ID == cfg.Entry {
			continue
		}
		if len(b.Preds) == 0 {
			t.Errorf("block %d (%q) is an orphan (0 preds) — should have been pruned", b.ID, b.Label)
		}
	}

	// IDs must be dense and index-aligned so CFG.Block(id) is valid.
	for i, b := range cfg.Blocks {
		if b.ID != i {
			t.Errorf("block at index %d has non-dense ID %d", i, b.ID)
		}
		if cfg.Block(b.ID) != b {
			t.Errorf("CFG.Block(%d) did not return the block at that index", b.ID)
		}
	}

	// Every edge must reference an existing block (no dangling refs to pruned IDs).
	n := len(cfg.Blocks)
	for _, b := range cfg.Blocks {
		for _, s := range b.Succs {
			if s < 0 || s >= n {
				t.Errorf("block %d has dangling succ %d (n=%d)", b.ID, s, n)
			}
		}
		for _, p := range b.Preds {
			if p < 0 || p >= n {
				t.Errorf("block %d has dangling pred %d (n=%d)", b.ID, p, n)
			}
		}
	}

	// Both returns should be recorded as exits, and exits must be valid IDs.
	if len(cfg.Exits) < 2 {
		t.Errorf("want >= 2 exit blocks (two returns), got %d", len(cfg.Exits))
	}
	for _, e := range cfg.Exits {
		if e < 0 || e >= n {
			t.Errorf("exit %d out of range (n=%d)", e, n)
		}
	}
}

// TestPrune_KeepsReachableMerge ensures a normal if/else (then falls through,
// no return) is NOT pruned — the merge block stays because it is reachable.
func TestPrune_KeepsReachableMerge(t *testing.T) {
	m := goFuncModule("FallsThrough",
		&ast.Node{
			Kind:     ast.NodeBranch,
			Children: []*ast.Node{callNode("a")},
			Else:     []*ast.Node{callNode("b")},
		},
		callNode("after"),
	)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	funcID := symbols.SymbolID("pkg/foo.go", symbols.SymFunc, "FallsThrough")
	cfg := cfgs[funcID]
	if cfg == nil {
		t.Fatal("no CFG for FallsThrough")
	}
	// entry, then, else, merge all reachable -> >= 4 blocks, no orphans.
	if len(cfg.Blocks) < 4 {
		t.Errorf("falls-through if/else should keep >= 4 blocks, got %d", len(cfg.Blocks))
	}
	for _, b := range cfg.Blocks {
		if b.ID != cfg.Entry && len(b.Preds) == 0 {
			t.Errorf("reachable block %d (%q) wrongly has 0 preds", b.ID, b.Label)
		}
	}
}
