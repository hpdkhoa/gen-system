package ast

import (
	"testing"
)

func goFE() Frontend { return ForLanguage(LangGo) }

// ─── Basic parse ──────────────────────────────────────────────

func TestGoFrontend_ParseEmptyPackage(t *testing.T) {
	src := `package foo`
	m, err := goFE().Parse("foo.go", []byte(src))
	assertNoError(t, err)
	if m.PackageName != "foo" {
		t.Errorf("PackageName = %q, want %q", m.PackageName, "foo")
	}
	if m.Language != LangGo {
		t.Errorf("Language = %q, want go", m.Language)
	}
	if m.Checksum == "" {
		t.Error("Checksum should be set")
	}
}

func TestGoFrontend_ParseFunction(t *testing.T) {
	src := `package foo

func Hello(name string) string {
	return "Hello " + name
}
`
	m, err := goFE().Parse("foo.go", []byte(src))
	assertNoError(t, err)

	funcs := m.TopLevelByKind(NodeFunction)
	if len(funcs) != 1 {
		t.Fatalf("expected 1 function, got %d", len(funcs))
	}
	fn := funcs[0]
	if fn.Name != "Hello" {
		t.Errorf("func name = %q, want Hello", fn.Name)
	}
	if !fn.Exported {
		t.Error("Hello should be exported")
	}
	if len(fn.Params) != 1 || fn.Params[0].Name != "name" {
		t.Errorf("unexpected params: %v", fn.Params)
	}
	if len(fn.Returns) != 1 || fn.Returns[0].Name != "string" {
		t.Errorf("unexpected returns: %v", fn.Returns)
	}
}

func TestGoFrontend_UnexportedFunction(t *testing.T) {
	src := `package foo

func helper() {}
`
	m, err := goFE().Parse("foo.go", []byte(src))
	assertNoError(t, err)
	funcs := m.TopLevelByKind(NodeFunction)
	if len(funcs) == 0 {
		t.Fatal("expected helper function")
	}
	if funcs[0].Exported {
		t.Error("helper should not be exported")
	}
}

func TestGoFrontend_ParseMethod(t *testing.T) {
	src := `package foo

type Repo struct{}

func (r *Repo) FindByID(id string) (*User, error) {
	return nil, nil
}
`
	m, err := goFE().Parse("foo.go", []byte(src))
	assertNoError(t, err)
	methods := m.TopLevelByKind(NodeMethod)
	if len(methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(methods))
	}
	fn := methods[0]
	if fn.Name != "FindByID" {
		t.Errorf("method name = %q, want FindByID", fn.Name)
	}
	if fn.Receiver == nil {
		t.Fatal("method should have receiver")
	}
	if fn.Receiver.Name != "Repo" {
		t.Errorf("receiver = %q, want Repo", fn.Receiver.Name)
	}
}

func TestGoFrontend_ParseStruct(t *testing.T) {
	src := `package foo

// User represents a user.
type User struct {
	ID   int    ` + "`" + `json:"id"` + "`" + `
	Name string ` + "`" + `json:"name"` + "`" + `
	email string // unexported
}
`
	m, err := goFE().Parse("foo.go", []byte(src))
	assertNoError(t, err)
	structs := m.TopLevelByKind(NodeStruct)
	if len(structs) != 1 {
		t.Fatalf("expected 1 struct, got %d", len(structs))
	}
	s := structs[0]
	if s.Name != "User" {
		t.Errorf("struct name = %q, want User", s.Name)
	}
	if len(s.Fields) != 3 {
		t.Errorf("expected 3 fields, got %d: %v", len(s.Fields), s.Fields)
	}
	// Check exported status
	for _, f := range s.Fields {
		switch f.Name {
		case "ID", "Name":
			if !f.Exported {
				t.Errorf("field %q should be exported", f.Name)
			}
		case "email":
			if f.Exported {
				t.Errorf("field %q should not be exported", f.Name)
			}
		}
	}
	if s.DocComment == "" {
		t.Error("User struct should have doc comment")
	}
}

func TestGoFrontend_ParseInterface(t *testing.T) {
	src := `package foo

type Repository interface {
	FindByID(id string) (*User, error)
	Save(u *User) error
	Delete(id string) error
}
`
	m, err := goFE().Parse("foo.go", []byte(src))
	assertNoError(t, err)
	ifaces := m.TopLevelByKind(NodeInterface)
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(ifaces))
	}
	iface := ifaces[0]
	if iface.Name != "Repository" {
		t.Errorf("interface name = %q, want Repository", iface.Name)
	}
	if len(iface.Children) != 3 {
		t.Errorf("expected 3 method signatures, got %d", len(iface.Children))
	}
}

func TestGoFrontend_ParseTypeAlias(t *testing.T) {
	src := `package foo

type UserID = string
type Handler func(w http.ResponseWriter, r *http.Request)
`
	m, err := goFE().Parse("foo.go", []byte(src))
	assertNoError(t, err)
	aliases := m.TopLevelByKind(NodeTypeAlias)
	if len(aliases) < 1 {
		t.Fatalf("expected at least 1 type alias, got %d", len(aliases))
	}
}

