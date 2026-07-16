// generator/internal/planner_v2/entity_validation_v2325_test.go — v23.25
// (task A): the deterministic noun-trace keeps idea entities and flags the
// hallucinated ones. Pure (no model) — exercises partitionByAnchor/ideaTokens.
package planner_v2

import "testing"

func specsNamed(names ...string) []EntitySpec {
	out := make([]EntitySpec, 0, len(names))
	for _, n := range names {
		out = append(out, EntitySpec{Name: n, Fields: map[string]string{"id": "int"}})
	}
	return out
}

func TestPartitionByAnchor_OrdersIdea(t *testing.T) {
	idea := "An orders app. An Order has a total and a status. Customers can check " +
		"out an order and apply a discount. A Product has a name and stock."
	anchors := ideaTokens(idea, nil)

	// Order, Product, Customer all appear; Invoice never does.
	specs := specsNamed("Order", "Product", "Customer", "Invoice")
	kept, dropped := partitionByAnchor(specs, anchors)

	keptNames := map[string]bool{}
	for _, s := range kept {
		keptNames[s.Name] = true
	}
	for _, want := range []string{"Order", "Product", "Customer"} {
		if !keptNames[want] {
			t.Errorf("expected %q to be anchored/kept, but it was dropped", want)
		}
	}
	if len(dropped) != 1 || dropped[0].Name != "Invoice" {
		t.Errorf("expected only Invoice dropped, got %v", dropped)
	}
}

func TestEntityAnchored_PluralAndSubstring(t *testing.T) {
	anchors := ideaTokens("Manage users and their orders", nil)
	// "User" anchors on "users" (singular/plural); "OrderItem" anchors on "orders" (substring).
	if !entityAnchored("User", anchors) {
		t.Error("User should anchor on 'users'")
	}
	if !entityAnchored("OrderItem", anchors) {
		t.Error("OrderItem should anchor on 'orders' via substring")
	}
	if entityAnchored("Payment", anchors) {
		t.Error("Payment is not in the idea and should not anchor")
	}
}

func TestIdeaTokens_DropsStopWords(t *testing.T) {
	toks := ideaTokens("The app should add a new thing", nil)
	for _, stop := range []string{"the", "app", "should", "add", "new"} {
		if toks[stop] {
			t.Errorf("stop word %q should not be an anchor token", stop)
		}
	}
	if !toks["thing"] {
		t.Error("'thing' is a content word and should be a token")
	}
}
