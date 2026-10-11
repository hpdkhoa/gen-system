// generator/internal/agent/executor_web_v2658_test.go, v26.58 (D030):
// GenerateWebProject returned without a message when it could not create
// frontend/, and the executor returned nil after it, so generate_types_barrel
// counted as a success with the web project files missing.
package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestExecuteTask_WebProjectWriteFailureFailsTask(t *testing.T) {
	root := t.TempDir()
	// A regular file where the frontend folder should be.
	if err := os.WriteFile(filepath.Join(root, "frontend"), []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := schema.Entity{Name: "Order", Fields: []schema.Field{{Name: "Total", Type: "int"}}}
	task := planner.Task{ID: "t1", Type: "generate_types_barrel"}
	if err := ExecuteTask(task, map[string]interface{}{"Order": e}, root); err == nil {
		t.Fatal("generate_types_barrel: want an error when frontend/ cannot be created, got nil")
	}
}
