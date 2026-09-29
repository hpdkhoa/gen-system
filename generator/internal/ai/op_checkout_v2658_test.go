// generator/internal/ai/op_checkout_v2658_test.go — v26.58.
//
// D070: opVerb kept "checkout" for Checkout, and OpMisplaced compared it with
// the phrase "check out an order", so it missed a Checkout op put on the wrong
// entity. The spellings "check out" and "checkout" are now one verb.
package ai

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func shopWithCustomer() []schema.Entity {
	return append(shopEntities(), schema.Entity{Name: "Customer", Fields: []schema.Field{{Name: "id", Type: "int"}, {Name: "email", Type: "string"}}})
}

func TestOpMisplaced_CheckoutSpellings(t *testing.T) {
	all := shopWithCustomer()
	for _, idea := range []string{"Customers can check out an order.", "Customers can checkout an order."} {
		for _, op := range []string{"Checkout", "CheckOut"} {
			if got := opVerb(op); got != "check out" {
				t.Errorf("opVerb(%q) = %q, want check out", op, got)
			}
			if !OpMisplaced(idea, "Product", op, all) {
				t.Errorf("idea %q: %s on Product should be flagged, the idea binds it to Order", idea, op)
			}
			if OpMisplaced(idea, "Order", op, all) {
				t.Errorf("idea %q: %s on Order must be kept", idea, op)
			}
		}
	}
}
