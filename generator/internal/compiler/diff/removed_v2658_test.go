// D040: Diff walked only the fields of the new entity, so a field that the
// entity lost never reached change.Removed and gen migrate said no migration
// was needed while the column stayed in the database.
package diff

import "testing"

func TestDiff_RemovedFieldReported(t *testing.T) {
	cs := Diff(
		ir(entity("User", field("id", "int"), field("age", "int"), field("name", "string"))),
		ir(entity("User", field("id", "int"), field("name", "string"))),
	)
	if len(cs.Modified) != 1 {
		t.Fatalf("want one Modified entry for User, got %+v", cs.Modified)
	}
	m := cs.Modified[0]
	if m.EntityName != "User" || len(m.Removed) != 1 || m.Removed[0] != field("age", "int") {
		t.Errorf("want Removed=[age int] on User, got %+v", m)
	}
	if len(m.Added) != 0 || len(m.Modified) != 0 {
		t.Errorf("want no Added or Modified fields, got %+v", m)
	}
}
