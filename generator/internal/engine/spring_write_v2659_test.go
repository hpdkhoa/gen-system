// spring_write_v2659_test.go — N07: in Spring mode GenerateBackendApp called
// GenerateSpringApp, writeSpringShipScripts and GenerateSpringDockerfile, which
// dropped every writeFileMk error, and then returned nil. A file that could not
// be written was missing and the task still counted as a success.
package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestGenerateBackendApp_SpringReportsWriteFailure(t *testing.T) {
	t.Setenv("GEN_BACKEND", "spring")
	e := schema.Entity{Name: "Order", Fields: []schema.Field{{Name: "Total", Type: "int"}}}

	cases := []struct {
		name  string
		block func(root string) error
	}{
		// A regular file where the backend folder must go: the Spring project fails.
		{"backend is a file", func(root string) error {
			return os.WriteFile(filepath.Join(root, "backend"), []byte("x"), 0o644)
		}},
		// A regular file where the deploy folder must go: the ship scripts fail.
		{"deploy is a file", func(root string) error {
			return os.WriteFile(filepath.Join(root, "deploy"), []byte("x"), 0o644)
		}},
		// A folder where the Dockerfile must go: the Dockerfile fails.
		{"Dockerfile is a folder", func(root string) error {
			return os.MkdirAll(filepath.Join(root, "Dockerfile", "x"), 0o755)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if err := c.block(root); err != nil {
				t.Fatal(err)
			}
			if err := GenerateBackendApp([]schema.Entity{e}, root); err == nil {
				t.Errorf("GenerateBackendApp in Spring mode: want an error when %s, got nil", c.name)
			}
		})
	}

	// A clean folder still generates without an error.
	if err := GenerateBackendApp([]schema.Entity{e}, t.TempDir()); err != nil {
		t.Errorf("GenerateBackendApp in Spring mode on a clean folder: %v", err)
	}
}
