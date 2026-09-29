// D039: RunMigrate wrote .gen/last_down.sql before the DB_URL check and before
// ApplySQL, so a dry run or a failed run replaced the down SQL of the migration
// that was really applied, and a later rollback ran the wrong statements.
package cli

import (
	"os"
	"testing"
)

func TestRunMigrate_DryRunKeepsLastDown(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DB_URL", "")
	if err := os.MkdirAll(".gen", 0o755); err != nil {
		t.Fatal(err)
	}
	const applied = "ALTER TABLE users DROP COLUMN email;\n"
	if err := os.WriteFile(".gen/last_down.sql", []byte(applied), 0o644); err != nil {
		t.Fatal(err)
	}
	schemaJSON := `{"entities":[{"name":"User","fields":[{"name":"email","type":"string"},{"name":"age","type":"int"}]}]}`
	if err := os.WriteFile("schema.json", []byte(schemaJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"schema.json", "--dry"}, {"schema.json"}} {
		captureStdout(t, func() { RunMigrate(args) })
		got, err := os.ReadFile(".gen/last_down.sql")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != applied {
			t.Errorf("RunMigrate(%q) without an applied migration replaced last_down.sql with %q", args, got)
		}
	}
}
