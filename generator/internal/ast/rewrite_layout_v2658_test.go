// OPEN-REWRITE: RewriteFunctionBody printed the new body with positions from a
// second file set, so line breaks inside a statement moved ("return total *
// \n 2, nil") and other declarations lost their layout. The output was valid
// Go that gofmt would change.
package ast

import (
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rewriteServiceSrc = `package service

type OrderService struct{ n int }

func NewOrderService() *OrderService { return &OrderService{} }

// Checkout returns the order total.
func (s *OrderService) Checkout(id int) (int, error) {
	return s.n + id, nil
}

// Tail stays as it is.
func Tail() {}
`

func rewriteFixture(t *testing.T, src string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "order_service.go")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func assertGofmtClean(t *testing.T, src []byte) {
	t.Helper()
	formatted, err := format.Source(src)
	if err != nil {
		t.Fatalf("output does not parse: %v\n%s", err, src)
	}
	if string(formatted) != string(src) {
		t.Errorf("output is not gofmt clean:\n%s\ngofmt gives:\n%s", src, formatted)
	}
}

func TestRewriteFunctionBody_KeepsLayout_v2658(t *testing.T) {
	p := rewriteFixture(t, rewriteServiceSrc)
	if err := RewriteFunctionBody(p, "Checkout", "total := s.n + id\n\treturn total * 2, nil"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	want := strings.Replace(rewriteServiceSrc, "\treturn s.n + id, nil\n",
		"\ttotal := s.n + id\n\treturn total * 2, nil\n", 1)
	if string(got) != want {
		t.Errorf("rewritten file:\n%s\nwant:\n%s", got, want)
	}
	assertGofmtClean(t, got)
}

func TestRewriteFunctionBody_StubIsGofmtClean_v2658(t *testing.T) {
	p := rewriteFixture(t, rewriteServiceSrc)
	if err := RewriteFunctionBody(p, "Added", "x := 1\n_ = x"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(got), rewriteServiceSrc) || !strings.Contains(string(got), "func Added() {\n\tx := 1\n\t_ = x\n}\n") {
		t.Errorf("stub not appended as expected:\n%s", got)
	}
	assertGofmtClean(t, got)
}

// A body that closes the function early and opens another one must not get
// into the file: the splice would add a declaration the caller never asked for.
func TestRewriteFunctionBody_RejectsBodyThatLeavesTheFunction_v2658(t *testing.T) {
	p := rewriteFixture(t, rewriteServiceSrc)
	err := RewriteFunctionBody(p, "Checkout", "return 0, nil\n}\n\nfunc Extra() {")
	if err == nil {
		t.Error("RewriteFunctionBody accepted a body that closes the function")
	}
	got, _ := os.ReadFile(p)
	if string(got) != rewriteServiceSrc {
		t.Errorf("file changed:\n%s", got)
	}
}
