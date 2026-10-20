package symbols

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

// v26.74: a bare Go call stays in its package, and a method call follows the receiver's declared type. Before, the
// first project symbol of that name won: fail() in one package linked to fail in another, and decl.End() on an
// *ast.FuncDecl linked to a project method Timer.End.
func TestGoCallsFollowPackageAndReceiverType(t *testing.T) {
	files := map[string]string{
		"app/trace/trace.go": `package trace
type Timer struct{}
func (t *Timer) End() {}
func (t *Timer) Stop() { t.End() }
func fail() {}
`,
		"app/svc/svc.go": `package svc
import "go/ast"
import "app/trace"
func fail() {}
func Run(decl *ast.FuncDecl, tm *trace.Timer) {
	fail()
	decl.End()
	tm.End()
}
type Store struct{}
func (s *Store) Save() {}
type Cache struct{}
func (c *Cache) Save() {}
func Use(x int) {
	var s *Store
	s.Save()
	var m map[int]*Store
	u := m[x]
	u.Save()
}
func NewCache() *Cache { return nil }
func Locals() {
	a := &Cache{}
	a.Save()
	b := NewCache()
	b.Save()
	func(c *Store) { c.Save() }(nil)
}
func Only() {
	var t *trace.Timer
	t.Stop()
}
`,
	}
	idx := NewIndex()
	for p, src := range files {
		m, err := ast.ParseFile(p, []byte(src))
		if err != nil || m == nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		idx.AddModule(m)
	}
	idx.ResolveAll()
	run := edgesFrom(idx, SymbolID("app/svc/svc.go", SymFunc, "Run"))
	if got, want := run["fail"], SymbolID("app/svc/svc.go", SymFunc, "fail"); got != want {
		t.Errorf("fail() resolved to %q, want its own package's %q", got, want)
	}
	if got := run["decl.End"]; got != "" {
		t.Errorf("decl.End() on *ast.FuncDecl resolved to %q, want nothing", got)
	}
	if got, want := run["tm.End"], SymbolID("app/trace/trace.go", SymMethod, "Timer.End"); got != want {
		t.Errorf("tm.End() on *trace.Timer resolved to %q, want %q", got, want)
	}
	stop := edgesFrom(idx, SymbolID("app/trace/trace.go", SymMethod, "Timer.Stop"))
	if got, want := stop["t.End"], SymbolID("app/trace/trace.go", SymMethod, "Timer.End"); got != want {
		t.Errorf("t.End() inside a Timer method resolved to %q, want %q", got, want)
	}
	// var s *Store shows the type; u's type is unknown and two methods named Save exist, so no guess.
	use := edgesFrom(idx, SymbolID("app/svc/svc.go", SymFunc, "Use"))
	if got, want := use["s.Save"], SymbolID("app/svc/svc.go", SymMethod, "Store.Save"); got != want {
		t.Errorf("s.Save() on var s *Store resolved to %q, want %q", got, want)
	}
	if got := use["u.Save"]; got != "" {
		t.Errorf("u.Save() with two Save methods resolved to %q, want nothing", got)
	}
	// Locals: &Cache{}, NewCache() returning *Cache, and a function literal's parameter.
	locals := edgesFrom(idx, SymbolID("app/svc/svc.go", SymFunc, "Locals"))
	for call, want := range map[string]string{"a.Save": "Cache.Save", "b.Save": "Cache.Save", "c.Save": "Store.Save"} {
		if got := locals[call]; got != SymbolID("app/svc/svc.go", SymMethod, want) {
			t.Errorf("%s() resolved to %q, want %s", call, got, want)
		}
	}
	// One method named Stop in the whole project: an unknown receiver may still take it.
	if got, want := edgesFrom(idx, SymbolID("app/svc/svc.go", SymFunc, "Only"))["t.Stop"], SymbolID("app/trace/trace.go", SymMethod, "Timer.Stop"); got != want {
		t.Errorf("t.Stop() resolved to %q, want the only Stop %q", got, want)
	}
}

func TestImportNameDropsMajorVersion(t *testing.T) {
	for path, want := range map[string]string{
		"github.com/go-chi/chi/v5": "chi", "gopkg.in/yaml.v3": "yaml", "net/http": "http", "fmt": "fmt",
		"github.com/x/vector": "vector", "golang.org/x/tools/go/packages": "packages",
	} {
		if got := importName(path); got != want {
			t.Errorf("importName(%q) = %q, want %q", path, got, want)
		}
	}
}

