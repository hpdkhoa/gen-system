// contract_v2657_test.go (v26.57, D002, D004): the generated TypeScript client
// must call the routes the generated Go backend serves and read the JSON keys
// the generated model writes. The test runs both generators on one entity and
// compares their output files, so any future drift between the two fails here.
//
// Before v26.57 the client called /blogPosts while the backend served
// /blogposts (every call got 404), and the interface named user_id userId,
// createdAt createdAt and PasswordHash passwordHash, while the model wrote the
// keys user_id, createdat and passwordhash.
package frontend_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/engine"
	"github.com/hpdkhoa/gen-system/generator/internal/frontend"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func contractEntity() schema.Entity {
	return schema.Entity{
		Name: "BlogPost",
		Fields: []schema.Field{
			{Name: "title", Type: "string"},
			{Name: "user_id", Type: "int"},
			{Name: "createdAt", Type: "time"},
			{Name: "PasswordHash", Type: "string"},
			{Name: "due date", Type: "string"},
		},
		Relations: []schema.Relation{{Type: "belongsTo", Target: "Author"}},
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

func TestContract_ClientRoutesMatchBackendRoutes(t *testing.T) {
	root := t.TempDir()
	e := contractEntity()
	engine.GenerateModel(e, root)
	engine.GenerateRepo(e, root)
	engine.GenerateHandler(e, root)
	frontend.GenerateAPIClient([]schema.Entity{e}, root)

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

	client := readFile(t, filepath.Join(root, "frontend", "src", "api", "client.ts"))
	want := "request<BlogPost[]>('" + served[0] + "')"
	if !strings.Contains(client, want) {
		t.Errorf("client does not call the backend route %s; want %q in:\n%s", served[0], want, client)
	}
	if strings.Contains(client, "/blogPosts") {
		t.Errorf("client still calls /blogPosts, which the backend does not serve")
	}
}

func TestContract_InterfaceKeysMatchModelJSONKeys(t *testing.T) {
	root := t.TempDir()
	e := contractEntity()
	engine.GenerateModel(e, root)
	frontend.GenerateTypeScript(e, root)

	model := readFile(t, filepath.Join(root, "backend", "internal", "model", schema.RouteName(e.Name)+".go"))
	var modelKeys []string
	for _, m := range regexp.MustCompile("`json:\"([^\"]+)\"`").FindAllStringSubmatch(model, -1) {
		modelKeys = append(modelKeys, m[1])
	}

	ts := readFile(t, filepath.Join(root, "frontend", "src", "types", e.Name+".ts"))
	body := ts[strings.Index(ts, "export interface"):]
	body = body[:strings.Index(body, "}")]
	var tsKeys []string
	for _, m := range regexp.MustCompile(`(?m)^\s+([A-Za-z_][A-Za-z0-9_]*)\??:\s*(number|string|boolean|unknown);`).FindAllStringSubmatch(body, -1) {
		tsKeys = append(tsKeys, m[1])
	}

	sort.Strings(modelKeys)
	sort.Strings(tsKeys)
	if strings.Join(modelKeys, ",") != strings.Join(tsKeys, ",") {
		t.Errorf("interface keys differ from model JSON keys\nmodel: %v\nts:    %v\n%s", modelKeys, tsKeys, ts)
	}
}
