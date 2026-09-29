// port_v2658_test.go — v26.58 (follow up of D036): the generated .env, the
// generated backend/main.go and the mobile client must name one port.
// GenerateEnvFiles wrote PORT=28080 while main.go listened on the fixed
// address :8080 and read no PORT at all, so the .env value was a lie, and the
// mobile client (now on mobile.BackendPort) could not follow a changed port.
// v26.59 (N08): GenerateEnvFiles owns .env.example and the ship scripts no
// longer write over it, so the PORT line must be a real setting, not the
// commented line of the ship scripts.
package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/mobile"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestPort_EnvMainAndMobileAgree(t *testing.T) {
	root := t.TempDir()
	ents := []schema.Entity{{Name: "Order", Fields: []schema.Field{{Name: "total", Type: "float"}}}}
	GenerateEnvFiles(ents, root, nil)
	if err := GenerateBackendApp(ents, root); err != nil {
		t.Fatalf("GenerateBackendApp: %v", err)
	}
	for _, name := range []string{".env", ".env.example"} {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if m := regexp.MustCompile(`(?m)^PORT=(\d+)$`).FindStringSubmatch(string(b)); m == nil || m[1] != mobile.BackendPort {
			t.Errorf("%s: PORT line %v, want PORT=%s", name, m, mobile.BackendPort)
		}
	}
	main, err := os.ReadFile(filepath.Join(root, "backend", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(main)
	if !strings.Contains(src, `os.Getenv("PORT")`) {
		t.Errorf("main.go ignores PORT:\n%s", src)
	}
	if !strings.Contains(src, `"`+mobile.BackendPort+`"`) {
		t.Errorf("main.go default port is not %s:\n%s", mobile.BackendPort, src)
	}
}
