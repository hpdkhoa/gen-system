// generator/internal/planner_v2/continue_correction_v2611_test.go, v26.11
// (session 5): checks that whatever the caller appends to the existing-summary
// (specifically, the human belief-correction block) reaches the delta source.
// So the model that plans the continuation actually sees the corrections.
package planner_v2

import (
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ai"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestCorrectionContextReachesDeltaSource_v2611(t *testing.T) {
	var capturedSummary string
	restore := SetDeltaSourceForTest(func(idea, existingSummary string) (ai.ContinueDelta, error) {
		capturedSummary = existingSummary
		return ai.ContinueDelta{}, nil // empty delta: no changes, fine for this test
	})
	defer restore()

	existing := []schema.Entity{{Name: "Order", Fields: []schema.Field{{Name: "total", Type: "float64"}}}}

	// Simulate exactly what cli/services do: append a correction block to the
	// summary before calling BuildContinuePlan.
	correctionBlock := "Human-verified facts about the existing code:\n- Order.ApplyDiscount does: reduce order total by a percentage\n"
	summary := "Existing entities:\n- Order{total}\n\n" + correctionBlock

	_, err := BuildContinuePlan(existing, summary, "add a refund operation", t.TempDir(), false)
	if err != nil {
		t.Fatalf("BuildContinuePlan: %v", err)
	}
	if !strings.Contains(capturedSummary, "reduce order total by a percentage") {
		t.Errorf("delta source did not receive the correction block; got:\n%s", capturedSummary)
	}
	if !strings.Contains(capturedSummary, "Human-verified") {
		t.Errorf("delta source summary missing the correction header:\n%s", capturedSummary)
	}
}
