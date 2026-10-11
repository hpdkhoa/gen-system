// generator/internal/engine/web_project_v2658_test.go, v26.58 (D030):
// GenerateWebProject returned without a message when it could not create
// frontend/, and dropped the errors of every later write. It now returns them.
package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestGenerateWebProject_ReportsWriteFailure(t *testing.T) {
	entities := []schema.Entity{{Name: "Order", Fields: []schema.Field{{Name: "Total", Type: "int"}}}}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "frontend"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := GenerateWebProject(entities, root); err == nil {
		t.Error("want an error when frontend is a regular file, got nil")
	}

	// A folder where package.json should be: MkdirAll works, the write fails.
	root2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root2, "frontend", "package.json", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := GenerateWebProject(entities, root2); err == nil {
		t.Error("want an error when package.json cannot be written, got nil")
	}

	ok := t.TempDir()
	if err := GenerateWebProject(entities, ok); err != nil {
		t.Fatalf("writable folder: want nil, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(ok, "frontend", "src", "App.tsx")); err != nil {
		t.Fatalf("App.tsx not written: %v", err)
	}
}
