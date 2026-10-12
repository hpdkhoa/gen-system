package symbols

import (
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

// v26.66: wrong edges seen on examples/shop with the Go stdlib attached.
// http.Error resolved to the method http2noCachedConnError.Error, len to the
// struct field typedPointer.len, err.Error to json.InvalidUnmarshalError.Error,
// and stdlib code called into the project's Store.Reserve.

func stdModule(path string, nodes ...*ast.Node) *ast.Module { return makeModule(path, nodes...) }

func TestPackageCallResolvesToFunctionNotMethod(t *testing.T) {
	idx := NewIndex()
	idx.AddModuleWithProvenance(stdModule("/goroot/src/net/http/h2.go", methodNode("noCachedConnError", "Error")), ProvStdlib)
	idx.AddModule(makeModule("app/shop/checkout.go", importDecl("http", "net/http"), funcWithCalls("Checkout", "http.Error")))
	idx.ResolveAll()
	caller := SymbolID("app/shop/checkout.go", SymFunc, "Checkout")
	if got := edgesFrom(idx, caller)["http.Error"]; got != "" {
		t.Fatalf("http.Error resolved to %q; a package call must not match a method", got)
	}

	idx.AddModuleWithProvenance(stdModule("/goroot/src/net/http/server.go", funcNode("Error", true)), ProvStdlib)
	idx.ResolveAll()
	want := SymbolID("/goroot/src/net/http/server.go", SymFunc, "Error")
	if got := edgesFrom(idx, caller)["http.Error"]; got != want {
		t.Fatalf("http.Error resolved to %q, want the function %q", got, want)
	}
}

func TestChainedPackageCallMayResolveToMethod(t *testing.T) {
	idx := NewIndex()
	idx.AddModuleWithProvenance(stdModule("/goroot/src/encoding/json/stream.go",
		funcNode("NewDecoder", true), methodNode("Decoder", "Decode")), ProvStdlib)
	idx.AddModule(makeModule("app/shop/checkout.go", importDecl("json", "encoding/json"),
		funcWithCalls("Checkout", "json.NewDecoder().Decode")))
	idx.ResolveAll()
	got := edgesFrom(idx, SymbolID("app/shop/checkout.go", SymFunc, "Checkout"))["json.NewDecoder().Decode"]
	if want := SymbolID("/goroot/src/encoding/json/stream.go", SymMethod, "Decoder.Decode"); got != want {
		t.Fatalf("json.NewDecoder().Decode resolved to %q, want %q", got, want)
	}
}

func TestGoBuiltinAndFieldsNeverResolve(t *testing.T) {
	idx := NewIndex()
	idx.AddModuleWithProvenance(stdModule("/goroot/src/encoding/json/v2/arshal.go",
		structNode("typedPointer", ast.Field{Name: "len", Type: ast.TypeRef{Name: "int"}},
			ast.Field{Name: "count", Type: ast.TypeRef{Name: "int"}})), ProvStdlib)
	idx.AddModule(makeModule("app/shop/checkout.go", funcWithCalls("Checkout", "len", "count")))
	idx.ResolveAll()
	edges := edgesFrom(idx, SymbolID("app/shop/checkout.go", SymFunc, "Checkout"))
	if edges["len"] != "" || edges["count"] != "" {
		t.Fatalf("a builtin or a field became a call target: %v", edges)
	}
}

func TestBareGoCallDoesNotMatchAMethod(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("app/shop/store.go", methodNode("Store", "Reserve")))
	idx.AddModule(makeModule("app/shop/checkout.go", funcWithCalls("Checkout", "Reserve", "s.Store.Reserve")))
	idx.ResolveAll()
	edges := edgesFrom(idx, SymbolID("app/shop/checkout.go", SymFunc, "Checkout"))
	if edges["Reserve"] != "" {
		t.Fatalf("bare Reserve resolved to %q; a bare Go call cannot call a method", edges["Reserve"])
	}
	if want := SymbolID("app/shop/store.go", SymMethod, "Store.Reserve"); edges["s.Store.Reserve"] != want {
		t.Fatalf("s.Store.Reserve resolved to %q, want the project method %q", edges["s.Store.Reserve"], want)
	}
}

func TestReceiverCallMatchesExternalOnlyByQualifiedName(t *testing.T) {
	idx := NewIndex()
	idx.AddModuleWithProvenance(stdModule("/goroot/src/encoding/json/decode.go",
		methodNode("InvalidUnmarshalError", "Error"), methodNode("Math", "Max")), ProvStdlib)
	idx.AddModule(makeModule("app/shop/checkout.go", funcWithCalls("Checkout", "err.Error", "Math.Max")))
	idx.ResolveAll()
	edges := edgesFrom(idx, SymbolID("app/shop/checkout.go", SymFunc, "Checkout"))
	if edges["err.Error"] != "" {
		t.Fatalf("err.Error resolved to %q; the receiver's type is unknown", edges["err.Error"])
	}
	if want := SymbolID("/goroot/src/encoding/json/decode.go", SymMethod, "Math.Max"); edges["Math.Max"] != want {
		t.Fatalf("Math.Max resolved to %q, want the exact match %q", edges["Math.Max"], want)
	}
}

func TestStdlibCodeNeverCallsIntoTheProject(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("app/shop/store.go", methodNode("Store", "Reserve")))
	conn := methodNode("ClientConn", "Reserve")
	conn.Children = []*ast.Node{callNode("cc.cc.Reserve")}
	idx.AddModuleWithProvenance(stdModule("/goroot/src/net/http/clientconn.go", conn), ProvStdlib)
	idx.ResolveAll()
	got := edgesFrom(idx, SymbolID("/goroot/src/net/http/clientconn.go", SymMethod, "ClientConn.Reserve"))["cc.cc.Reserve"]
	if strings.HasPrefix(got, "app/") {
		t.Fatalf("stdlib code resolved into the project: %q", got)
	}
}

// A COBOL CALL "PROGRAM" names the called program's module symbol. Making
// modules uncallable dropped CobolCraft's internal recall from 294/294 to 162.
func TestCobolCallResolvesToProgramModule(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(&ast.Module{Language: ast.LangCOBOL, FilePath: "/cbl/sub.cob", PackageName: "SUBPGM"})
	caller := &ast.Node{Kind: ast.NodeFunction, Name: "MAIN-PARA", Language: ast.LangCOBOL,
		Children: []*ast.Node{{Kind: ast.NodeCall, Callee: "SUBPGM", Language: ast.LangCOBOL}}}
	idx.AddModule(&ast.Module{Language: ast.LangCOBOL, FilePath: "/cbl/main.cob", PackageName: "MAINPGM",
		Nodes: []*ast.Node{caller}})
	idx.ResolveAll()
	got := edgesFrom(idx, SymbolID("/cbl/main.cob", SymFunc, "MAIN-PARA"))["SUBPGM"]
	if want := SymbolID("/cbl/sub.cob", SymModule, "SUBPGM"); got != want {
		t.Fatalf("CALL SUBPGM resolved to %q, want the program module %q", got, want)
	}
}
