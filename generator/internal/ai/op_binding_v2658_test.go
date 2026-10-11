// generator/internal/ai/op_binding_v2658_test.go (v26.58).
//
// D069: bindOperationsToEntities found verbs inside longer words, so "payment
// reference" bound "pay" to Order and "relationship" bound "ship" to Product.
// A verb now matches only as a whole word.
package ai

import (
	"strings"
	"testing"
)

func TestBindOperations_VerbInsideWordNotMatched(t *testing.T) {
	idea := "Each Order stores a payment reference. A Product has a relationship to a supplier."
	b := bindOperationsToEntities(idea, shopEntities())
	if len(b) != 0 {
		t.Errorf("the idea states no operation, got bindings %v", b)
	}
	p := BuildLogicPrompt(idea, shopEntities()[0], shopEntities())
	if strings.Contains(p, "OPERATIONS THE IDEA IMPLIES FOR Order") {
		t.Error("Order prompt tells the model it SHOULD implement an operation the idea never states")
	}
}

func TestBindOperations_WholeVerbStillMatched(t *testing.T) {
	idea := "Staff can ship an order. Staff can adjust stock, then pay the supplier for the product."
	b := bindOperationsToEntities(idea, shopEntities())
	if got := strings.Join(b["Order"], "|"); !strings.Contains(got, "ship an order") {
		t.Errorf("Order should bind ship an order, got %v", b["Order"])
	}
	if got := strings.Join(b["Product"], "|"); !strings.Contains(got, "pay the supplier") {
		t.Errorf("Product should bind pay the supplier, got %v", b["Product"])
	}
}

func TestOpMisplaced_ShipOrderNotMisplacedByRelationship(t *testing.T) {
	idea := "A Product has a relationship to a supplier."
	if OpMisplaced(idea, "Order", "ShipOrder", shopEntities()) {
		t.Error("ShipOrder on Order flagged misplaced because of the word relationship")
	}
}
