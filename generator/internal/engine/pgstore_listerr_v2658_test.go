// generator/internal/engine/pgstore_listerr_v2658_test.go — v26.58 (D027): the
// generated PGStore.List returned the rows it had when s.db.Query failed and
// skipped rows that failed to scan, and the CRUD interface gave List no error
// result, so GET on the collection answered 200 with an empty or short list when
// the database was down. List now returns an error and the handler answers 500.
package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// failingListTest is a test for the generated handler package: a store whose
// List fails must make GET on the collection answer 500, not 200.
const failingListTest = `package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"app/backend/internal/model"
)

type downStore struct{}

func (downStore) Create(m model.Order) (model.Order, error)   { return m, nil }
func (downStore) List() ([]model.Order, error)                { return nil, errors.New("db down") }
func (downStore) Get(id int) (model.Order, bool)              { return model.Order{}, false }
func (downStore) Update(id int, m model.Order) (model.Order, bool) { return m, false }
func (downStore) Delete(id int) bool                          { return false }

func TestListErrorAnswers500(t *testing.T) {
	mux := http.NewServeMux()
	NewOrderHandlerWith(downStore{}).Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/orders", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("GET /orders with a failing store: want 500, got %d (%s)", w.Code, w.Body.String())
	}
}
`

func TestPGStore_ListReturnsErrors(t *testing.T) {
	t.Setenv("GEN_BACKEND", "postgres")
	root := t.TempDir()
	entities := []schema.Entity{
		{Name: "Order", Fields: []schema.Field{{Name: "Total", Type: "int"}}},
	}
	generateBackend(t, entities, root)

	pg, err := os.ReadFile(filepath.Join(root, "backend/internal/store/order_pg.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(pg)
	if !strings.Contains(src, "List() ([]model.Order, error)") {
		t.Errorf("PGStore.List should return an error:\n%s", src)
	}
	if !strings.Contains(src, "rows.Err()") {
		t.Errorf("PGStore.List should check rows.Err:\n%s", src)
	}
	if strings.Contains(src, "rows.Scan(&m.ID, &m.Total) == nil") {
		t.Errorf("PGStore.List still skips rows that fail to scan:\n%s", src)
	}

	if err := os.WriteFile(filepath.Join(root, "backend/internal/handler/listerr_test.go"), []byte(failingListTest), 0o644); err != nil {
		t.Fatal(err)
	}
	vetGeneratedApp(t, root)
	cmd := exec.Command("go", "test", "./backend/internal/handler/")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated handler test failed: %v\n%s", err, out)
	}
}
