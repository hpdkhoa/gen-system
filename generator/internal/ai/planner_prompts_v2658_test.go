// generator/internal/ai/planner_prompts_v2658_test.go — v26.58.
//
// D067: ExtractFeatures, PlanArchitecture and PlanEntities sliced
// resp[start:end+1] when the closing bracket stood before the opening one, so
// an answer such as "done} now {" panicked with slice bounds out of range
// instead of returning the parse error their callers handle.
package ai

import "testing"

func noPanic(t *testing.T, name string, fn func() error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", name, r)
		}
	}()
	if err := fn(); err == nil {
		t.Errorf("%s: want an error for an answer with no JSON, got nil", name)
	}
}

func TestPlanner_BracketsReversedReturnError(t *testing.T) {
	t.Run("ExtractFeatures", func(t *testing.T) {
		fakeModel(t, "done} now {")
		noPanic(t, "ExtractFeatures", func() error {
			_, err := ExtractFeatures("a shop")
			return err
		})
	})
	t.Run("PlanArchitecture", func(t *testing.T) {
		fakeModel(t, "done} now {")
		noPanic(t, "PlanArchitecture", func() error {
			_, _, _, err := PlanArchitecture([]string{"orders"})
			return err
		})
	})
	t.Run("PlanEntities", func(t *testing.T) {
		fakeModel(t, "see item] and then [")
		noPanic(t, "PlanEntities", func() error {
			_, err := PlanEntities("a shop", []string{"orders"}, "monolith", []string{"api"})
			return err
		})
	})
}