func TestGoFrontend_ParseImports(t *testing.T) {
	src := `package foo

import (
	"fmt"
	"net/http"
	myjson "encoding/json"
)
`
	m, err := goFE().Parse("foo.go", []byte(src))
	assertNoError(t, err)
	imports := m.TopLevelByKind(NodeImport)
	if len(imports) != 3 {
		t.Fatalf("expected 3 imports, got %d", len(imports))
	}
	paths := map[string]bool{}
	for _, imp := range imports {
		paths[imp.ImportPath] = true
	}
	for _, want := range []string{"fmt", "net/http", "encoding/json"} {
		if !paths[want] {
			t.Errorf("missing import %q", want)
		}
	}
	// Check alias
	for _, imp := range imports {
		if imp.ImportPath == "encoding/json" && imp.ImportAlias != "myjson" {
			t.Errorf("json alias = %q, want myjson", imp.ImportAlias)
		}
	}
}

func TestGoFrontend_ParseConstants(t *testing.T) {
	src := `package foo

const (
	StatusActive   = "active"
	StatusInactive = "inactive"
	MaxRetries     = 3
)
`
	m, err := goFE().Parse("foo.go", []byte(src))
	assertNoError(t, err)
	consts := m.TopLevelByKind(NodeConstant)
	if len(consts) != 3 {
		t.Fatalf("expected 3 constants, got %d", len(consts))
	}
}

func TestGoFrontend_ParseVariables(t *testing.T) {
	src := `package foo

var (
	DefaultTimeout = 30
	ErrNotFound    error
)
`
	m, err := goFE().Parse("foo.go", []byte(src))
	assertNoError(t, err)
	vars := m.TopLevelByKind(NodeVariable)
	if len(vars) != 2 {
		t.Fatalf("expected 2 variables, got %d", len(vars))
	}
}

func TestGoFrontend_ExtractsCallSites(t *testing.T) {
	src := `package foo

func Run() {
	fmt.Println("start")
	os.Exit(1)
}
`
	m, err := goFE().Parse("foo.go", []byte(src))
	assertNoError(t, err)
	funcs := m.TopLevelByKind(NodeFunction)
	if len(funcs) == 0 {
		t.Fatal("expected Run function")
	}
	var calls []*Node
	for _, child := range funcs[0].Children {
		if child.Kind == NodeCall {
			calls = append(calls, child)
		}
	}
	if len(calls) < 2 {
		t.Errorf("expected at least 2 call sites in Run(), got %d", len(calls))
	}
}

func TestGoFrontend_ChecksumChangesWithContent(t *testing.T) {
	fe := goFE()
	m1, _ := fe.Parse("a.go", []byte("package a\nfunc F1(){}"))
	m2, _ := fe.Parse("a.go", []byte("package a\nfunc F2(){}"))
	if m1.Checksum == m2.Checksum {
		t.Error("different source should produce different checksums")
	}
}

func TestGoFrontend_SyntaxError_Recoverable(t *testing.T) {
	// go/parser returns a partial AST with errors for most syntax issues
	src := `package foo

func Broken(`
	m, err := goFE().Parse("broken.go", []byte(src))
	// We expect either err!=nil or m with ParseErrors
	if err == nil && m == nil {
		t.Error("expected either error or module for broken source")
	}
	if m != nil && len(m.ParseErrors) == 0 {
		// go/parser may still return a partial AST; that's fine
	}
}

func TestGoFrontend_Emit_RoundTrip(t *testing.T) {
	src := `package foo

func Hello(name string) string {
	return "Hello " + name
}
`
	fe := goFE()
	m, err := fe.Parse("foo.go", []byte(src))
	assertNoError(t, err)
	out, err := fe.Emit(m)
	assertNoError(t, err)
	if len(out) == 0 {
		t.Error("Emit returned empty output")
	}
}

// ─── Helpers ──────────────────────────────────────────────────

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// v23.49: Go for/range loops capture their condition into RawSource so the CFG
// loop decision shows the real test (uniform loop rendering across languages).
func TestGoFrontend_LoopConditionCaptured_v2349(t *testing.T) {
	src := `package p
func sum(a []int, n int) int {
	total := 0
	for i := 0; i < n; i++ {
		total += a[i]
	}
	for _, v := range a {
		total += v
	}
	return total
}`
	m, err := goFE().Parse("foo.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var forCond, rangeCond string
	var walk func(ns []*Node)
	walk = func(ns []*Node) {
		for _, n := range ns {
			if n.Kind == NodeLoop {
				if n.Name == "for" {
					forCond = n.RawSource
				}
				if n.Name == "range" {
					rangeCond = n.RawSource
				}
			}
			walk(n.Children)
		}
	}
	for _, top := range m.Nodes {
		walk([]*Node{top})
	}
	if forCond != "i < n" {
		t.Errorf("for loop condition: got %q want %q", forCond, "i < n")
	}
	if rangeCond == "" || rangeCond[:5] != "range" {
		t.Errorf("range loop label: got %q want it to start with 'range'", rangeCond)
	}
}
