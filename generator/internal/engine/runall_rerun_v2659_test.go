// runall_rerun_v2659_test.go (N06): RunAll decided a first run from an empty
// change set. Since D072 a rerun with an unchanged schema also gives an empty
// change set, so RunAll regenerated every entity on every rerun and the
// incremental skip never happened.
package engine

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// runAllOutputV2659 runs RunAll and returns what it printed to os.Stdout.
func runAllOutputV2659(t *testing.T, entities []schema.Entity, path string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() { os.Stdout = old }()
	RunAll(entities, path)
	w.Close()
	os.Stdout = old
	return <-done
}

func TestRunAll_RerunSkipsUnchangedEntities(t *testing.T) {
	t.Setenv("GEN_BACKEND", "")
	t.Chdir(t.TempDir())
	out := t.TempDir()
	order := schema.Entity{Name: "Order", Fields: []schema.Field{{Name: "total", Type: "float"}}}
	product := schema.Entity{Name: "Product", Fields: []schema.Field{{Name: "title", Type: "string"}}}

	first := runAllOutputV2659(t, []schema.Entity{order, product}, out)
	for _, name := range []string{"Order", "Product"} {
		if !strings.Contains(first, "Processing: "+name) {
			t.Errorf("first run must process %s; output:\n%s", name, first)
		}
	}

	second := runAllOutputV2659(t, []schema.Entity{order, product}, out)
	if strings.Contains(second, "Processing:") {
		t.Errorf("rerun with the same schema processed an entity; output:\n%s", second)
	}
	for _, name := range []string{"Order", "Product"} {
		if !strings.Contains(second, "Skipping unchanged entity: "+name) {
			t.Errorf("rerun must skip %s; output:\n%s", name, second)
		}
	}

	// A changed field regenerates only that entity.
	order2 := schema.Entity{Name: "Order", Fields: []schema.Field{
		{Name: "total", Type: "float"}, {Name: "note", Type: "string"}}}
	third := runAllOutputV2659(t, []schema.Entity{order2, product}, out)
	if !strings.Contains(third, "Processing: Order") {
		t.Errorf("a changed Order must be processed; output:\n%s", third)
	}
	if strings.Contains(third, "Processing: Product") {
		t.Errorf("an unchanged Product must be skipped; output:\n%s", third)
	}
}
