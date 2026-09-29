// healer_v2658_test.go — v26.58.
//
// D074: on a fresh run the context loads before any task writes a file, so
// ctx.Files has no entry for the target. FixTask then sent no existing code in
// the repair prompt, and the revert after a failed compile wrote an empty body.
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

func TestFixTask_FreshRunSendsTheFileAndRestoresIt(t *testing.T) {
	root := t.TempDir()
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
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Prompt string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		prompts = append(prompts, req.Prompt)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"response": "x := notDefinedAnywhere\n\treturn x, nil", "done": true})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("OLLAMA_STREAM", "")

	// An empty context stands for the load that ran before the file existed.
	if err := FixTask(healTask(checkoutErr), agentctx.Context{}, root); err == nil {
		t.Fatal("FixTask reported success although no attempt compiled")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) == 0 {
		t.Fatal("no repair prompt reached the model")
	}
	if !strings.Contains(prompts[0], "return s.n + id, nil") {
		t.Errorf("the repair prompt holds no existing code:\n%s", prompts[0])
	}
	got, _ := os.ReadFile(file)
	if string(got) != healServiceSrc {
		t.Fatalf("the old body was not restored:\n%s", got)
	}
}
