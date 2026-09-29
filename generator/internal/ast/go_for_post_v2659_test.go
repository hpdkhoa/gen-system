// N09: the Go frontend dropped the calls in the post statement of a for loop.
// The loop node got the calls of Init, Cond and the body, but never of Post,
// so the call graph missed a callee such as next in `i = next(i)`.
package ast

import "testing"

func TestGoFrontend_ForPostCalls_v2659(t *testing.T) {
	src := `package p

func Run(n int) {
	for i := 0; i < n; i = next(i) {
		work(i)
	}
}
`
	fn := goFuncNamed(t, src, "Run")
	var loop *Node
	for _, ch := range fn.Children {
		if ch.Kind == NodeLoop {
			loop = ch
		}
	}
	if loop == nil {
		t.Fatal("no loop node under Run")
	}
	got := goCallees(loop)
	for _, want := range []string{"next", "work"} {
		if !got[want] {
			t.Errorf("loop has no NodeCall for %s; callees = %v", want, got)
		}
	}
	// The post statement runs after the body, so its call comes last.
	last := loop.Children[len(loop.Children)-1]
	if last.Kind != NodeCall || last.Callee != "next" {
		t.Errorf("last loop child = %v %q, want the call to next", last.Kind, last.Callee)
	}
}
