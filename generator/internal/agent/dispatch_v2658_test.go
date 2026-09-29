// dispatch_v2658_test.go — v26.58: the repair dispatch of the validation loop
// and of the verify stage.
//
// D080: dispatchRepairs sent the fixed text "compilation error in <file>
// (round N)" for every kind of failure, so the repair prompt held no real
// failure text and every failure of a file and round shared one memory key.
//
// D079: inferEntityFromPath did not know the store folder, so an error in
// internal/store/order.go got no repair in any round.
//
// D104: inferEntityFromPath looked for "/service/" and friends only, so a
// Windows path with backslashes gave no entity and the autofix repair of a
// verify finding never ran.
package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
)

// promptFixture builds the one-module project of healFixture and a fake model
// that records every prompt and answers with a body that compiles.
func promptFixture(t *testing.T) (root string, prompts func() []string) {
	t.Helper()
	root = t.TempDir()
	t.Chdir(root)
	file := filepath.Join(root, "backend", "internal", "service", "order_service.go")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module app\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(healServiceSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Prompt string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		got = append(got, req.Prompt)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"response": "total := s.n + id\n\treturn total * 2, nil", "done": true})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("OLLAMA_STREAM", "")
	return root, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

// A go test failure reaches the repair prompt as its own text.
func TestDispatchRepairs_PromptHoldsTheTestFailure(t *testing.T) {
	root, prompts := promptFixture(t)
	testOut := "--- FAIL: TestOrderService_OperationsHonorContract\n" +
		"    order_service_test.go:40: Checkout panicked: runtime error: index out of range\n"
	dispatchRepairs([]string{"backend/internal/service/order_service.go"}, testOut, agentctx.Context{}, root, 1)
	p := prompts()
	if len(p) == 0 {
		t.Fatal("no repair prompt reached the model")
	}
	if !strings.Contains(p[0], "Checkout panicked: runtime error: index out of range") {
		t.Errorf("the repair prompt holds no failure text:\n%s", p[0])
	}
}

func TestInferEntityFromPath_StoreFolder(t *testing.T) {
	for _, p := range []string{"internal/store/order.go", "backend/internal/store/order.go"} {
		ent, layer := inferEntityFromPath(p)
		if ent != "Order" || layer != "generate_repo" {
			t.Errorf("inferEntityFromPath(%q) = %q, %q, want Order, generate_repo", p, ent, layer)
		}
	}
}

func TestInferEntityFromPath_WindowsPaths(t *testing.T) {
	for _, p := range []string{
		`C:\proj\backend\internal\service\order_service.go`,
		`internal\service\order_service.go`,
	} {
		ent, layer := inferEntityFromPath(p)
		if ent != "Order" || layer != "generate_service" {
			t.Errorf("inferEntityFromPath(%q) = %q, %q, want Order, generate_service", p, ent, layer)
		}
	}
	// go build on Windows prints the relative path with backslashes.
	files := extractBrokenFiles("internal\\service\\order_service.go:9:2: undefined: price\n")
	if len(files) != 1 {
		t.Fatalf("extractBrokenFiles gave %q", files)
	}
	if ent, _ := inferEntityFromPath(files[0]); ent != "Order" {
		t.Errorf("no entity from the build output path %q", files[0])
	}
}

// An autofix repair of a verify finding carries the finding text to the model.
func TestDispatchRepairs_VerifyFindingOnWindowsPath(t *testing.T) {
	root, prompts := promptFixture(t)
	file := filepath.Join(root, "backend", "internal", "service", "order_service.go")
	evidence := file + ": effect OrderService.Checkout: read-style operation has a DB-WRITE effect\n"
	dispatchRepairs([]string{file}, evidence, agentctx.Context{}, root, 99)
	p := prompts()
	if len(p) == 0 {
		t.Fatal("no repair prompt reached the model")
	}
	if !strings.Contains(p[0], "read-style operation has a DB-WRITE effect") {
		t.Errorf("the repair prompt holds no finding text:\n%s", p[0])
	}
}