// v26.74: with go.mod known, an import of a module's root package resolves to the repo root, whatever the checkout
// folder is called (the old suffix match needed a folder named after the last import segment).
func TestGoModuleRootImportResolves(t *testing.T) {
	idx := NewIndex()
	idx.AddGoModule("github.com/go-chi/chi/v5", "checkout")
	for p, src := range map[string]string{
		"checkout/chi.go":               "package chi\nfunc NewRouter() {}\n",
		"checkout/middleware/m_test.go": "package middleware\nimport \"github.com/go-chi/chi/v5\"\nfunc TestX() { chi.NewRouter() }\n",
		"other/chi/router.go":           "package chi\nfunc NewRouter() {}\n",
	} {
		m, err := ast.ParseFile(p, []byte(src))
		if err != nil || m == nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		idx.AddModule(m)
	}
	idx.ResolveAll()
	got := edgesFrom(idx, SymbolID("checkout/middleware/m_test.go", SymFunc, "TestX"))["chi.NewRouter"]
	if want := SymbolID("checkout/chi.go", SymFunc, "NewRouter"); got != want {
		t.Errorf("chi.NewRouter resolved to %q, want the module root's %q", got, want)
	}
}

// v26.74: with a go.mod known, a chain from a variable of an outside type stays outside (r.Header.Get on an
// *http.Request), and s := store.New() takes New's result type from the other package.
func TestGoVariableTypesWithModule(t *testing.T) {
	idx := NewIndex()
	idx.AddGoModule("example.com/app", "app")
	for p, src := range map[string]string{
		"app/srv/srv.go": "package srv\nimport (\n\t\"net/http\"\n\t\"example.com/app/store\"\n)\n" +
			"type Header struct{}\nfunc (h Header) Get() {}\n" +
			"func Handle(r *http.Request) { r.Header.Get() }\n" +
			"func Use() { s := store.New(); s.Get() }\n",
		"app/store/store.go": "package store\ntype Item struct{}\nfunc New() *Item { return nil }\nfunc (i *Item) Get() {}\n",
	} {
		m, err := ast.ParseFile(p, []byte(src))
		if err != nil || m == nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		idx.AddModule(m)
	}
	idx.ResolveAll()
	if got := edgesFrom(idx, SymbolID("app/srv/srv.go", SymFunc, "Handle"))["r.Header.Get"]; got != "" {
		t.Errorf("r.Header.Get() on *http.Request resolved to %q, want nothing", got)
	}
	got := edgesFrom(idx, SymbolID("app/srv/srv.go", SymFunc, "Use"))["s.Get"]
	if want := SymbolID("app/store/store.go", SymMethod, "Item.Get"); got != want {
		t.Errorf("s.Get() on store.New() resolved to %q, want %q", got, want)
	}
}

// v26.74: a local is visible only in its scope, so an inner r *http.Request shadows an outer r := NewRouter() only
// inside its literal; and a chain follows fields and method results (c.Flags().Get leaves the project).
func TestGoLocalScopesAndChains(t *testing.T) {
	idx := NewIndex()
	idx.AddGoModule("example.com/app", "app")
	src := "package app\nimport \"net/http\"\nimport \"example.com/other/flag\"\n" +
		"type Router struct{}\nfunc NewRouter() *Router { return nil }\nfunc (r *Router) Get(h func(http.ResponseWriter, *http.Request)) {}\n" +
		"type Ctx struct{}\nfunc (c *Ctx) Get() {}\nfunc (c *Ctx) Context() {}\n" +
		"type Cmd struct{ ctx *Ctx }\nfunc (c *Cmd) Flags() *flag.Set { return nil }\nfunc (c *Cmd) Sub() *Ctx { return nil }\n" +
		"func Test() {\n" +
		"\tr := NewRouter()\n" +
		"\tr.Get(func(w http.ResponseWriter, r *http.Request) {\n" +
		"\t\tr.Context()\n" +
		"\t})\n" +
		"}\n" +
		"func Chain(c *Cmd) {\n" +
		"\tc.Flags().Get()\n" +
		"\tc.Sub().Get()\n" +
		"\tc.ctx.Get()\n" +
		"\tsub := c.Sub()\n" +
		"\tsub.Context()\n" +
		"}\n"
	m, err := ast.ParseFile("app/app.go", []byte(src))
	if err != nil || m == nil {
		t.Fatalf("parse: %v", err)
	}
	idx.AddModule(m)
	idx.ResolveAll()
	test := edgesFrom(idx, SymbolID("app/app.go", SymFunc, "Test"))
	if got, want := test["r.Get"], SymbolID("app/app.go", SymMethod, "Router.Get"); got != want {
		t.Errorf("r.Get() on r := NewRouter() resolved to %q, want %q", got, want)
	}
	if got := test["r.Context"]; got != "" {
		t.Errorf("r.Context() on the literal's r *http.Request resolved to %q, want nothing", got)
	}
	chain := edgesFrom(idx, SymbolID("app/app.go", SymFunc, "Chain"))
	if got := chain["c.Flags().Get"]; got != "" {
		t.Errorf("c.Flags().Get() on an outside type resolved to %q, want nothing", got)
	}
	for call, want := range map[string]string{"c.Sub().Get": "Ctx.Get", "c.ctx.Get": "Ctx.Get", "sub.Context": "Ctx.Context"} {
		if got := chain[call]; got != SymbolID("app/app.go", SymMethod, want) {
			t.Errorf("%s() resolved to %q, want %s", call, got, want)
		}
	}
}

