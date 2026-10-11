// go_cfg_v236_test.go (v23.6): convertBody preserves control flow
// (if/for/switch -> NodeBranch/NodeLoop, return -> NodeReturn).
package ast

import "testing"

// v23.6: convertBody must preserve control structure: if/for/switch become
// NodeBranch/NodeLoop with nested Children, and return becomes NodeReturn.
func TestGoBody_PreservesControlFlow(t *testing.T) {
	src := `package p
func H(id int) error {
	x, err := f(id)
	if err != nil {
		return err
	}
	for i := 0; i < id; i++ {
		g(i)
	}
	switch {
	case x > 1:
		s(x)
	}
	return nil
}`
	mod, err := (&GoFrontend{}).Parse("h.go", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var fn *Node
	for _, n := range mod.Nodes {
		if n.Kind == NodeFunction && n.Name == "H" {
			fn = n
		}
	}
	if fn == nil {
		t.Fatal("function H not found")
	}
	kinds := map[NodeKind]int{}
	var walk func(ns []*Node)
	walk = func(ns []*Node) {
		for _, n := range ns {
			kinds[n.Kind]++
			walk(n.Children)
		}
	}
	walk(fn.Children)
	if kinds[NodeBranch] < 2 {
		t.Errorf("want >=2 branches (if + switch), got %d", kinds[NodeBranch])
	}
	if kinds[NodeLoop] < 1 {
		t.Errorf("want >=1 loop (for), got %d", kinds[NodeLoop])
	}
	if kinds[NodeReturn] < 2 {
		t.Errorf("want >=2 returns, got %d", kinds[NodeReturn])
	}
}
