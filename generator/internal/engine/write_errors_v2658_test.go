// generator/internal/engine/write_errors_v2658_test.go — v26.58 (D029):
// writeFileMk, emitGoMod and cleanStaleBackend dropped every file system error
// and the exported generators returned nothing, so a folder that could not be
// written left no message and the generate task counted as a success. The
// generators now return the error.
package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ai"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

var writeErrEntity = schema.Entity{Name: "Order", Fields: []schema.Field{{Name: "Total", Type: "int"}}}

// A regular file named backend in the output folder: nothing below backend/
// can be written.
func TestGenerators_ReportBackendWriteFailure(t *testing.T) {
	t.Setenv("GEN_BACKEND", "postgres")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "backend"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := writeErrEntity
	checks := map[string]error{
		"GenerateModel":        GenerateModel(e, root),
		"GenerateRepo":         GenerateRepo(e, root),
		"GenerateValidation":   GenerateValidation(e, root),
		"GenerateHandler":      GenerateHandler(e, root),
		"GenerateServiceLogic": GenerateServiceLogic("an orders app", e, []schema.Entity{e}, ai.ReplayProvider(), root),
		"GenerateBackendApp":   GenerateBackendApp([]schema.Entity{e}, root),
	}
	for name, err := range checks {
		if err == nil {
			t.Errorf("%s: want an error when backend/ is a regular file, got nil", name)
		}
	}
}

// go.mod is a folder: emitGoMod cannot write it.
func TestEmitGoMod_ReportsWriteFailure(t *testing.T) {
	t.Setenv("GEN_BACKEND", "")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "go.mod", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := emitGoMod(root); err == nil {
		t.Error("emitGoMod: want an error when go.mod is a folder, got nil")
	}
	if err := GenerateModel(writeErrEntity, root); err == nil {
		t.Error("GenerateModel: want an error when go.mod is a folder, got nil")
	}
}

// backend/go.mod is a folder that is not empty: cleanStaleBackend cannot
// remove it. A stale file that is simply absent is not an error.
func TestCleanStaleBackend_ReportsRemoveFailure(t *testing.T) {
	t.Setenv("GEN_BACKEND", "")
	clean := t.TempDir()
	if err := cleanStaleBackend(clean); err != nil {
		t.Fatalf("cleanStaleBackend on an empty folder: want nil, got %v", err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "backend", "go.mod", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cleanStaleBackend(root); err == nil {
		t.Error("cleanStaleBackend: want an error when backend/go.mod cannot be removed, got nil")
	}
	if err := GenerateBackendApp([]schema.Entity{writeErrEntity}, root); err == nil {
		t.Error("GenerateBackendApp: want the cleanStaleBackend error, got nil")
	}
}

// The normal path still returns nil.
func TestGenerators_NoErrorOnWritableFolder(t *testing.T) {
	t.Setenv("GEN_BACKEND", "")
	root := t.TempDir()
	generateBackend(t, []schema.Entity{writeErrEntity}, root)
	if err := GenerateServiceLogic("an orders app", writeErrEntity, []schema.Entity{writeErrEntity}, ai.ReplayProvider(), root); err != nil {
		t.Fatalf("GenerateServiceLogic: %v", err)
	}
}
