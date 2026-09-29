// D102: the TS and Python frontends build class methods with no Receiver, so
// extractNode gave every such method the QualName ".name". Two classes in one
// file with a method of the same name (constructor, save, __init__) shared one
// symbol ID: the index kept one and the call edges of both merged.
package symbols

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

func classWithMethods(lang ast.Language, class string, methods ...*ast.Node) *ast.Node {
	return &ast.Node{Kind: ast.NodeClass, Name: class, Language: lang, Exported: true, Children: methods}
}

func bareMethod(lang ast.Language, name string, callees ...string) *ast.Node {
	m := &ast.Node{Kind: ast.NodeMethod, Name: name, Language: lang, Exported: true}
	for _, c := range callees {
		m.Children = append(m.Children, &ast.Node{Kind: ast.NodeCall, Callee: c, Language: lang})
	}
	return m
}

func TestD102_MethodsOfTwoClassesKeepTheirOwnIDs(t *testing.T) {
	for _, tc := range []struct {
		lang ast.Language
		file string
		name string
	}{
		{ast.LangTS, "svc/worker.ts", "save"},
		{ast.LangPython, "svc/worker.py", "__init__"},
	} {
		m := &ast.Module{Language: tc.lang, FilePath: tc.file, PackageName: "worker", Nodes: []*ast.Node{
			classWithMethods(tc.lang, "A", bareMethod(tc.lang, tc.name, "helperA")),
			classWithMethods(tc.lang, "B", bareMethod(tc.lang, tc.name, "helperB")),
		}}
		idx := NewIndex()
		idx.AddModule(m)
		idx.ResolveAll()

		aID := SymbolID(tc.file, SymMethod, "A."+tc.name)
		bID := SymbolID(tc.file, SymMethod, "B."+tc.name)
		for _, id := range []string{aID, bID} {
			s, ok := idx.ByID(id)
			if !ok {
				t.Errorf("%s: symbol %q missing", tc.lang, id)
				continue
			}
			if s.QualName != id[len(tc.file)+len("#method:"):] || s.ReceiverType == "" {
				t.Errorf("%s: symbol %q has QualName %q ReceiverType %q", tc.lang, id, s.QualName, s.ReceiverType)
			}
		}
		callers := map[string]string{}
		for _, e := range idx.AllCallEdges() {
			callers[e.CalleeName] = e.CallerID
		}
		if callers["helperA"] != aID || callers["helperB"] != bID {
			t.Errorf("%s: call edges use callers %v, want helperA from %q and helperB from %q", tc.lang, callers, aID, bID)
		}
	}
}

// A Go or Java method keeps its receiver, whatever encloses it.
func TestD102_ReceiverStillWins(t *testing.T) {
	recv := ast.TypeRef{Name: "Svc"}
	m := &ast.Module{Language: ast.LangJava, FilePath: "a/Svc.java", PackageName: "a", Nodes: []*ast.Node{
		classWithMethods(ast.LangJava, "Svc", &ast.Node{Kind: ast.NodeMethod, Name: "run", Receiver: &recv, Language: ast.LangJava}),
	}}
	tb := BuildTable(m)
	if _, ok := tb.Get(SymbolID("a/Svc.java", SymMethod, "Svc.run")); !ok {
		t.Fatal("Java method lost its receiver ID")
	}
}

// Go interface methods have no Receiver either. They get the interface name in
// the ID, so two interfaces no longer collide, but no ReceiverType, since an
// interface method has no receiver.
func TestD102_InterfaceMethodsQualifiedWithoutReceiverType(t *testing.T) {
	iface := func(name string) *ast.Node {
		return &ast.Node{Kind: ast.NodeInterface, Name: name, Language: ast.LangGo, Exported: true,
			Children: []*ast.Node{{Kind: ast.NodeMethod, Name: "Get", Language: ast.LangGo, Exported: true}}}
	}
	tb := BuildTable(&ast.Module{Language: ast.LangGo, FilePath: "p/s.go", PackageName: "p", Nodes: []*ast.Node{iface("Reader"), iface("Store")}})
	for _, qn := range []string{"Reader.Get", "Store.Get"} {
		s, ok := tb.Get(SymbolID("p/s.go", SymMethod, qn))
		if !ok {
			t.Errorf("interface method %q missing", qn)
			continue
		}
		if s.ReceiverType != "" {
			t.Errorf("interface method %q has ReceiverType %q, want empty", qn, s.ReceiverType)
		}
	}
}
