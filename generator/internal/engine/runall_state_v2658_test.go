// runall_state_v2658_test.go — v26.58 (follow up 7): RunAll must read and write
// its IR snapshot in the output folder it generates into. It used the working
// directory, so a stale snapshot there made RunAll treat entities as unchanged
// and skip them when it wrote into a fresh folder (bench/postgres_live.go does
// exactly that), and every run overwrote the caller's own snapshot.
package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestRunAll_SnapshotLivesInTheOutputFolder(t *testing.T) {
	t.Setenv("GEN_BACKEND", "")
	work := t.TempDir()
	t.Chdir(work)
	order := schema.Entity{Name: "Order", Fields: []schema.Field{{Name: "total", Type: "float"}}}
	product := schema.Entity{Name: "Product", Fields: []schema.Field{{Name: "title", Type: "string"}}}

	// A stale snapshot in the working directory that already knows Order.
	stale, err := compiler.Compile(schema.SchemaFile{Entities: []schema.Entity{order}})
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.SaveIR(stale); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(work, ".gen", "ir_snapshot.json"))

	out := t.TempDir()
	RunAll([]schema.Entity{order, product}, out)

	for _, f := range []string{"Order_handler.go", "Product_handler.go"} {
		if _, err := os.Stat(filepath.Join(out, "backend", "internal", "handler", f)); err != nil {
			t.Errorf("a fresh output folder must get every entity; %s missing: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, ".gen", "ir_snapshot.json")); err != nil {
		t.Errorf("snapshot not written into the output folder: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(work, ".gen", "ir_snapshot.json"))
	if string(after) != string(before) {
		t.Errorf("RunAll overwrote the snapshot in the working directory")
	}
}
