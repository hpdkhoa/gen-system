// wire_v2658_test.go: D035 applied to the web form and list. GenerateForm,
// GenerateList and the marker components of EvolveForm and EvolveList built
// their request paths as strings.ToLower(name) + "s" and used the raw field
// names as JSON keys. For Address the form posted to /addresss while the
// backend served /address, and a field "due date" became the invalid key
// form.due date. They now take the path from schema.CollectionPath and the
// keys from schema.JSONKey, the rules the Go backend uses.
package frontend_test

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/engine"
	"github.com/hpdkhoa/gen-system/generator/internal/frontend"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func wireEntities() (schema.Entity, schema.Entity) {
	addr := schema.Entity{
		Name: "Address",
		Fields: []schema.Field{
			{Name: "street", Type: "string"},
			{Name: "due date", Type: "string"},
			{Name: "createdAt", Type: "string"},
		},
		Relations: []schema.Relation{{Type: "belongsTo", Target: "Order_Item", ForeignKey: "OrderItemID"}},
	}
	item := schema.Entity{Name: "Order_Item", Fields: []schema.Field{{Name: "qty", Type: "int"}}}
	return addr, item
}

func servedRoutes(t *testing.T, root string) map[string]bool {
	t.Helper()
	handlers, _ := filepath.Glob(filepath.Join(root, "backend", "internal", "handler", "*_handler.go"))
	served := map[string]bool{}
	re := regexp.MustCompile(`mux\.HandleFunc\("([^"]+)", h\.collection\)`)
	for _, h := range handlers {
		for _, m := range re.FindAllStringSubmatch(readFile(t, h), -1) {
			served[m[1]] = true
		}
	}
	return served
}

func modelKeys(t *testing.T, root string, e schema.Entity) map[string]bool {
	t.Helper()
	engine.GenerateModel(e, root)
	model := readFile(t, filepath.Join(root, "backend", "internal", "model", schema.RouteName(e.Name)+".go"))
	keys := map[string]bool{}
	for _, m := range regexp.MustCompile("`json:\"([^\"]+)\"`").FindAllStringSubmatch(model, -1) {
		keys[m[1]] = true
	}
	return keys
}

func TestWire_FormAndListCallBackendRoutes(t *testing.T) {
	root := t.TempDir()
	addr, item := wireEntities()
	engine.GenerateHandler(addr, root)
	engine.GenerateHandler(item, root)
	served := servedRoutes(t, root)
	if !served["/address"] || !served["/orderitems"] {
		t.Fatalf("backend routes changed, got %v", served)
	}

	if err := frontend.GenerateForm(addr, root); err != nil {
		t.Fatal(err)
	}
	if err := frontend.GenerateList(addr, root); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`fetch\("(/[^"]*)"`)
	for _, f := range []string{"addressForm.jsx", "addressList.jsx"} {
		src := readFile(t, filepath.Join(root, "frontend", f))
		calls := re.FindAllStringSubmatch(src, -1)
		if len(calls) == 0 {
			t.Fatalf("%s makes no fetch call:\n%s", f, src)
		}
		for _, c := range calls {
			if !served[c[1]] {
				t.Errorf("%s calls %s, which the backend does not serve (%v)", f, c[1], served)
			}
		}
	}
}

func TestWire_FormAndListUseModelJSONKeys(t *testing.T) {
	root := t.TempDir()
	addr, _ := wireEntities()
	keys := modelKeys(t, root, addr)

	if err := frontend.GenerateForm(addr, root); err != nil {
		t.Fatal(err)
	}
	if err := frontend.GenerateList(addr, root); err != nil {
		t.Fatal(err)
	}
	frontend.EvolveForm(addr, root)
	frontend.EvolveList(addr, root)

	// A key is either a bare identifier or a quoted string.
	formKeyRe := regexp.MustCompile(`setForm\(\{\.\.\.form, (?:([A-Za-z_][A-Za-z0-9_]*)|'([^']+)'): e\.target\.value\}\)`)
	listKeyRe := regexp.MustCompile(`\{item(?:\.([A-Za-z_][A-Za-z0-9_]*)|\['([^']+)'\])\}`)
	files := map[string]*regexp.Regexp{
		filepath.Join("frontend", "addressForm.jsx"):                      formKeyRe,
		filepath.Join("frontend", "addressList.jsx"):                      listKeyRe,
		filepath.Join("frontend", "src", "components", "AddressForm.jsx"): formKeyRe,
		filepath.Join("frontend", "src", "components", "AddressList.jsx"): listKeyRe,
	}
	for f, re := range files {
		src := readFile(t, filepath.Join(root, f))
		found := re.FindAllStringSubmatch(src, -1)
		if len(found) < len(addr.Fields) {
			t.Errorf("%s: %d valid keys for %d fields:\n%s", f, len(found), len(addr.Fields), src)
		}
		for _, m := range found {
			if k := m[1] + m[2]; !keys[k] {
				t.Errorf("%s uses key %q, which the model does not write (%v)", f, k, keys)
			}
		}
	}
}
