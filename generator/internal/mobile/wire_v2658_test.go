// wire_v2658_test.go: D035. The mobile screen must call the route the
// generated Go backend serves, and send the JSON keys the generated model
// reads. The tests run the Go engine and GenerateScreen on the
// same entity and compare their output files.
//
// D035: the screen built its path as strings.ToLower(name) + "s", so Address
// called /addresss and Order_Item called /order_items, while the backend
// served /address and /orderitems. Every request got 404. The form keys were
// the raw field names ("due date"), not the model's JSON keys.
package mobile_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/engine"
	"github.com/hpdkhoa/gen-system/generator/internal/mobile"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// screenFetchPaths returns the request paths of the fetch calls in a screen.
func screenFetchPaths(src string) []string {
	re := regexp.MustCompile(`fetch\((?:"https?://[^/"]+|API_BASE_URL \+ ")(/[^"]*)"`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		out = append(out, m[1])
	}
	return out
}

func TestScreen_CallsBackendRoute(t *testing.T) {
	for _, name := range []string{"Address", "Order_Item", "Order Item", "BlogPost", "Category"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			e := schema.Entity{Name: name, Fields: []schema.Field{{Name: "title", Type: "string"}}}
			engine.GenerateHandler(e, root)
			handlers, _ := filepath.Glob(filepath.Join(root, "backend", "internal", "handler", "*_handler.go"))
			var served []string
			re := regexp.MustCompile(`mux\.HandleFunc\("([^"]+)", h\.collection\)`)
			for _, h := range handlers {
				for _, m := range re.FindAllStringSubmatch(readFile(t, h), -1) {
					served = append(served, m[1])
				}
			}
			if len(served) != 1 {
				t.Fatalf("expected one collection route in the backend, got %v", served)
			}

			if err := mobile.GenerateScreen(e, root); err != nil {
				t.Fatal(err)
			}
			screens, _ := filepath.Glob(filepath.Join(root, "mobile", "*_screen.js"))
			if len(screens) != 1 {
				t.Fatalf("expected one screen, got %v", screens)
			}
			paths := screenFetchPaths(readFile(t, screens[0]))
			if len(paths) != 2 {
				t.Fatalf("expected two fetch calls in the screen, got %v", paths)
			}
			for _, p := range paths {
				if p != served[0] {
					t.Errorf("screen calls %s, backend serves %s", p, served[0])
				}
			}
		})
	}
}

func TestScreen_SendsModelJSONKeys(t *testing.T) {
	root := t.TempDir()
	e := schema.Entity{Name: "BlogPost", Fields: []schema.Field{
		{Name: "title", Type: "string"},
		{Name: "due date", Type: "string"},
		{Name: "createdAt", Type: "string"},
	}}
	engine.GenerateModel(e, root)
	model := readFile(t, filepath.Join(root, "backend", "internal", "model", schema.RouteName(e.Name)+".go"))
	modelKeys := map[string]bool{}
	for _, m := range regexp.MustCompile("`json:\"([^\"]+)\"`").FindAllStringSubmatch(model, -1) {
		modelKeys[m[1]] = true
	}

	if err := mobile.GenerateScreen(e, root); err != nil {
		t.Fatal(err)
	}
	screen := readFile(t, filepath.Join(root, "mobile", "blogpost_screen.js"))
	keys := regexp.MustCompile(`setForm\(\{\.\.\.form, "?([^:"]+)"?: v\}\)`).FindAllStringSubmatch(screen, -1)
	if len(keys) != len(e.Fields) {
		t.Fatalf("expected %d form keys, got %v", len(e.Fields), keys)
	}
	for _, k := range keys {
		if !modelKeys[k[1]] {
			t.Errorf("screen sends key %q, which the model does not read (model keys %v)", k[1], modelKeys)
		}
	}
}
