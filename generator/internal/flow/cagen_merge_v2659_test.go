// N04: in a CA Gen (or COBOL) procedure, an IF with an ELSE whose arms both end
// in EXIT or ESCAPE left the if_merge block with no predecessors and nothing
// in it. buildProcedureCFG never pruned, so AnalyzeDataFlow reported that
// empty block as dead_code. A statement after such an IF is real dead code
// and must still be reported.
package flow

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

// bothArmsExit is IF VALID { CALL CHECK; EXIT } ELSE { ESCAPE }, then rest.
func bothArmsExit(rest ...*ast.Node) *ast.Node {
	ifNode := &ast.Node{
		Kind: ast.NodeBranch, Name: "IF VALID",
		Children: []*ast.Node{
			{Kind: ast.NodeCall, Callee: "CHECK"},
			{Kind: ast.NodeReturn, Name: "EXIT PROCEDURE"},
		},
		Else: []*ast.Node{{Kind: ast.NodeEscape, Name: "ESCAPE"}},
	}
	return &ast.Node{
		Kind:     ast.NodeProcedure,
		Name:     "P",
		Children: append([]*ast.Node{{Kind: ast.NodeCall, Callee: "START"}, ifNode}, rest...),
	}
}

func TestN04_IfElseBothExitNoEmptyDeadBlock(t *testing.T) {
	cfg := buildProcedureCFG("p", bothArmsExit())
	if got := deadIssues(cfg); got != 0 {
		t.Errorf("dead_code findings = %d, want 0 (the empty join of two exiting arms is not code)", got)
	}
	var sawFalse bool
	for i, blk := range cfg.Blocks {
		if blk.ID != i {
			t.Errorf("block at index %d has ID %d", i, blk.ID)
		}
		if blk.Label == "if_false" {
			sawFalse = true
		}
		if blk.Label == "if_merge" {
			t.Errorf("empty if_merge block %d survived with preds %v", blk.ID, blk.Preds)
		}
	}
	if !sawFalse {
		t.Error("the ELSE body lost its if_false block (D014)")
	}
	if len(cfg.Exits) != 2 {
		t.Errorf("exits = %v, want the two exiting arms", cfg.Exits)
	}
	for _, e := range cfg.Exits {
		if cfg.Block(e) == nil {
			t.Errorf("exit %d does not name a block", e)
		}
	}
}

func TestN04_StatementAfterBothExitIsStillDeadCode(t *testing.T) {
	cfg := buildProcedureCFG("p", bothArmsExit(&ast.Node{Kind: ast.NodeCall, Callee: "NEVER"}))
	if got := deadIssues(cfg); got != 1 {
		t.Errorf("dead_code findings = %d, want 1 (the call after the IF never runs)", got)
	}
	if blockWithCall(cfg, "NEVER") == nil {
		t.Error("the dead call left the CFG")
	}
}
