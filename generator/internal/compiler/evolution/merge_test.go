package evolution_test

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler/evolution"
)

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

func fields(pairs ...string) []evolution.Field {
	out := make([]evolution.Field, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, evolution.Field{Name: pairs[i], Type: pairs[i+1]})
	}
	return out
}

func findEntity(schema evolution.Schema, name string) (evolution.Entity, bool) {
	for _, e := range schema.Entities {
		if e.Name == name {
			return e, true
		}
	}
	return evolution.Entity{}, false
}

func findField(e evolution.Entity, name string) (evolution.Field, bool) {
	for _, f := range e.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return evolution.Field{}, false
}

// ─────────────────────────────────────────────
// Merge (additive / safe evolution) tests
// ─────────────────────────────────────────────

// TestMerge_NewEntityAdded verifies that a brand-new entity in the new IR
// is appended to the result.
func TestMerge_NewEntityAdded(t *testing.T) {
	old := evolution.Schema{
		Entities: []evolution.Entity{
			{Name: "User", Fields: fields("id", "uuid", "email", "string")},
		},
	}
	newIR := evolution.Schema{
		Entities: []evolution.Entity{
			{Name: "Post", Fields: fields("id", "uuid", "title", "string")},
		},
	}

	result := evolution.Merge(old, newIR)

	if len(result.Entities) != 2 {
		t.Fatalf("want 2 entities, got %d", len(result.Entities))
	}
	if _, ok := findEntity(result, "User"); !ok {
		t.Error("User should be preserved")
	}
	if _, ok := findEntity(result, "Post"); !ok {
		t.Error("Post should be added")
	}
}

// TestMerge_ExistingEntityFieldsUpserted verifies field-level upsert:
// old fields are preserved, new fields are added, changed types win.
func TestMerge_ExistingEntityFieldsUpserted(t *testing.T) {
	old := evolution.Schema{
		Entities: []evolution.Entity{
			{Name: "User", Fields: fields("id", "uuid", "name", "string", "age", "int")},
		},
	}
	newIR := evolution.Schema{
		Entities: []evolution.Entity{
			// adds "email", updates "age" type, leaves "name" and "id" untouched
			{Name: "User", Fields: fields("age", "string", "email", "string")},
		},
	}

	result := evolution.Merge(old, newIR)

	user, ok := findEntity(result, "User")
	if !ok {
		t.Fatal("User entity missing")
	}

	// id and name preserved
	if _, ok := findField(user, "id"); !ok {
		t.Error("id field should be preserved")
	}
	if _, ok := findField(user, "name"); !ok {
		t.Error("name field should be preserved")
	}

	// email added
	if _, ok := findField(user, "email"); !ok {
		t.Error("email field should be added")
	}

	// age type updated to string
	age, ok := findField(user, "age")
	if !ok {
		t.Fatal("age field missing")
	}
	if age.Type != "string" {
		t.Errorf("age type: want string, got %s", age.Type)
	}
}

// TestMerge_NonDestructive_FieldNotInNewIRIsKept is the "Safe Evolution" guarantee:
// if a field doesn't appear in the new IR, it must not be deleted.
func TestMerge_NonDestructive_FieldNotInNewIRIsKept(t *testing.T) {
	old := evolution.Schema{
		Entities: []evolution.Entity{
			{Name: "User", Fields: fields("id", "uuid", "legacy_field", "string")},
		},
	}
	newIR := evolution.Schema{
		Entities: []evolution.Entity{
			{Name: "User", Fields: fields("email", "string")}, // no legacy_field
		},
	}

	result := evolution.Merge(old, newIR)

	user, _ := findEntity(result, "User")
	if _, ok := findField(user, "legacy_field"); !ok {
		t.Error("safe evolution: legacy_field must be preserved even if not in new IR")
	}
}

// TestMerge_RelationDedup verifies that duplicate relations are not added twice.
func TestMerge_RelationDedup(t *testing.T) {
	rel := evolution.Relation{FromEntity: "Post", ToEntity: "User", FromField: "user_id"}
	old := evolution.Schema{Relations: []evolution.Relation{rel}}
	newIR := evolution.Schema{Relations: []evolution.Relation{rel}} // same relation

	result := evolution.Merge(old, newIR)

	if len(result.Relations) != 1 {
		t.Errorf("want 1 relation (no dedup), got %d", len(result.Relations))
	}
}

// TestMerge_NewRelationAdded verifies that a genuinely new relation is appended.
func TestMerge_NewRelationAdded(t *testing.T) {
	old := evolution.Schema{
		Relations: []evolution.Relation{
			{FromEntity: "Post", ToEntity: "User", FromField: "user_id"},
		},
	}
	newIR := evolution.Schema{
		Relations: []evolution.Relation{
			{FromEntity: "Comment", ToEntity: "Post", FromField: "post_id"},
		},
	}

	result := evolution.Merge(old, newIR)

	if len(result.Relations) != 2 {
		t.Errorf("want 2 relations, got %d", len(result.Relations))
	}
}

