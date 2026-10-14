// go_init_order_v2668_test.go (v26.68): the Init of an if, for or switch
// (`if err := f(); err != nil`) went into the node's Children, which for an if
// is the then-body. The CFG drew `err := f()` inside the yes arm, after the
// decision. The Init now comes before the node it belongs to.
package ast

import "testing"

const goInitSrc = `package p

func Run() {
	if err := load(); err != nil {
		report(err)
	}
	for i := start(); i < 3; i++ {
		step(i)
	}
	switch v := pick(); v {
	case 1:
		one()
	}
}
`

func TestGoInitStatementsComeBeforeTheirNode(t *testing.T) {
	m, err := (&GoFrontend{}).Parse("p.go", []byte(goInitSrc))
	if err != nil {
		t.Fatal(err)
	}
	var run *Node
	for _, n := range m.Nodes {
		if n.Kind == NodeFunction && n.Name == "Run" {
			run = n
		}
	}
	if run == nil {
		t.Fatal("no Run function")
	}
	type step struct {
		kind NodeKind
		name string
	}
	var got []step
	for _, n := range run.Children {
		got = append(got, step{n.Kind, n.Name})
	}
	want := []step{
		{NodeAssign, "err"}, {NodeBranch, "if"},
		{NodeAssign, "i"}, {NodeLoop, "for"},
		{NodeAssign, "v"}, {NodeBranch, "switch"},
	}
	if len(got) != len(want) {
		t.Fatalf("Run body = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Run body = %v, want %v", got, want)
		}
	}
	// The then-body of the if holds report, not load.
	for _, c := range run.Children[1].Children {
		if c.Kind == NodeCall && c.Callee == "load" {
			t.Fatal("load() sits inside the then-body of the if")
		}
	}
}
