// generator/internal/engine/pgstore_nofields_v2658_test.go — v26.58 (D026):
// genStorePostgres emitted `INSERT INTO tags () VALUES ()` and
// `UPDATE tags SET  WHERE id=$1`, , id)` for an entity whose only field is id,
// so the generated Postgres store did not parse and the backend did not build.
package engine

import (
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// generateBackend runs the exported backend generators for every entity, the
// way the executor does on the main path, into root.
func generateBackend(t *testing.T, entities []schema.Entity, root string) {
	t.Helper()
	for _, e := range entities {
		if err := GenerateModel(e, root); err != nil {
			t.Fatalf("GenerateModel(%s): %v", e.Name, err)
		}
		if err := GenerateRepo(e, root); err != nil {
			t.Fatalf("GenerateRepo(%s): %v", e.Name, err)
		}
		if err := GenerateHandler(e, root); err != nil {
			t.Fatalf("GenerateHandler(%s): %v", e.Name, err)
		}
	}
	if err := GenerateBackendApp(entities, root); err != nil {
		t.Fatalf("GenerateBackendApp: %v", err)
	}
}

// vetGeneratedApp runs `go vet ./...` in the generated module, offline.
func vetGeneratedApp(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	cmd := exec.Command("go", "vet", "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go vet of the generated app failed: %v\n%s", err, out)
	}
}

func TestPGStore_EntityWithOnlyID_Compiles(t *testing.T) {
	t.Setenv("GEN_BACKEND", "postgres")
	root := t.TempDir()
	entities := []schema.Entity{
		{Name: "Tag", Fields: []schema.Field{{Name: "id", Type: "int"}}},
		{Name: "Label"}, // no field at all
	}
	generateBackend(t, entities, root)

	for _, f := range []string{"tag_pg.go", "label_pg.go"} {
		src, err := os.ReadFile(filepath.Join(root, "backend/internal/store", f))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := format.Source(src); err != nil {
			t.Fatalf("%s does not parse: %v\n%s", f, err, src)
		}
		if strings.Contains(string(src), "VALUES ()") {
			t.Errorf("%s still holds an empty column list:\n%s", f, src)
		}
		if !strings.Contains(string(src), "DEFAULT VALUES RETURNING id") {
			t.Errorf("%s: Create should insert DEFAULT VALUES:\n%s", f, src)
		}
	}
	vetGeneratedApp(t, root)
}
