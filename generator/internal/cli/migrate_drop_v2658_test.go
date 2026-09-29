// D040 and D041 through gen migrate: a field removed from the schema must
// reach the v26.57 --force gate as DROP COLUMN. Before, Diff never filled
// Removed and gen migrate said "No migrations needed".
package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestRunMigrate_RemovedFieldHitsGate(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DB_URL", "")
	old, err := compiler.Compile(schema.SchemaFile{Entities: []schema.Entity{
		{Name: "User", Fields: []schema.Field{{Name: "email", Type: "string"}, {Name: "age", Type: "int"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.SaveIR(old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("schema.json", []byte(`{"entities":[{"name":"User","fields":[{"name":"email","type":"string"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { RunMigrate([]string{"schema.json"}) })
	if !strings.Contains(out, "DROP COLUMN User.age") || !strings.Contains(out, "--force") {
		t.Errorf("want the --force gate with DROP COLUMN User.age, got %q", out)
	}
	out = captureStdout(t, func() { RunMigrate([]string{"schema.json", "--force"}) })
	if strings.Contains(out, "No migrations needed") {
		t.Errorf("with --force the removed field must produce SQL, got %q", out)
	}
}
