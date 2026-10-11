// cagen_multiply_v2658_test.go (v26.58, D057): tokenizeCAGen cut every line
// at the first " * ", as if it started an inline comment. A multiplication
// such as "SET ORDER.TOTAL TO ORDER.QTY * ORDER.PRICE" lost the rest of the
// expression. A "*" starts a comment only at the start of a line.
package ast

import "testing"

func findCAGenNode(nodes []*Node, kind NodeKind) *Node {
	for _, n := range nodes {
		if n.Kind == kind {
			return n
		}
		if c := findCAGenNode(n.Children, kind); c != nil {
			return c
		}
	}
	return nil
}

func TestCAGen_MultiplicationIsNotAComment(t *testing.T) {
	src := "* header comment\nPROCEDURE Totals\n  SET ORDER.TOTAL TO ORDER.QTY * ORDER.PRICE\n  IF A * B > 3\n    WRITE ORDER\n  END\nEND\n"
	m, err := cagenFE().Parse("totals.ad", []byte(src))
	assertNoError(t, err)

	as := findCAGenNode(m.Nodes, NodeAssign)
	if as == nil {
		t.Fatal("no assign node")
	}
	if want := "ORDER.QTY * ORDER.PRICE"; as.ValueLiteral != want {
		t.Errorf("assign value = %q, want %q", as.ValueLiteral, want)
	}
	br := findCAGenNode(m.Nodes, NodeBranch)
	if br == nil {
		t.Fatal("no IF node")
	}
	if want := "A * B > 3"; br.RawSource != want {
		t.Errorf("IF condition = %q, want %q", br.RawSource, want)
	}
	if len(m.TopLevelByKind(NodeProcedure)) != 1 {
		t.Errorf("want 1 procedure, the comment line must still be skipped")
	}
}
