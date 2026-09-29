// generate_v2658_test.go: D032. GenerateForm and GenerateList dropped the
// os.WriteFile result and never created the frontend folder, so on a fresh
// output folder they wrote nothing and reported nothing, and the
// generate_frontend task counted as done.
package frontend

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestGenerateFormList_CreateFrontendFolder(t *testing.T) {
	root := t.TempDir()
	e := schema.Entity{Name: "BlogPost", Fields: []schema.Field{{Name: "title", Type: "string"}}}
	if err := GenerateForm(e, root); err != nil {
		t.Fatalf("GenerateForm: %v", err)
	}
	if err := GenerateList(e, root); err != nil {
		t.Fatalf("GenerateList: %v", err)
	}
	for _, f := range []string{"blogpostForm.jsx", "blogpostList.jsx"} {
		if _, err := os.Stat(filepath.Join(root, "frontend", f)); err != nil {
			t.Errorf("%s was not written on a root with no frontend folder: %v", f, err)
		}
	}
}

func TestGenerateFormList_ReportWriteError(t *testing.T) {
	root := t.TempDir()
	// A FILE named frontend blocks the folder, so both writes must fail loudly.
	if err := os.WriteFile(filepath.Join(root, "frontend"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := schema.Entity{Name: "BlogPost"}
	if err := GenerateForm(e, root); err == nil {
		t.Error("GenerateForm returned nil although the form could not be written")
	}
	if err := GenerateList(e, root); err == nil {
		t.Error("GenerateList returned nil although the list could not be written")
	}
}
