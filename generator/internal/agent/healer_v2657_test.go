// healer_v2657_test.go — v26.57: FixTask on a real Go file with a fake model.
//
// D010: after a failed attempt FixTask "restored" the file by passing the
// WHOLE file text to RewriteFunction as a function body. parseBody rejected it,
// the error was dropped, and the failed AI body stayed in the file.
//
// D011: FixTask targeted GetAll<Entity>, a function no generated file defines.
// RewriteFunction then appended a new stub GetAllOrder with the AI body, and
// the function that failed stayed as it was.
package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/planner"
)

const healServiceSrc = `package service

type OrderService struct{ n int }

func NewOrderService() *OrderService { return &OrderService{} }

// Checkout returns the order total.
func (s *OrderService) Checkout(id int) (int, error) {
	return s.n + id, nil
}
`

// healFixture builds a one-module project whose service file the healer can
// build, and a fake model that always answers with body.
func healFixture(t *testing.T, body string) (root, file string, calls *int) {
	t.Helper()
	root = t.TempDir()
	t.Chdir(root) // the healer keeps its memory in ./.gen
	file = filepath.Join(root, "backend", "internal", "service", "order_service.go")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module app\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(healServiceSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		_ = json.NewEncoder(w).Encode(map[string]any{"response": body, "done": true})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("OLLAMA_STREAM", "")
	return root, file, &n
}

func healTask(errText string) planner.Task {
	return planner.Task{ID: "t1", Type: "generate_service", EntityName: "Order", Error: errText}
}

// The compiler names line 9, which lies inside Checkout.
const checkoutErr = "backend/internal/service/order_service.go:9:2: undefined: price"

func TestFixTask_FailedAttemptsRestoreTheFile(t *testing.T) {
	root, file, _ := healFixture(t, "x := notDefinedAnywhere\n\treturn x, nil")
	err := FixTask(healTask(checkoutErr), agentctx.Context{}, root)
	if err == nil {
		t.Fatal("FixTask reported success although no attempt compiled")
	}
	got, _ := os.ReadFile(file)
	if string(got) != healServiceSrc {
		t.Fatalf("file not restored after the failed attempts:\n%s", got)
	}
}

func TestFixTask_RepairsTheFunctionTheErrorNames(t *testing.T) {
	root, file, _ := healFixture(t, "total := s.n + id\n\treturn total * 2, nil")
	if err := FixTask(healTask(checkoutErr), agentctx.Context{}, root); err != nil {
		t.Fatalf("FixTask: %v", err)
	}
	got, _ := os.ReadFile(file)
	src := string(got)
	if strings.Contains(src, "GetAllOrder") {
		t.Errorf("healer added a function the generated file never had:\n%s", src)
	}
	// RewriteFunctionBody prints the new body with positions from its own file
	// set, so line breaks inside the statement can move; compare without spaces.
	squash := strings.Join(strings.Fields(src), "")
	if !strings.Contains(squash, "returntotal*2,nil") {
		t.Errorf("Checkout did not receive the repaired body:\n%s", src)
	}
}

// A failing contract test names the operation, not a line of the service file.
func TestFixTask_NamesTheFunctionFromATestFailure(t *testing.T) {
	root, file, _ := healFixture(t, "total := s.n + id\n\treturn total * 2, nil")
	errText := "--- FAIL: TestOrderService_OperationsHonorContract\n" +
		"    order_service_test.go:40: Checkout panicked: runtime error: index out of range"
	if err := FixTask(healTask(errText), agentctx.Context{}, root); err != nil {
		t.Fatalf("FixTask: %v", err)
	}
	got, _ := os.ReadFile(file)
	if !strings.Contains(strings.Join(strings.Fields(string(got)), ""), "returntotal*2,nil") {
		t.Errorf("Checkout did not receive the repaired body:\n%s", got)
	}
}

func TestRepairEvidence_KeepsTheLinesOfTheFile(t *testing.T) {
	out := "# app/backend/internal/service\n" +
		"backend/internal/service/order_service.go:9:2: undefined: price\n" +
		"backend/internal/service/user_service.go:4:1: syntax error\n"
	got := repairEvidence(out, "backend/internal/service/order_service.go")
	if !strings.Contains(got, "order_service.go:9:2") || strings.Contains(got, "user_service.go") {
		t.Fatalf("wrong evidence for order_service.go:\n%s", got)
	}
	// No line names the file: the tail of the output is the evidence.
	if got := repairEvidence("Checkout panicked", "backend/internal/service/order_service.go"); got != "Checkout panicked" {
		t.Fatalf("want the output tail, got %q", got)
	}
}

func TestFixTask_RefusesWhenNoFunctionIsNamed(t *testing.T) {
	root, file, calls := healFixture(t, "total := s.n + id\n\treturn total * 2, nil")
	err := FixTask(healTask("compilation error in order_service.go (round 1)"), agentctx.Context{}, root)
	if err == nil {
		t.Fatal("FixTask must refuse when it cannot tell which function failed")
	}
	if *calls != 0 {
		t.Errorf("model called %d times for a task with no target function", *calls)
	}
	got, _ := os.ReadFile(file)
	if string(got) != healServiceSrc {
		t.Fatalf("file changed although FixTask refused:\n%s", got)
	}
}
