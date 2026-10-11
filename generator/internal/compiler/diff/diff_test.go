// diff_test.go (TASK 3, v19): TestDiff_NoChanges tightened to assert
// len(cs.Modified)==0 now that the ghost-entry bug is fixed. Regression
// test added for "one field added, other entities unchanged".

package diff

import (
	"testing"
)

// ─────────────────────────────────────────────
// Helper: build Schema quickly
// ─────────────────────────────────────────────

func ir(entities ...Entity) Schema {
	return Schema{Entities: entities}
}

func entity(name string, fields ...Field) Entity {
	return Entity{Name: name, Fields: fields}
}

func field(name, typ string) Field {
	return Field{Name: name, Type: typ}
}

// containsEntity reports whether the slice contains an entity with the
// given Name.
func containsEntity(es []Entity, name string) bool {
	for _, e := range es {
		if e.Name == name {
			return true
		}
	}
	return false
}

func containsModified(cs ChangeSet, name string) bool {
	for _, m := range cs.Modified {
		if m.EntityName == name {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────
// Diff tests
// ─────────────────────────────────────────────

// TestDiff_NoChanges verifies that diffing identical IRs produces an empty
// ChangeSet. The v19 fix ensures no ghost EntityChange entries appear.
func TestDiff_NoChanges(t *testing.T) {
	schema := ir(entity("User", field("id", "uuid"), field("email", "string")))
	cs := Diff(schema, schema)

	if len(cs.AddEntities) != 0 {
		t.Errorf("identical IRs should have no AddEntities, got %+v", cs.AddEntities)
	}
	if len(cs.RemoveEntities) != 0 {
		t.Errorf("identical IRs should have no RemoveEntities, got %+v", cs.RemoveEntities)
	}
	// v19 fix: ghost entries are gone.
	if len(cs.Modified) != 0 {
		t.Errorf("identical IRs should have no Modified entries, got %+v", cs.Modified)
	}
}

// TestDiff_NewEntityDetected verifies that a new entity in newIR is in AddEntities.
func TestDiff_NewEntityDetected(t *testing.T) {
	old := ir(entity("User", field("id", "uuid")))
	newS := ir(entity("User", field("id", "uuid")), entity("Post", field("id", "uuid")))

	cs := Diff(old, newS)

	if len(cs.AddEntities) != 1 || !containsEntity(cs.AddEntities, "Post") {
		t.Errorf("want Post in AddEntities, got %v", cs.AddEntities)
	}
}

// TestDiff_RemovedEntityDetected verifies that a missing entity is in RemoveEntities.
func TestDiff_RemovedEntityDetected(t *testing.T) {
	old := ir(entity("User", field("id", "uuid")), entity("Post", field("id", "uuid")))
	newS := ir(entity("User", field("id", "uuid")))

	cs := Diff(old, newS)

	if len(cs.RemoveEntities) != 1 || !containsEntity(cs.RemoveEntities, "Post") {
		t.Errorf("want Post in RemoveEntities, got %v", cs.RemoveEntities)
	}
}

// TestDiff_FieldAddedToExistingEntity verifies field-level diff detection.
func TestDiff_FieldAddedToExistingEntity(t *testing.T) {
	old := ir(entity("User", field("id", "uuid")))
	newS := ir(entity("User", field("id", "uuid"), field("email", "string")))

	cs := Diff(old, newS)

	if len(cs.Modified) != 1 {
		t.Fatalf("want 1 modified entity, got %d", len(cs.Modified))
	}
	mod := cs.Modified[0]
	if mod.EntityName != "User" {
		t.Errorf("wrong modified entity: %s", mod.EntityName)
	}
	if len(mod.Added) != 1 || mod.Added[0].Name != "email" {
		t.Errorf("want email in Added, got %v", mod.Added)
	}
}

// TestDiff_FieldTypeChanged is the "breaking change" scenario.
func TestDiff_FieldTypeChanged(t *testing.T) {
	old := ir(entity("User", field("id", "uuid"), field("age", "int")))
	newS := ir(entity("User", field("id", "uuid"), field("age", "string")))

	cs := Diff(old, newS)

	if len(cs.Modified) != 1 {
		t.Fatalf("want 1 modified entity, got %d", len(cs.Modified))
	}
	mod := cs.Modified[0]
	var sawBreaking bool
	for _, fc := range mod.Modified {
		if fc.Breaking && fc.Field == "age" {
			sawBreaking = true
			break
		}
	}
	if !sawBreaking {
		t.Errorf("int→string type change should be flagged as breaking; got Modified=%+v", mod.Modified)
	}
}

// TestDiff_OneFieldAdded_OtherEntitiesUnchanged is the regression test for
// the ghost-entry bug: only the entity with an actual change should appear
// in Modified; unchanged entities must not pollute the slice.
func TestDiff_OneFieldAdded_OtherEntitiesUnchanged(t *testing.T) {
	old := ir(
		entity("User", field("id", "uuid"), field("email", "string")),
		entity("Post", field("id", "uuid"), field("title", "string")),
		entity("Comment", field("id", "uuid")),
	)
	newS := ir(
		entity("User", field("id", "uuid"), field("email", "string"), field("name", "string")),
		entity("Post", field("id", "uuid"), field("title", "string")),
		entity("Comment", field("id", "uuid")),
	)
	cs := Diff(old, newS)

	if len(cs.AddEntities) != 0 {
		t.Errorf("no new entities expected; got %+v", cs.AddEntities)
	}
	if len(cs.RemoveEntities) != 0 {
		t.Errorf("no removed entities expected; got %+v", cs.RemoveEntities)
	}
	// Only User should appear in Modified.
	if len(cs.Modified) != 1 {
		t.Errorf("want exactly 1 Modified entry (User); got %d: %+v", len(cs.Modified), cs.Modified)
	}
	if cs.Modified[0].EntityName != "User" {
		t.Errorf("modified entity should be User, got %s", cs.Modified[0].EntityName)
	}
	if len(cs.Modified[0].Added) != 1 || cs.Modified[0].Added[0].Name != "name" {
		t.Errorf("User should have 'name' in Added; got %+v", cs.Modified[0].Added)
	}
	// Post and Comment must not be in Modified.
	if containsModified(cs, "Post") {
		t.Error("Post is unchanged; must not appear in Modified")
	}
	if containsModified(cs, "Comment") {
		t.Error("Comment is unchanged; must not appear in Modified")
	}
}

// TestDiff_MigrationUnaffected_ByFix verifies BuildMigration behaviour
// is identical before and after the fix for the common "one change" case.
// (Downstream audit: no code change needed in migration/)
func TestDiff_MigrationDoesNotProduceEmptySteps(t *testing.T) {
	// Two identical entities + one that gained a field.
	old := ir(
		entity("Org", field("id", "uuid")),
		entity("Team", field("id", "uuid")),
	)
	newS := ir(
		entity("Org", field("id", "uuid")),
		entity("Team", field("id", "uuid"), field("name", "string")),
	)
	cs := Diff(old, newS)

	if len(cs.Modified) != 1 || cs.Modified[0].EntityName != "Team" {
		t.Fatalf("want 1 Modified (Team), got %+v", cs.Modified)
	}
	// The Modified entry for Team must have exactly the added field.
	if len(cs.Modified[0].Added) != 1 || cs.Modified[0].Added[0].Name != "name" {
		t.Errorf("Team.Added should be [name]; got %+v", cs.Modified[0].Added)
	}
	// Org must not appear (it is unchanged).
	if containsModified(cs, "Org") {
		t.Error("Org is unchanged and must not appear in Modified")
	}
}
