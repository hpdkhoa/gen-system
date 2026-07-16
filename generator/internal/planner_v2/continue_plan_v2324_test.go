// generator/internal/planner_v2/continue_plan_v2324_test.go — v23.24 (task 4A):
// offline tests for the delta application logic. No model is involved — these
// exercise applyDelta directly with hand-built ContinueDelta values, which is
// the deterministic core that protects existing entities from being dropped and
// applies NEW / ADD_FIELD / NEW_OP / REMOVE correctly.
package planner_v2

import (
	"testing"

	"gen-system/generator/internal/ai"
	"gen-system/generator/internal/schema"
)

func existingShop() []schema.Entity {
	return []schema.Entity{
		{Name: "Order", Fields: []schema.Field{
			{Name: "id", Type: "int"},
			{Name: "total", Type: "float"},
			{Name: "status", Type: "string"},
		}},
		{Name: "Product", Fields: []schema.Field{
			{Name: "id", Type: "int"},
			{Name: "name", Type: "string"},
			{Name: "stock", Type: "int"},
		}},
	}
}

func entityByName(es []schema.Entity, name string) (schema.Entity, bool) {
	for _, e := range es {
		if e.Name == name {
			return e, true
		}
	}
	return schema.Entity{}, false
}

func hasFieldNamed(e schema.Entity, f string) bool {
	for _, fld := range e.Fields {
		if fld.Name == f {
			return true
		}
	}
	return false
}

// The headline guarantee: proposing only additions must NEVER drop an existing
// entity, and must add the new entity + new field + record the new op.
func TestApplyDelta_AdditiveNeverDropsExisting_v2324(t *testing.T) {
	delta := ai.ContinueDelta{Changes: []ai.ContinueChange{
		{Kind: ai.ChangeNew, Entity: "Customer", Fields: map[string]string{"name": "string", "email": "string"}},
		{Kind: ai.ChangeAddField, Entity: "Order", Field: "email", FieldType: "string"},
		{Kind: ai.ChangeNewOp, Entity: "Order", Op: "Refund"},
	}}

	got := applyDelta(existingShop(), delta, false)

	// Both original entities still present.
	if _, ok := entityByName(got.mergedEntities, "Order"); !ok {
		t.Fatal("Order was dropped — additive delta must preserve existing entities")
	}
	if _, ok := entityByName(got.mergedEntities, "Product"); !ok {
		t.Fatal("Product was dropped — additive delta must preserve existing entities")
	}

	// New entity present, with an auto id.
	cust, ok := entityByName(got.mergedEntities, "Customer")
	if !ok {
		t.Fatal("Customer (NEW) was not added")
	}
	if !hasFieldNamed(cust, "id") {
		t.Error("NEW entity must always carry an id field")
	}
	if !hasFieldNamed(cust, "email") {
		t.Error("Customer.email (from NEW fields) missing")
	}

	// New field added to existing entity.
	order, _ := entityByName(got.mergedEntities, "Order")
	if !hasFieldNamed(order, "email") {
		t.Error("Order.email (ADD_FIELD) was not applied")
	}
	// Original Order fields untouched.
	for _, f := range []string{"id", "total", "status"} {
		if !hasFieldNamed(order, f) {
			t.Errorf("Order lost its original field %q", f)
		}
	}

	// changedSet covers the touched entities (Order, Customer) but not Product.
	if !got.changedSet["Order"] || !got.changedSet["Customer"] {
		t.Errorf("changedSet should include Order and Customer, got %v", got.changedSet)
	}
	if got.changedSet["Product"] {
		t.Error("Product was untouched and must not be in changedSet")
	}

	// New op recorded.
	if ops := got.newOps["Order"]; len(ops) != 1 || ops[0] != "Refund" {
		t.Errorf("expected Order op Refund recorded, got %v", got.newOps["Order"])
	}
}

