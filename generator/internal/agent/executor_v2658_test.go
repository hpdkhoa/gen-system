// executor_v2658_test.go (v26.58).
//
// D045: the service security review built its path from the raw entity name,
// so "Order Item" gave "order item_service.go". The generator writes
// orderitem_service.go, ReviewCode could not read the wrong path, returned
// Safe, and the executor printed "clean" although no review ran.
package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestExecuteTask_ServiceReviewReadsTheGeneratedFile(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	var mu sync.Mutex
	var reviewed []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Prompt string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if strings.Contains(req.Prompt, "security reviewer") {
			mu.Lock()
			reviewed = append(reviewed, req.Prompt)
			mu.Unlock()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"response": `{"safe": true, "issues": []}`, "done": true})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("OLLAMA_STREAM", "")
	t.Setenv("GEN_LOGIC", "")

	e := schema.Entity{Name: "Order Item", Fields: []schema.Field{{Name: "qty", Type: "int"}}}
	SetLogicRun("an order system", []schema.Entity{e}, nil)
	task := planner.Task{ID: "svc", Type: "generate_service", EntityName: e.Name}
	if err := ExecuteTask(task, map[string]interface{}{e.Name: e}, root); err != nil {
		t.Fatalf("ExecuteTask: %v", err)
	}
	if _, err := os.Stat(targetFileFor(e.Name, "generate_service", root)); err != nil {
		t.Fatalf("the generated service file is missing: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reviewed) != 1 {
		t.Fatalf("the service review sent %d prompts to the model, want 1 (it read a path that does not exist)", len(reviewed))
	}
}
