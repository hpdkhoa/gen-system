// runall_snapshot_v2659_test.go — N11: RunAll dropped the error of LoadIRAt.
// A broken .gen/ir_snapshot.json read as a first run, so RunAll regenerated
// every entity of an existing app without a word about the broken file.
package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestRunAll_BrokenSnapshotStopsWithoutWriting(t *testing.T) {
	t.Setenv("GEN_BACKEND", "")
	t.Chdir(t.TempDir())
	out := t.TempDir()
	if err := os.MkdirAll(filepath.Join(out, ".gen"), 0o755); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(out, ".gen", "ir_snapshot.json")
	if err := os.WriteFile(snap, []byte(`{"entities": [`), 0o644); err != nil {
		t.Fatal(err)
	}
	entities := []schema.Entity{{Name: "Order", Fields: []schema.Field{{Name: "total", Type: "float"}}}}

	got := runAllOutputV2659(t, entities, out)

	if strings.Contains(got, "Processing") {
		t.Errorf("RunAll generated entities over a broken snapshot:\n%s", got)
	}
	if !strings.Contains(got, "ir_snapshot.json") {
		t.Errorf("RunAll did not name the broken snapshot:\n%s", got)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != ".gen" {
			t.Errorf("RunAll wrote %s although it stopped", e.Name())
		}
	}
	if data, _ := os.ReadFile(snap); string(data) != `{"entities": [` {
		t.Errorf("RunAll replaced the broken snapshot; the user loses the evidence: %q", data)
	}
}
