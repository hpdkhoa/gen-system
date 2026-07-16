package ast

import (
	"testing"
)

// ─── NodeKind / Language constants ───────────────────────────

func TestNodeKindConstants(t *testing.T) {
	kinds := []NodeKind{
		NodeModule, NodeFunction, NodeMethod, NodeStruct, NodeClass,
		NodeInterface, NodeEnum, NodeTypeAlias, NodeVariable, NodeConstant,
		NodeImport, NodeExport, NodeCall, NodeReturn, NodeAssign,
		NodeBranch, NodeLoop, NodeDefer, NodeTry, NodeEscape, NodeProcedure,
		NodeUnknown,
	}
	seen := map[NodeKind]bool{}
	for _, k := range kinds {
		if seen[k] {
			t.Errorf("duplicate NodeKind value: %q", k)
		}
		seen[k] = true
		if k == "" {
			t.Error("empty NodeKind string")
		}
	}
}

func TestLanguageConstants(t *testing.T) {
	langs := []Language{LangGo, LangTS, LangPython, LangCAGen, LangUnknown}
	seen := map[Language]bool{}
	for _, l := range langs {
		if seen[l] {
			t.Errorf("duplicate Language value: %q", l)
		}
		seen[l] = true
	}
}

// ─── Span ────────────────────────────────────────────────────

func TestSpanIsValid(t *testing.T) {
	cases := []struct {
		span  Span
		valid bool
	}{
		{Span{Start: Pos{Line: 1, Offset: 0}, End: Pos{Line: 1, Offset: 10}}, true},
		{Span{Start: Pos{Line: 1, Offset: 5}, End: Pos{Line: 1, Offset: 3}}, false},
		{Span{Start: Pos{Line: 1}, End: Pos{Line: 2}}, true},
		{Span{}, false},
	}
	for _, c := range cases {
		got := c.span.IsValid()
		if got != c.valid {
			t.Errorf("Span%+v.IsValid() = %v, want %v", c.span, got, c.valid)
		}
	}
}

// ─── TypeRef ─────────────────────────────────────────────────

func TestTypeRefString(t *testing.T) {
	cases := []struct {
		ref  TypeRef
		want string
	}{
		{TypeRef{Name: "string"}, "string"},
		{TypeRef{Name: "Request", Package: "http"}, "http.Request"},
		{TypeRef{Name: "T"}, "T"},
		{TypeRef{}, ""},
	}
	for _, c := range cases {
		got := c.ref.String()
		if got != c.want {
			t.Errorf("TypeRef%+v.String() = %q, want %q", c.ref, got, c.want)
		}
	}
}

func TestUnknownType(t *testing.T) {
	if UnknownType.Name != "?" {
		t.Errorf("UnknownType.Name = %q, want %q", UnknownType.Name, "?")
	}
}

// ─── Module.TopLevelByKind ────────────────────────────────────

func TestModuleTopLevelByKind(t *testing.T) {
	m := &Module{
		Language: LangGo,
		Nodes: []*Node{
			{Kind: NodeFunction, Name: "Foo"},
			{Kind: NodeStruct, Name: "Bar"},
			{Kind: NodeFunction, Name: "Baz"},
			{Kind: NodeImport, Name: "fmt"},
			{Kind: NodeVariable, Name: "x"},
		},
	}

	funcs := m.TopLevelByKind(NodeFunction)
	if len(funcs) != 2 {
		t.Errorf("TopLevelByKind(NodeFunction) = %d nodes, want 2", len(funcs))
	}
	if funcs[0].Name != "Foo" || funcs[1].Name != "Baz" {
		t.Errorf("TopLevelByKind returned wrong names: %v, %v", funcs[0].Name, funcs[1].Name)
	}

	multi := m.TopLevelByKind(NodeStruct, NodeVariable)
	if len(multi) != 2 {
		t.Errorf("TopLevelByKind(Struct,Var) = %d, want 2", len(multi))
	}

	none := m.TopLevelByKind(NodeEnum)
	if len(none) != 0 {
		t.Errorf("TopLevelByKind(Enum) should be empty, got %d", len(none))
	}
}

// ─── Module.UnknownNodes ─────────────────────────────────────

func TestModuleUnknownNodes_Flat(t *testing.T) {
	m := &Module{
		Nodes: []*Node{
			{Kind: NodeFunction, Name: "Foo"},
			{Kind: NodeUnknown, UnknownReason: "complex_expr"},
			{Kind: NodeStruct, Name: "Bar"},
		},
	}
	unknowns := m.UnknownNodes()
	if len(unknowns) != 1 {
		t.Fatalf("UnknownNodes() = %d, want 1", len(unknowns))
	}
	if unknowns[0].UnknownReason != "complex_expr" {
		t.Errorf("unexpected reason: %q", unknowns[0].UnknownReason)
	}
}

func TestModuleUnknownNodes_Nested(t *testing.T) {
	m := &Module{
		Nodes: []*Node{
			{
				Kind: NodeFunction,
				Name: "Foo",
				Children: []*Node{
					{Kind: NodeCall, Callee: "bar"},
					{Kind: NodeUnknown, UnknownReason: "body_not_parsed"},
					{
						Kind: NodeStruct,
						Children: []*Node{
							{Kind: NodeUnknown, UnknownReason: "deeply_nested"},
						},
					},
				},
			},
		},
	}
	unknowns := m.UnknownNodes()
	if len(unknowns) != 2 {
		t.Fatalf("UnknownNodes() = %d, want 2 (nested)", len(unknowns))
	}
}

func TestModuleUnknownNodes_Empty(t *testing.T) {
	m := &Module{Nodes: []*Node{{Kind: NodeFunction, Name: "F"}}}
	if got := m.UnknownNodes(); len(got) != 0 {
		t.Errorf("UnknownNodes() should be empty for clean module, got %d", len(got))
	}
}
