// D060: the Go frontend built one node from the outer call of an expression
// statement and of a defer, so calls in the arguments and in a deferred
// function literal were lost: outer(nested()), defer cleanup(inner()) and
// defer func() { deep() }() gave no NodeCall for nested, inner and deep.
package ast

import "testing"

func TestGoFrontend_NestedAndDeferredCalls_v2658(t *testing.T) {
	src := `package p

func Run() {
	outer(nested())
	defer cleanup(inner())
	defer func() { deep() }()
}
`
	fn := goFuncNamed(t, src, "Run")
	got := goCallees(fn)
	for _, want := range []string{"outer", "nested", "cleanup", "inner", "deep"} {
		if !got[want] {
			t.Errorf("no NodeCall for %s; callees = %v", want, got)
		}
	}
	// The statement shapes stay: a call node for outer, then two defer nodes.
	if len(fn.Children) < 3 || fn.Children[0].Kind != NodeCall || fn.Children[0].Callee != "outer" {
		t.Fatalf("first node is not the call to outer: %+v", fn.Children)
	}
	var defers []string
	for _, ch := range fn.Children {
		if ch.Kind == NodeDefer {
			defers = append(defers, ch.Callee)
		}
	}
	if len(defers) != 2 || defers[0] != "cleanup" || defers[1] != FuncLiteralCallee {
		t.Errorf("defer nodes = %q, want [cleanup %q]", defers, FuncLiteralCallee)
	}
}