// v26.74: for _, x := range r.items takes the element type, y := r.first copies a field's type, and NewBox().Open()
// and new(Item).Open() start a chain at a call.
func TestGoRangeFieldAndCallRoots(t *testing.T) {
	idx := NewIndex()
	src := "package app\n" +
		"type Item struct{}\nfunc (i *Item) Open() {}\n" +
		"type Box struct{ items []*Item; first *Item; byName map[string]*Item }\nfunc NewBox() *Box { return nil }\nfunc (b *Box) Open() {}\n" +
		"type Other struct{}\nfunc (o *Other) Open() {}\n" +
		"func (b *Box) Walk() {\n" +
		"\tfor _, it := range b.items {\n" +
		"\t\tit.Open()\n" +
		"\t}\n" +
		"\tfor _, v := range b.byName {\n" +
		"\t\tv.Open()\n" +
		"\t}\n" +
		"\tf := b.first\n" +
		"\tf.Open()\n" +
		"\tNewBox().Open()\n" +
		"\tnew(Item).Open()\n" +
		"}\n"
	m, err := ast.ParseFile("app/app.go", []byte(src))
	if err != nil || m == nil {
		t.Fatalf("parse: %v", err)
	}
	idx.AddModule(m)
	idx.ResolveAll()
	walk := edgesFrom(idx, SymbolID("app/app.go", SymMethod, "Box.Walk"))
	for call, want := range map[string]string{"it.Open": "Item.Open", "v.Open": "Item.Open", "f.Open": "Item.Open", "NewBox().Open": "Box.Open", "new(Item).Open": "Item.Open"} {
		if got := walk[call]; got != SymbolID("app/app.go", SymMethod, want) {
			t.Errorf("%s() resolved to %q, want %s", call, got, want)
		}
	}
}

// v26.74: out, err := run() gives err the second result's type, so err.Error() is not any project method named Error.
func TestGoSecondResultType(t *testing.T) {
	idx := NewIndex()
	src := "package app\n" +
		"type Fail struct{}\nfunc (f *Fail) Error() string { return \"\" }\n" +
		"type Other struct{}\nfunc (o *Other) Error() string { return \"\" }\n" +
		"func run() (string, *Fail) { return \"\", nil }\n" +
		"func Use() {\n" +
		"\tout, err := run()\n" +
		"\t_ = out\n" +
		"\terr.Error()\n" +
		"}\n"
	m, err := ast.ParseFile("app/app.go", []byte(src))
	if err != nil || m == nil {
		t.Fatalf("parse: %v", err)
	}
	idx.AddModule(m)
	idx.ResolveAll()
	got := edgesFrom(idx, SymbolID("app/app.go", SymFunc, "Use"))["err.Error"]
	if want := SymbolID("app/app.go", SymMethod, "Fail.Error"); got != want {
		t.Errorf("err.Error() resolved to %q, want %q", got, want)
	}
}

// v26.74: a type assertion and a type switch clause show the type, so n.End() picks the asserted type's method.
func TestGoTypeAssertions(t *testing.T) {
	idx := NewIndex()
	src := "package app\n" +
		"type A struct{}\nfunc (a *A) End() {}\n" +
		"type B struct{}\nfunc (b *B) End() {}\n" +
		"func F(v any) {\n" +
		"\ta := v.(*A)\n" +
		"\ta.End()\n" +
		"\tb, _ := v.(*B)\n" +
		"\tb.End()\n" +
		"\tswitch x := v.(type) {\n" +
		"\tcase *A:\n" +
		"\t\tx.End()\n" +
		"\t}\n" +
		"}\n"
	m, err := ast.ParseFile("app/app.go", []byte(src))
	if err != nil || m == nil {
		t.Fatalf("parse: %v", err)
	}
	idx.AddModule(m)
	idx.ResolveAll()
	f := edgesFrom(idx, SymbolID("app/app.go", SymFunc, "F"))
	for call, want := range map[string]string{"a.End": "A.End", "b.End": "B.End", "x.End": "A.End"} {
		if got := f[call]; got != SymbolID("app/app.go", SymMethod, want) {
			t.Errorf("%s() resolved to %q, want %s", call, got, want)
		}
	}
}
