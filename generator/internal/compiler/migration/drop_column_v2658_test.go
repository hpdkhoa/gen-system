// D041: BuildMigration read only mod.Added and mod.Modified, so a removed
// field produced no DROP_COLUMN step and the column stayed in the database.
package migration

import (
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler/diff"
)

func TestBuildMigration_RemovedFieldDropsColumn(t *testing.T) {
	cs := diff.ChangeSet{Modified: []diff.EntityChange{{
		EntityName: "User",
		Removed:    []diff.Field{{Name: "age", Type: "int"}},
	}}}
	m := BuildMigration(cs)
	if len(m.Steps) != 1 {
		t.Fatalf("want one DROP_COLUMN step, got %+v", m.Steps)
	}
	st := m.Steps[0]
	if st.Type != "DROP_COLUMN" || st.Entity != "User" || st.Column != "age" || st.ColumnType != "int" {
		t.Errorf("got step %+v, want DROP_COLUMN User.age int", st)
	}
	up := GenerateSQL(m)
	down := GenerateRollbackSQL(m)
	if len(up) != 1 || !strings.Contains(up[0], "DROP COLUMN IF EXISTS") {
		t.Errorf("up SQL %q, want one DROP COLUMN", up)
	}
	if len(down) != 1 || !strings.Contains(down[0], "ADD COLUMN") || !strings.Contains(down[0], "INTEGER") {
		t.Errorf("down SQL %q, want the column added back as INTEGER", down)
	}
}
