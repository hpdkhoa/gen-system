// generator/internal/agent/executor_write_v2658_test.go, v26.58 (D029): the
// engine write helpers dropped every file system error and the exported
// generators returned nothing, so a generate task whose files could not be
// written still counted as a success. With a regular file named backend in the
// output folder, the backend generate tasks must now fail.
package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestExecuteTask_BackendWriteFailureFailsTask(t *testing.T) {
	t.Setenv("GEN_BACKEND", "")
	e := schema.Entity{Name: "Order", Fields: []schema.Field{{Name: "Total", Type: "int"}}}
	entities := map[string]interface{}{"Order": e}
	for _, typ := range []string{"generate_model", "generate_repo", "generate_service", "generate_handler", "generate_validation", "generate_router"} {
		root := t.TempDir()
		// A regular file where the backend folder should be: no backend file can
		// be written below it.
		if err := os.WriteFile(filepath.Join(root, "backend"), []byte("not a folder"), 0o644); err != nil {
			t.Fatal(err)
		}
		task := planner.Task{ID: "t1", Type: typ, EntityName: "Order"}
		if err := ExecuteTask(task, entities, root); err == nil {
			t.Errorf("%s: want an error when backend/ cannot be created, got nil", typ)
		}
	}
}
