// env_example_v2659_test.go (N08): two writers of .env.example.
// GenerateEnvFiles builds it from the schema and components; the ship scripts
// (writeShipScripts, and writeSpringShipScripts in Spring mode) wrote a fixed
// file over it. Whichever ran last won, so the schema based file was lost when
// the ship scripts ran after GenerateEnvFiles. GenerateEnvFiles now owns the
// file and the ship scripts write theirs only when none exists yet.
package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestEnvExample_SchemaBasedFileWinsInBothOrders(t *testing.T) {
	ents := []schema.Entity{{Name: "Order", Fields: []schema.Field{{Name: "total", Type: "float"}}}}
	components := []string{"api", "database", "auth"}

	for _, backend := range []string{"", "spring"} {
		for _, envFirst := range []bool{true, false} {
			name := "go"
			if backend != "" {
				name = backend
			}
			if envFirst {
				name += "/env first"
			} else {
				name += "/ship scripts first"
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv("GEN_BACKEND", backend)
				root := t.TempDir()
				if envFirst {
					GenerateEnvFiles(ents, root, components)
				}
				if err := GenerateBackendApp(ents, root); err != nil {
					t.Fatalf("GenerateBackendApp: %v", err)
				}
				if !envFirst {
					GenerateEnvFiles(ents, root, components)
				}
				b, err := os.ReadFile(filepath.Join(root, ".env.example"))
				if err != nil {
					t.Fatal(err)
				}
				got := string(b)
				for _, want := range []string{
					"DATABASE_NAME=order_db",
					"JWT_SECRET=",
					"\nPORT=" + schema.DefaultBackendPort + "\n",
				} {
					if !strings.Contains(got, want) {
						t.Errorf(".env.example lacks %q; the schema based file was lost:\n%s", want, got)
					}
				}
			})
		}
	}
}

// Without GenerateEnvFiles the ship scripts still leave an .env.example.
func TestEnvExample_ShipScriptsWriteOneWhenNoneExists(t *testing.T) {
	for _, backend := range []string{"", "spring"} {
		t.Run("backend="+backend, func(t *testing.T) {
			t.Setenv("GEN_BACKEND", backend)
			root := t.TempDir()
			ents := []schema.Entity{{Name: "Order", Fields: []schema.Field{{Name: "total", Type: "float"}}}}
			if err := GenerateBackendApp(ents, root); err != nil {
				t.Fatalf("GenerateBackendApp: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, ".env.example")); err != nil {
				t.Errorf("no .env.example after the ship scripts: %v", err)
			}
		})
	}
}