// TestMerge_EmptyOld verifies merge from a blank slate.
func TestMerge_EmptyOld(t *testing.T) {
	old := evolution.Schema{}
	newIR := evolution.Schema{
		Entities: []evolution.Entity{
			{Name: "User", Fields: fields("id", "uuid")},
		},
	}

	result := evolution.Merge(old, newIR)

	if len(result.Entities) != 1 {
		t.Fatalf("want 1 entity, got %d", len(result.Entities))
	}
}

// TestMerge_EmptyNew verifies that a merge with an empty new IR is a no-op.
func TestMerge_EmptyNew(t *testing.T) {
	old := evolution.Schema{
		Entities: []evolution.Entity{
			{Name: "User", Fields: fields("id", "uuid")},
		},
	}

	result := evolution.Merge(old, evolution.Schema{})

	if len(result.Entities) != 1 {
		t.Fatalf("want 1 entity preserved, got %d", len(result.Entities))
	}
}

// ─────────────────────────────────────────────
// MergeWithDiff (deletion support) tests
// ─────────────────────────────────────────────

// TestMergeWithDiff_RemoveField verifies that an explicitly signaled
// field deletion is applied.
func TestMergeWithDiff_RemoveField(t *testing.T) {
	old := evolution.Schema{
		Entities: []evolution.Entity{
			{Name: "User", Fields: fields("id", "uuid", "age", "int", "email", "string")},
		},
	}
	newIR := evolution.Schema{} // no change to entities

	intent := evolution.DiffIntent{
		RemoveFields: map[string]map[string]bool{
			"User": {"age": true},
		},
	}

	result := evolution.MergeWithDiff(old, newIR, intent)

	user, ok := findEntity(result, "User")
	if !ok {
		t.Fatal("User entity missing")
	}
	if _, ok := findField(user, "age"); ok {
		t.Error("age field should have been removed by DiffIntent")
	}
	if _, ok := findField(user, "email"); !ok {
		t.Error("email field should be preserved")
	}
	if _, ok := findField(user, "id"); !ok {
		t.Error("id field should be preserved")
	}
}

// TestMergeWithDiff_RemoveEntity verifies that an explicitly signaled
// entity deletion is applied.
func TestMergeWithDiff_RemoveEntity(t *testing.T) {
	old := evolution.Schema{
		Entities: []evolution.Entity{
			{Name: "User", Fields: fields("id", "uuid")},
			{Name: "OldTable", Fields: fields("id", "uuid")},
		},
	}

	intent := evolution.DiffIntent{
		RemoveEntities: map[string]bool{"OldTable": true},
	}

	result := evolution.MergeWithDiff(old, evolution.Schema{}, intent)

	if _, ok := findEntity(result, "OldTable"); ok {
		t.Error("OldTable should have been removed")
	}
	if _, ok := findEntity(result, "User"); !ok {
		t.Error("User should be preserved")
	}
}

// TestMergeWithDiff_RemoveRelation verifies relation deletion.
func TestMergeWithDiff_RemoveRelation(t *testing.T) {
	rel1 := evolution.Relation{FromEntity: "Post", ToEntity: "User", FromField: "user_id"}
	rel2 := evolution.Relation{FromEntity: "Comment", ToEntity: "Post", FromField: "post_id"}

	old := evolution.Schema{Relations: []evolution.Relation{rel1, rel2}}

	intent := evolution.DiffIntent{
		RemoveRelations: map[string]bool{
			evolution.RelationKey(rel1): true,
		},
	}

	result := evolution.MergeWithDiff(old, evolution.Schema{}, intent)

	if len(result.Relations) != 1 {
		t.Fatalf("want 1 relation, got %d", len(result.Relations))
	}
	if result.Relations[0].FromEntity != "Comment" {
		t.Error("wrong relation kept")
	}
}

// TestMergeWithDiff_EmptyIntent_BehavesLikeMerge verifies that an empty
// DiffIntent doesn't change the additive-merge behaviour.
func TestMergeWithDiff_EmptyIntent_BehavesLikeMerge(t *testing.T) {
	old := evolution.Schema{
		Entities: []evolution.Entity{
			{Name: "User", Fields: fields("id", "uuid", "name", "string")},
		},
	}
	newIR := evolution.Schema{
		Entities: []evolution.Entity{
			{Name: "Post", Fields: fields("id", "uuid")},
		},
	}

	plain := evolution.Merge(old, newIR)
	withDiff := evolution.MergeWithDiff(old, newIR, evolution.DiffIntent{})

	if len(plain.Entities) != len(withDiff.Entities) {
		t.Errorf("empty intent should produce same result as plain Merge: got %d vs %d",
			len(withDiff.Entities), len(plain.Entities))
	}
}

// TestMerge_Idempotent verifies that merging a schema with itself twice
// doesn't produce duplicate entities or fields.
func TestMerge_Idempotent(t *testing.T) {
	schema := evolution.Schema{
		Entities: []evolution.Entity{
			{Name: "User", Fields: fields("id", "uuid", "name", "string")},
		},
	}

	once := evolution.Merge(schema, schema)
	twice := evolution.Merge(once, schema)

	if len(twice.Entities) != 1 {
		t.Errorf("idempotent merge: want 1 entity, got %d", len(twice.Entities))
	}
	user, _ := findEntity(twice, "User")
	if len(user.Fields) != 2 {
		t.Errorf("idempotent merge: want 2 fields, got %d", len(user.Fields))
	}
}
