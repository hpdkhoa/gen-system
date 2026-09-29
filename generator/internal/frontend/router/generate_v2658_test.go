// generate_v2658_test.go: D034. GenerateRouter dropped the os.WriteFile result
// and never created the frontend folder, so on a root with no frontend folder
// it wrote no AppRouter.jsx and reported nothing, and the generate_router
// task counted as done.
package router

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestGenerateRouter_CreatesFrontendFolder(t *testing.T) {
	root := t.TempDir()
	if err := GenerateRouter([]schema.Entity{{Name: "BlogPost"}}, root); err != nil {
		t.Fatalf("GenerateRouter: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "frontend", "AppRouter.jsx")); err != nil {
		t.Fatalf("AppRouter.jsx was not written on a root with no frontend folder: %v", err)
	}
}

func TestGenerateRouter_ReportsWriteError(t *testing.T) {
	root := t.TempDir()
	// A FILE named frontend blocks the folder, so the write must fail loudly.
	if err := os.WriteFile(filepath.Join(root, "frontend"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := GenerateRouter([]schema.Entity{{Name: "BlogPost"}}, root); err == nil {
		t.Fatal("GenerateRouter returned nil although AppRouter.jsx could not be written")
	}
}
