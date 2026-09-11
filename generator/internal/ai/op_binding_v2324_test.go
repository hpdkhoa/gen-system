// generator/internal/ai/op_binding_v2324_test.go — v23.24: the verb→entity
// binding that fixes operation misplacement (Product getting no ops).
package ai

import (
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func shopEntities() []schema.Entity {
	return []schema.Entity{
		{Name: "Order", Fields: []schema.Field{{Name: "id", Type: "int"}, {Name: "total", Type: "float"}, {Name: "status", Type: "string"}}},
		{Name: "Product", Fields: []schema.Field{{Name: "id", Type: "int"}, {Name: "name", Type: "string"}, {Name: "stock", Type: "int"}}},
	}
}

func TestBindOperations_OrdersProducts_v2324(t *testing.T) {
	idea := "An orders app. Customers can check out an order and apply a discount to an order. A Product has stock; staff can adjust stock but it can't go negative."
	b := bindOperationsToEntities(idea, shopEntities())

	if len(b["Order"]) == 0 {
		t.Error("Order should bind checkout/discount operations")
	}
	joinedOrder := strings.ToLower(strings.Join(b["Order"], " "))
	if !strings.Contains(joinedOrder, "check") || !strings.Contains(joinedOrder, "discount") {
		t.Errorf("Order bindings missing checkout/discount: %v", b["Order"])
	}

	// The headline fix: Product MUST receive the stock-adjust operation.
	joinedProduct := strings.ToLower(strings.Join(b["Product"], " "))
	if !strings.Contains(joinedProduct, "stock") {
		t.Errorf("Product must bind 'adjust stock', got: %v", b["Product"])
	}
}

func TestLogicPrompt_PositiveHintForProduct_v2324(t *testing.T) {
	idea := "Staff can adjust stock on a Product but it can never go negative."
	all := shopEntities()
	p := BuildLogicPrompt(idea, all[1], all) // Product
	if !strings.Contains(strings.ToLower(p), "adjust") {
		t.Error("Product prompt should carry a positive 'adjust stock' hint")
	}
	if !strings.Contains(p, "ENFORCE PRECONDITIONS") {
		t.Error("prompt should demand precondition guards")
	}
}

func TestLogicPrompt_EmptyHintWhenNoOps_v2324(t *testing.T) {
	idea := "A blog with posts and comments." // no action verbs for Tag
	all := []schema.Entity{{Name: "Tag", Fields: []schema.Field{{Name: "id", Type: "int"}, {Name: "label", Type: "string"}}}}
	p := BuildLogicPrompt(idea, all[0], all)
	if !strings.Contains(p, `{"operations": []}`) {
		t.Error("entity with no implied ops should be told empty list is correct")
	}
}
