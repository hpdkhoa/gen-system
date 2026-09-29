// D059: the Go frontend dropped the calls in the Init of if, for, switch and
// type switch, and in the guard of a type switch. The type switch node also had
// an empty RawSource.
package ast

import "testing"

// goCallees lists the Callee of every NodeCall under n, in Children and Else.
func goCallees(n *Node) map[string]bool {
	out := map[string]bool{}
	var walk func(*Node)
	walk = func(x *Node) {
		if x == nil {
			return
		}
		if x.Kind == NodeCall && x.Callee != "" {
			out[x.Callee] = true
		}
		for _, ch := range x.Children {
			walk(ch)
		}
		for _, ch := range x.Else {
			walk(ch)
		}
	}
	walk(n)
	return out
}

func goFuncNamed(t *testing.T, src, name string) *Node {
	t.Helper()
	m, err := goFE().Parse("p.go", []byte(src))
	assertNoError(t, err)
	for _, fn := range m.TopLevelByKind(NodeFunction) {
		if fn.Name == name {
			return fn
		}
	}
	t.Fatalf("function %s not found", name)
	return nil
}

func TestGoFrontend_CallsInStatementInit_v2658(t *testing.T) {
	src := `package p

func Run(x any) {
	if err := f(); err != nil {
		return
	}
	for i := g(); i < 3; i++ {
	}
	switch v := h(); v {
	case 1:
	}
	switch y := k(); t := y.(type) {
	case int:
		_ = t
	}
	switch m(x).(type) {
	case string:
	}
}
`
	fn := goFuncNamed(t, src, "Run")
	got := goCallees(fn)
	for _, want := range []string{"f", "g", "h", "k", "m"} {
		if !got[want] {
			t.Errorf("no NodeCall for %s; callees = %v", want, got)
		}
	}
	var guards []string
	for _, ch := range fn.Children {
		if ch.Kind == NodeBranch && ch.Name == "type-switch" {
			guards = append(guards, ch.RawSource)
		}
	}
	if len(guards) != 2 || guards[0] != "t := y.(type)" || guards[1] != "m(x).(type)" {
		t.Errorf("type switch RawSource = %q, want [\"t := y.(type)\" \"m(x).(type)\"]", guards)
	}
}
