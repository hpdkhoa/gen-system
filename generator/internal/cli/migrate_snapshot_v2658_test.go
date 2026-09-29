// D038: RunMigrate dropped the LoadPreviousIR error, so a corrupt
// .gen/ir_snapshot.json counted as an empty snapshot. Every entity then looked
// new and the ADD COLUMN for a new field on an old entity was lost.
package cli

import (
	"os"
	"strings"
	"testing"
)

func TestRunMigrate_CorruptSnapshotStops(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DB_URL", "")
	if err := os.MkdirAll(".gen", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".gen/ir_snapshot.json", []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	schemaJSON := `{"entities":[{"name":"User","fields":[{"name":"email","type":"string"},{"name":"age","type":"int"}]}]}`
	if err := os.WriteFile("schema.json", []byte(schemaJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { RunMigrate([]string{"schema.json"}) })
	if !strings.Contains(out, "ir_snapshot.json") {
		t.Errorf("want an error that names the snapshot file, got %q", out)
	}
	if strings.Contains(out, "DB_URL") {
		t.Errorf("RunMigrate went on past the corrupt snapshot: %q", out)
	}
}
