// executor_mobile_v2659_test.go (N07): GenerateMobileProject dropped every
// write error and the executor ignored it, so generate_types_barrel counted as
// a success with the mobile project files missing.
package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestExecuteTask_MobileProjectWriteFailureFailsTask(t *testing.T) {
	root := t.TempDir()
	// mobile/ exists, so the executor writes the mobile project, but a folder
	// sits where mobile/package.json must go.
	if err := os.MkdirAll(filepath.Join(root, "mobile", "package.json", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := schema.Entity{Name: "Order", Fields: []schema.Field{{Name: "Total", Type: "int"}}}
	task := planner.Task{ID: "t1", Type: "generate_types_barrel"}
	if err := ExecuteTask(task, map[string]interface{}{"Order": e}, root); err == nil {
		t.Fatal("generate_types_barrel: want an error when mobile/package.json cannot be written, got nil")
	}
}
