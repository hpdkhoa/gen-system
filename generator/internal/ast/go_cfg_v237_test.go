// go_cfg_v237_test.go (v23.7): if-else populates Node.Else (distinct else
// block); nested else-if nests as a NodeBranch inside Else.
package ast

import "testing"

// v23.7: if-else must populate Node.Else (distinct else body), and nested
// else-if must nest as a NodeBranch inside Else.
func TestGoBody_ElsePath(t *testing.T) {
	src := `package p
func C(n int) string {
	if n < 0 {
		return neg()
	} else if n == 0 {
		return zero()
	} else {
		return pos()
	}
}`
	mod, err := (&GoFrontend{}).Parse("c.go", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var fn *Node
	for _, n := range mod.Nodes {
		if n.Kind == NodeFunction && n.Name == "C" {
			fn = n
		}
	}
	if fn == nil {
		t.Fatal("C not found")
	}
	// outer if is a branch with non-empty Else
	var outer *Node
	for _, ch := range fn.Children {
		if ch.Kind == NodeBranch {
			outer = ch
		}
	}
	if outer == nil {
		t.Fatal("no outer branch")
	}
	if len(outer.Else) == 0 {
		t.Fatal("outer if has empty Else (else-path lost)")
	}
	// the else should itself contain a nested branch (the "else if")
	var nested *Node
	for _, e := range outer.Else {
		if e.Kind == NodeBranch {
			nested = e
		}
	}
	if nested == nil {
		t.Fatal("else-if did not nest as a branch in Else")
	}
	if len(nested.Else) == 0 {
		t.Fatal("final else lost")
	}
}