// A model that re-proposes an existing entity must not duplicate it or its
// fields — NEW on an existing entity degrades to a field merge.
func TestApplyDelta_NewOnExistingEntity_Dedups_v2324(t *testing.T) {
	delta := ai.ContinueDelta{Changes: []ai.ContinueChange{
		{Kind: ai.ChangeNew, Entity: "Order", Fields: map[string]string{"total": "float", "note": "string"}},
	}}
	got := applyDelta(existingShop(), delta, false)

	count := 0
	for _, e := range got.mergedEntities {
		if e.Name == "Order" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("Order duplicated %d times; NEW on existing must merge, not duplicate", count)
	}
	order, _ := entityByName(got.mergedEntities, "Order")
	// total existed → not duplicated; note is new → added.
	totals := 0
	for _, f := range order.Fields {
		if f.Name == "total" {
			totals++
		}
	}
	if totals != 1 {
		t.Errorf("total field duplicated (%d); existing field must not re-add", totals)
	}
	if !hasFieldNamed(order, "note") {
		t.Error("genuinely-new field note should have been added")
	}
}

// REMOVE is collected but NOT applied unless allowRemove is true.
func TestApplyDelta_RemoveGatedByFlag_v2324(t *testing.T) {
	delta := ai.ContinueDelta{Changes: []ai.ContinueChange{
		{Kind: ai.ChangeRemove, Entity: "Product"},
	}}

	// allowRemove == false → Product survives.
	got := applyDelta(existingShop(), delta, false)
	if _, ok := entityByName(got.mergedEntities, "Product"); !ok {
		t.Fatal("Product removed despite allowRemove=false — REMOVE must be gated")
	}

	// allowRemove == true → Product dropped, recorded.
	got2 := applyDelta(existingShop(), delta, true)
	if _, ok := entityByName(got2.mergedEntities, "Product"); ok {
		t.Fatal("Product not removed despite allowRemove=true")
	}
	if rec := got2.removed["Product"]; len(rec) != 1 || rec[0] != "*" {
		t.Errorf("expected entity-level removal recorded for Product, got %v", rec)
	}
}

// REMOVE of a single field (allowed) drops just that field.
func TestApplyDelta_RemoveField_v2324(t *testing.T) {
	delta := ai.ContinueDelta{Changes: []ai.ContinueChange{
		{Kind: ai.ChangeRemove, Entity: "Order", Field: "status"},
	}}
	got := applyDelta(existingShop(), delta, true)
	order, _ := entityByName(got.mergedEntities, "Order")
	if hasFieldNamed(order, "status") {
		t.Error("Order.status should have been removed")
	}
	if hasFieldNamed(order, "total") == false {
		t.Error("Order.total must survive a field-level remove of status")
	}
}

// Removing an entity must force every survivor into the changed set so the
// shared/global files (store interfaces, main wiring) regenerate without it.
func TestApplyDelta_RemoveForcesSurvivorRegen_v2324(t *testing.T) {
	delta := ai.ContinueDelta{Changes: []ai.ContinueChange{
		{Kind: ai.ChangeRemove, Entity: "Product"},
	}}
	got := applyDelta(existingShop(), delta, true) // allowRemove

	if _, ok := entityByName(got.mergedEntities, "Product"); ok {
		t.Fatal("Product should be removed")
	}
	// Order (the survivor) must be in changedSet so globals rebuild.
	if !got.changedSet["Order"] {
		t.Error("survivor Order must be forced into changedSet on entity removal")
	}
	// The removed entity must NOT linger in changedSet (it no longer exists).
	if got.changedSet["Product"] {
		t.Error("removed Product must not remain in changedSet")
	}
}

// Empty delta → no changes, no entities lost.
func TestApplyDelta_EmptyDelta_v2324(t *testing.T) {
	got := applyDelta(existingShop(), ai.ContinueDelta{}, false)
	if len(got.mergedEntities) != 2 {
		t.Errorf("empty delta must preserve all entities, got %d", len(got.mergedEntities))
	}
	if len(got.changedSet) != 0 {
		t.Errorf("empty delta must touch nothing, changedSet=%v", got.changedSet)
	}
}
