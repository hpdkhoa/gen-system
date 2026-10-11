// index_import_test.go (v26.34): a package qualified call resolves through the
// caller file's imports, and never falls back to a bare name capture.
//
// Before this, `errors.New` was stripped to `New` and resolved to whatever
// project symbol was called New. On a generated backend with a store.New,
// every errors.New call became a project edge into the store. And a stdlib
// call that nothing captured became an "unresolved project call" in the
// verify stage.
package symbols

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

func importDecl(name, path string) *ast.Node {
	return &ast.Node{
		ID:         "import:" + path,
		Kind:       ast.NodeImport,
		Name:       name,
		Language:   ast.LangGo,
		ImportPath: path,
		Span:       ast.Span{Start: ast.Pos{Line: 1}},
	}
}

func callNode(callee string) *ast.Node {
	return &ast.Node{Kind: ast.NodeCall, Callee: callee, Language: ast.LangGo}
}

func funcWithCalls(name string, callees ...string) *ast.Node {
	f := funcNode(name, true)
	for _, c := range callees {
		f.Children = append(f.Children, callNode(c))
	}
	return f
}

func edgesFrom(idx *Index, caller string) map[string]string {
	out := map[string]string{}
	for _, e := range idx.AllCallEdges() {
		if e.CallerID == caller {
			out[e.CalleeName] = e.CalleeID
		}
	}
	return out
}

func buildImportIndex() (*Index, string) {
	idx := NewIndex()
	// app/internal/store/store.go: func New
	idx.AddModule(makeModule("app/internal/store/store.go", funcNode("New", true)))
	// app/internal/service/svc.go: imports store and errors; calls both, plus a
	// receiver qualified call and a function literal.
	svc := makeModule("app/internal/service/svc.go",
		importDecl("store", "app/internal/store"),
		importDecl("errors", "errors"),
		funcWithCalls("Run", "store.New", "errors.New", "s.helper.New", ast.FuncLiteralCallee),
	)
	idx.AddModule(svc)
	callerID := SymbolID("app/internal/service/svc.go", SymFunc, "Run")
	return idx, callerID
}

func TestQualifiedCallResolvesThroughImport(t *testing.T) {
	idx, caller := buildImportIndex()
	idx.ResolveAll()
	got := edgesFrom(idx, caller)
	want := SymbolID("app/internal/store/store.go", SymFunc, "New")
	if got["store.New"] != want {
		t.Fatalf("store.New resolved to %q, want %q", got["store.New"], want)
	}
}

func TestQualifiedCallToAbsentPackageStaysUnresolved(t *testing.T) {
	idx, caller := buildImportIndex()
	idx.ResolveAll()
	got := edgesFrom(idx, caller)
	if got["errors.New"] != "" {
		t.Fatalf("errors.New was captured by %q; a call into a package that is not in the index must stay unresolved", got["errors.New"])
	}
}

func TestReceiverQualifiedCallFallsBackToBareName(t *testing.T) {
	idx, caller := buildImportIndex()
	idx.ResolveAll()
	got := edgesFrom(idx, caller)
	// "s" is not an import, so the old bare name rule applies, unchanged.
	if got["s.helper.New"] == "" {
		t.Fatal("a receiver qualified call should still resolve by bare name")
	}
}

func TestFuncLiteralNeverResolves(t *testing.T) {
	idx, caller := buildImportIndex()
	idx.ResolveAll()
	if got := edgesFrom(idx, caller)[ast.FuncLiteralCallee]; got != "" {
		t.Fatalf("function literal resolved to %q", got)
	}
}

func TestResolveCallEdgesPathAgrees(t *testing.T) {
	// The second entry point (BuildFromModules → resolveCallEdges) must apply
	// the same import rule as ResolveAll; v26.26 was about exactly this drift.
	idx, caller := buildImportIndex()
	idx.resolveCallEdges()
	got := edgesFrom(idx, caller)
	if got["errors.New"] != "" || got["store.New"] == "" {
		t.Fatalf("resolveCallEdges disagrees with ResolveAll: %v", got)
	}
}

func TestImportAliasIsHonoured(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("app/internal/store/store.go", funcNode("New", true)))
	alias := importDecl("store", "app/internal/store")
	alias.ImportAlias = "st"
	idx.AddModule(makeModule("app/internal/service/svc.go", alias, funcWithCalls("Run", "st.New")))
	idx.ResolveAll()
	caller := SymbolID("app/internal/service/svc.go", SymFunc, "Run")
	if edgesFrom(idx, caller)["st.New"] == "" {
		t.Fatal("st.New should resolve through the import alias st")
	}
}

// A project package and a stdlib package can share a trailing directory name
// (internal/trace and runtime/trace). The project import must resolve to the
// project symbol whichever one sorts first by ID, and the stdlib import must
// resolve to the stdlib symbol.
func TestImportSuffixTieBreaksTowardTheLongerMatchThenProject(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("app/internal/trace/trace.go", funcNode("Start", true)))
	std := makeModule("/goroot/src/runtime/trace/trace.go", funcNode("Start", true))
	idx.AddModuleWithProvenance(std, ProvStdlib)
	svc := makeModule("app/internal/agent/agent.go",
		importDecl("trace", "app/internal/trace"),
		funcWithCalls("Run", "trace.Start"),
	)
	idx.AddModule(svc)
	rt := makeModule("app/internal/other/other.go",
		importDecl("trace", "runtime/trace"),
		funcWithCalls("Go", "trace.Start"),
	)
	idx.AddModule(rt)
	idx.ResolveAll()
	projectStart := SymbolID("app/internal/trace/trace.go", SymFunc, "Start")
	stdStart := SymbolID("/goroot/src/runtime/trace/trace.go", SymFunc, "Start")
	if got := edgesFrom(idx, SymbolID("app/internal/agent/agent.go", SymFunc, "Run"))["trace.Start"]; got != projectStart {
		t.Fatalf("project import resolved to %q, want the project Start", got)
	}
	if got := edgesFrom(idx, SymbolID("app/internal/other/other.go", SymFunc, "Go"))["trace.Start"]; got != stdStart {
		t.Fatalf("runtime/trace import resolved to %q, want the stdlib Start", got)
	}
}
