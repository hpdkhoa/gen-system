// broken_v2658_test.go (v26.58, follow up of D050): once the healer records the
// body that failed, the training example shows it under "Broken Code" again,
// and it is never the fix itself.
package finetune

import (
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/intelligence"
)

func TestBuildTrainingExample_ShowsTheBrokenBody(t *testing.T) {
	const broken = "return s.n + price, nil"
	rec := intelligence.FixRecord{
		ErrorType: "undefined", TaskType: "generate_service", EntityName: "Order",
		FixBody: d050Fix, BrokenBody: broken, SuccessCount: 2,
	}
	user := buildTrainingExample(rec).Messages[1].Content
	if !strings.Contains(user, "## Broken Code") || !strings.Contains(user, broken) {
		t.Fatalf("user message lacks the broken body:\n%s", user)
	}
	if strings.Contains(user, d050Fix) {
		t.Fatalf("user message holds the fix:\n%s", user)
	}
}
