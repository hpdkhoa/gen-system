// generator/internal/ai/op_binding_v2325_test.go, v23.25 (task B): the
// negative post-filter keeps correctly-bound ops and flags an op generated on
// the wrong entity. Mirrors the real orders/products scenario from the on-box
// run (Order: checkout, discount; Product: adjust-stock).
package ai

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func ordersProductsSchema() []schema.Entity {
	return []schema.Entity{
		{Name: "Order", Fields: []schema.Field{{Name: "Total", Type: "float"}, {Name: "Status", Type: "string"}}},
		{Name: "Product", Fields: []schema.Field{{Name: "Name", Type: "string"}, {Name: "Stock", Type: "int"}}},
	}
}

const ordersIdea = "An orders app. Customers can check out an order and apply a " +
	"discount to an unpaid order. Staff can adjust stock on a product up or down."

func TestOpMisplaced_KeepsCorrectlyBoundOps(t *testing.T) {
	all := ordersProductsSchema()
	// These are the ops the real run produced; none should be flagged.
	if OpMisplaced(ordersIdea, "Order", "Checkout", all) {
		t.Error("Checkout on Order should NOT be flagged misplaced")
	}
	if OpMisplaced(ordersIdea, "Order", "ApplyDiscount", all) {
		t.Error("ApplyDiscount on Order should NOT be flagged misplaced")
	}
	if OpMisplaced(ordersIdea, "Product", "AdjustStock", all) {
		t.Error("AdjustStock on Product should NOT be flagged misplaced")
	}
}

func TestOpMisplaced_FlagsWrongEntity(t *testing.T) {
	all := ordersProductsSchema()
	// AdjustStock clearly belongs to Product; on Order it is misplaced.
	if !OpMisplaced(ordersIdea, "Order", "AdjustStock", all) {
		t.Error("AdjustStock on Order SHOULD be flagged misplaced (binds to Product)")
	}
}

func TestOpMisplaced_AmbiguousKept(t *testing.T) {
	all := ordersProductsSchema()
	// A verb not present in the idea has no binding → never flagged (keep).
	if OpMisplaced(ordersIdea, "Order", "Archive", all) {
		t.Error("an unbound verb must not be flagged (conservative keep)")
	}
}
