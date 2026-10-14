// entry_handler_v2668_test.go (v26.68): a Go web service showed 0 entry
// points, because only names were checked. A function or method with the
// net/http handler signature is now a handler whatever its name.
package presentation

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

const handlerSrc = `package shop

import "net/http"

type Shop struct{}

func (s *Shop) Checkout(w http.ResponseWriter, r *http.Request) {}

func health(w http.ResponseWriter, r *http.Request) {}

func render(w http.ResponseWriter, r http.Request) {}

func total(a, b int) int { return a + b }
`

func TestHTTPHandlerSignatureIsAnEntryPoint(t *testing.T) {
	m, err := (&ast.GoFrontend{}).Parse("/app/shop/checkout.go", []byte(handlerSrc))
	if err != nil {
		t.Fatal(err)
	}
	idx := symbols.NewIndex()
	idx.AddModule(m)
	got := map[string]string{}
	for _, sym := range idx.AllSymbols() {
		got[sym.QualName] = classifyEntry(sym)
	}
	want := map[string]string{
		"Shop.Checkout": "handler", // a method, named for its action
		"health":        "handler", // a function with a lower case name
		"render":        "",        // *http.Request is required, not a value
		"total":         "",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("classifyEntry(%s) = %q, want %q", name, got[name], kind)
		}
	}
}
