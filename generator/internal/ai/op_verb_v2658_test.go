// generator/internal/ai/op_verb_v2658_test.go — v26.58.
//
// D068: opVerb matched by prefix, so CheckStock became "check", ReOpen became
// "reject" and PaymentStatus became "pay". OpMisplaced then found "check"
// inside the Order phrase "check out an order" and dropped CheckStock from
// Product. opVerb now maps only a whole verb, and OpMisplaced compares the op
// verb with the verb each phrase was bound by.
package ai

import "testing"

func TestOpVerb_WholeVerbOnly(t *testing.T) {
	cases := map[string]string{
		"CheckStock":    "check",
		"ReOpen":        "reopen",
		"Reopen":        "reopen",
		"PaymentStatus": "payment",
		"C":             "c",
		"ApplyDiscount": "apply",
		"AdjustStock":   "adjust",
		"Pay":           "pay",
	}
	for op, want := range cases {
		if got := opVerb(op); got != want {
			t.Errorf("opVerb(%q) = %q, want %q", op, got, want)
		}
	}
}

func TestOpMisplaced_CheckStockKeptOnProduct(t *testing.T) {
	idea := "Customers can check out an Order. Staff can adjust stock on a Product."
	if OpMisplaced(idea, "Product", "CheckStock", shopEntities()) {
		t.Error("CheckStock on Product was dropped: the verb check is not check out")
	}
}
