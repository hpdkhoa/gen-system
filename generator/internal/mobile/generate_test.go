// generate_test.go (v26.57, D003): GenerateScreen must write the screen file
// even when the project has no mobile/ folder yet. In an agent build into a
// fresh folder nothing created mobile/ before the first generate_mobile task,
// the write failed without a message, and App.js still imported the missing
// screen, so the bundle could not resolve it.
package mobile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestGenerateScreen_CreatesMobileFolder(t *testing.T) {
	root := t.TempDir()
	e := schema.Entity{Name: "User", Fields: []schema.Field{{Name: "email", Type: "string"}}}
	if err := GenerateScreen(e, root); err != nil {
		t.Fatalf("GenerateScreen: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "mobile", "user_screen.js")); err != nil {
		t.Fatalf("screen file missing: %v", err)
	}
}

func TestGenerateScreen_ReportsWriteError(t *testing.T) {
	root := t.TempDir()
	// A FILE named mobile blocks the folder, so the write must fail loudly.
	if err := os.WriteFile(filepath.Join(root, "mobile"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := GenerateScreen(schema.Entity{Name: "User"}, root); err == nil {
		t.Fatal("GenerateScreen returned nil although the screen could not be written")
	}
}
