// OPEN-TABLES: GenerateSQL and GenerateRollbackSQL used the entity and field
// names as given, so gen migrate wrote table Post (Postgres folds it to post)
// while the generated app reads table posts. Both now use the shared rules of
// internal/schema/wire.go: table = Plural(RouteName(entity)), column =
// JSONKey(field).
package migration

import (
	"reflect"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler/diff"
)

func TestSQL_UsesAppTableAndColumnNames(t *testing.T) {
	cs := diff.ChangeSet{
		AddEntities: []diff.Entity{{Name: "BlogPost", Fields: []diff.Field{
			{Name: "id", Type: "int"}, {Name: "createdAt", Type: "time"}, {Name: "due date", Type: "string"},
		}}},
		RemoveEntities: []diff.Entity{{Name: "Tag"}},
		Modified: []diff.EntityChange{{
			EntityName: "Order Item",
			Added:      []diff.Field{{Name: "unitPrice", Type: "float"}},
			Removed:    []diff.Field{{Name: "old note", Type: "string"}},
			Modified:   []diff.FieldChange{{Field: "qty", OldType: "string", NewType: "int", Breaking: true}},
		}},
	}
	m := BuildMigration(cs)
	wantUp := []string{
		"CREATE TABLE IF NOT EXISTS blogposts (id SERIAL PRIMARY KEY, createdat TIMESTAMPTZ, due_date TEXT);",
		"DROP TABLE IF EXISTS tags;",
		"ALTER TABLE orderitems ADD COLUMN IF NOT EXISTS unitprice NUMERIC;",
		"ALTER TABLE orderitems ALTER COLUMN qty TYPE INTEGER;",
		"ALTER TABLE orderitems DROP COLUMN IF EXISTS old_note;",
	}
	if got := GenerateSQL(m); !reflect.DeepEqual(got, wantUp) {
		t.Errorf("up SQL\n got %q\nwant %q", got, wantUp)
	}
	wantDown := []string{
		"ALTER TABLE orderitems ADD COLUMN old_note TEXT; -- restored",
		"ALTER TABLE orderitems ALTER COLUMN qty TYPE TEXT; -- was int",
		"ALTER TABLE orderitems DROP COLUMN IF EXISTS unitprice;",
		"-- MANUAL RESTORE REQUIRED for table: tags",
		"DROP TABLE IF EXISTS blogposts;",
	}
	if got := GenerateRollbackSQL(m); !reflect.DeepEqual(got, wantDown) {
		t.Errorf("down SQL\n got %q\nwant %q", got, wantDown)
	}
}
