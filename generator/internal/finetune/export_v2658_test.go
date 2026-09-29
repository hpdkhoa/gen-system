// D050: buildTrainingExample put the stored fix under the heading "Broken Code"
// in the user message and the same fix in the assistant message, so the model
// learned to copy its input. FixRecord holds no broken body, so the section
// must be left out.
package finetune

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/intelligence"
)

const d050Fix = "return &User{ID: id}, nil // the verified fix"

func TestBuildTrainingExample_UserDoesNotHoldTheFix(t *testing.T) {
	rec := intelligence.FixRecord{
		ErrorType: "undefined", TaskType: "generate_service",
		EntityName: "User", FixBody: d050Fix, SuccessCount: 2,
	}
	ex := buildTrainingExample(rec)
	if len(ex.Messages) != 3 {
		t.Fatalf("want 3 messages, got %d", len(ex.Messages))
	}
	user := ex.Messages[1].Content
	if strings.Contains(user, d050Fix) {
		t.Errorf("user message holds the fix body:\n%s", user)
	}
	if strings.Contains(user, "Broken Code") {
		t.Errorf("user message has a Broken Code section with no broken code:\n%s", user)
	}
	if ex.Messages[2].Content != d050Fix {
		t.Errorf("assistant = %q, want the fix", ex.Messages[2].Content)
	}
}

// The same through ExportTrainingData, which reads .gen/memory.json from the
// working directory.
func TestExportTrainingData_UserDoesNotHoldTheFix(t *testing.T) {
	t.Chdir(t.TempDir())
	mem := intelligence.Memory{Fixes: map[string]intelligence.FixRecord{
		"abc": {ErrorType: "logic", TaskType: "generate_handler", EntityName: "Order",
			FixBody: d050Fix, SuccessCount: 1},
	}}
	if err := intelligence.SaveMemory(mem); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "train.jsonl")
	stats, err := ExportTrainingData(out, 1)
	if err != nil || stats.Exported != 1 {
		t.Fatalf("export: stats %+v err %v", stats, err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var ex TrainingExample
	if err := json.Unmarshal(data, &ex); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ex.Messages[1].Content, d050Fix) {
		t.Errorf("user message holds the fix body:\n%s", ex.Messages[1].Content)
	}
}
