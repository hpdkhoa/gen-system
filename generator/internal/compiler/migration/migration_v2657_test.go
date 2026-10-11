// migration_v2657_test.go (v26.57): the migration gate and the CREATE TABLE step.
//
// D005: HasBreakingChanges looked only at type changes, so a removed entity
// produced "DROP TABLE IF EXISTS Post;" without the --force gate, and the down
// SQL held only a comment: the data was gone.
//
// D007: the CREATE_TABLE step carried only the entity name, so a new entity got
// a table with the id column alone. gen migrate then saved the new IR, so no
// later run ever added the missing columns.
package migration

import (
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler/diff"
)

func TestHasBreakingChanges_RemovedEntity(t *testing.T) {
	cs := diff.ChangeSet{RemoveEntities: []diff.Entity{{Name: "Post"}}}
	if !HasBreakingChanges(cs) {
		t.Fatal("dropping a table must count as a breaking change")
	}
}

func TestHasBreakingChanges_RemovedField(t *testing.T) {
	cs := diff.ChangeSet{Modified: []diff.EntityChange{{
		EntityName: "Post",
		Removed:    []diff.Field{{Name: "title", Type: "string"}},
	}}}
	if !HasBreakingChanges(cs) {
		t.Fatal("dropping a column must count as a breaking change")
	}
}

func TestHasBreakingChanges_AdditiveIsSafe(t *testing.T) {
	cs := diff.ChangeSet{
		AddEntities: []diff.Entity{{Name: "Tag"}},
		Modified: []diff.EntityChange{{
			EntityName: "Post",
			Added:      []diff.Field{{Name: "views", Type: "int"}},
		}},
	}
	if HasBreakingChanges(cs) {
		t.Fatal("adding a table or a column is not breaking")
	}
}

func TestCreateTable_HasEveryField(t *testing.T) {
	cs := diff.ChangeSet{AddEntities: []diff.Entity{{
		Name: "Post",
		Fields: []diff.Field{
			{Name: "id", Type: "int"}, // the built-in key, not a second column
			{Name: "title", Type: "string"},
			{Name: "views", Type: "int"},
			{Name: "published", Type: "bool"},
		},
	}}}
	sql := GenerateSQL(BuildMigration(cs))
	// v26.58 (OPEN-TABLES): the table is posts, the name the generated app uses.
	want := "CREATE TABLE IF NOT EXISTS posts (id SERIAL PRIMARY KEY, title TEXT, views INTEGER, published BOOLEAN);"
	if len(sql) != 1 || sql[0] != want {
		t.Fatalf("got %q\nwant %q", sql, want)
	}
	down := GenerateRollbackSQL(BuildMigration(cs))
	if len(down) != 1 || !strings.HasPrefix(down[0], "DROP TABLE IF EXISTS posts;") {
		t.Fatalf("rollback of a new table must drop it, got %q", down)
	}
}
