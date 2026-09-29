// port_v2658_test.go: D036. The mobile screen called 10.0.2.2:28080, while the
// generated backend/main.go listens on :8080, so every request from the
// emulator failed. The test runs the Go engine and GenerateScreen and compares
// the port main.go listens on with the port the mobile files call.
package mobile_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/engine"
	"github.com/hpdkhoa/gen-system/generator/internal/mobile"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestScreen_CallsBackendPort(t *testing.T) {
	t.Setenv("GEN_BACKEND", "")
	root := t.TempDir()
	e := schema.Entity{Name: "BlogPost", Fields: []schema.Field{{Name: "title", Type: "string"}}}
	engine.GenerateBackendApp([]schema.Entity{e}, root)
	main := readFile(t, filepath.Join(root, "backend", "main.go"))
	// main.go reads PORT and falls back to a default port literal.
	m := regexp.MustCompile(`port = "(\d+)"`).FindStringSubmatch(main)
	if m == nil {
		t.Fatalf("no default port in backend/main.go:\n%s", main)
	}
	backendPort := m[1]

	if err := mobile.GenerateScreen(e, root); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(root, "mobile", "*.js"))
	var all strings.Builder
	for _, f := range files {
		all.WriteString(readFile(t, f))
	}
	urls := regexp.MustCompile(`"https?://[^/":]+:(\d+)`).FindAllStringSubmatch(all.String(), -1)
	if len(urls) == 0 {
		t.Fatalf("no backend URL with a port in the mobile files %v", files)
	}
	for _, u := range urls {
		if u[1] != backendPort {
			t.Errorf("mobile app calls port %s, backend/main.go listens on %s", u[1], backendPort)
		}
	}
}

// The screens read the backend address from one config value, and a
// regeneration keeps an address the user changed there.
func TestScreen_ReadsBaseURLFromConfig(t *testing.T) {
	root := t.TempDir()
	e := schema.Entity{Name: "BlogPost", Fields: []schema.Field{{Name: "title", Type: "string"}}}
	if err := mobile.GenerateScreen(e, root); err != nil {
		t.Fatal(err)
	}
	screen := readFile(t, filepath.Join(root, "mobile", "blogpost_screen.js"))
	if strings.Contains(screen, "http://") {
		t.Errorf("screen still holds a fixed backend address:\n%s", screen)
	}
	if !strings.Contains(screen, `import { API_BASE_URL } from "./config";`) {
		t.Errorf("screen does not import API_BASE_URL from ./config:\n%s", screen)
	}
	cfg := filepath.Join(root, "mobile", "config.js")
	if !strings.Contains(readFile(t, cfg), "export const API_BASE_URL") {
		t.Fatalf("config.js does not export API_BASE_URL")
	}

	custom := "export const API_BASE_URL = \"http://192.168.1.5:8080\";\n"
	if err := os.WriteFile(cfg, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mobile.GenerateScreen(e, root); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, cfg); got != custom {
		t.Errorf("regeneration overwrote the user's config.js:\n%s", got)
	}
}
