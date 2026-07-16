// v23_endpoints_test.go — v24: constructor detection for the call-graph relabel.
package presentation

import (
	"testing"

	"gen-system/generator/internal/ast"
	"gen-system/generator/internal/symbols"
)

func TestIsConstructorSym_v24(t *testing.T) {
	ctor := &symbols.Symbol{
		Name: "OrderService", ReceiverType: "OrderService",
		Kind: symbols.SymMethod, Language: ast.LangJava,
	}
	if !isConstructorSym(ctor) {
		t.Error("a method whose name equals its receiver type should be a constructor")
	}
	method := &symbols.Symbol{
		Name: "place", ReceiverType: "OrderService",
		Kind: symbols.SymMethod, Language: ast.LangJava,
	}
	if isConstructorSym(method) {
		t.Error("a normal method (name != receiver type) must NOT be flagged a constructor")
	}
	fn := &symbols.Symbol{Name: "main", Kind: symbols.SymFunc}
	if isConstructorSym(fn) {
		t.Error("a plain function (no receiver) must NOT be a constructor")
	}
	if isConstructorSym(nil) {
		t.Error("nil must not panic / must be false")
	}
}
