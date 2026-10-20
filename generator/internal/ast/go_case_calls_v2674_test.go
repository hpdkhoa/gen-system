// v26.74: the Go frontend dropped the calls in a switch case value and in a select comm clause, so
// `case overlaps(a, b):` and `case v := <-next():` left overlaps and next out of the call graph.
package ast

import "testing"

func TestGoFrontend_CaseValueCalls_v2674(t *testing.T) {
	src := `package p

func Run(a, b int, ch chan int) {
	switch {
	case overlaps(a, b):
		work(a)
	case a > limit(b), isZero(b):
	}
	select {
	case v := <-next(ch):
		work(v)
	default:
	}
}
`
	got := goCallees(goFuncNamed(t, src, "Run"))
	for _, want := range []string{"overlaps", "work", "limit", "isZero", "next"} {
		if !got[want] {
			t.Errorf("Run has no NodeCall for %s; callees = %v", want, got)
		}
	}
}
