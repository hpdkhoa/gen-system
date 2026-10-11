// generator/internal/engine/service_logic_v2310_test.go, v23.10 (Phase 4-B):
// the containment guarantee. A model op whose body does not compile must be
// dropped to a safe "not implemented" stub so the service still builds.
package engine

import (
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ai"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestSanitizeOperations_BadBodyDropsToStub(t *testing.T) {
	e := schema.Entity{Name: "Order", Fields: []schema.Field{{Name: "total", Type: "float"}}}
	ops := []ai.Operation{
		{Name: "Good", HTTPMethod: "POST", Body: "return nil, nil"},
		{Name: "Bad", HTTPMethod: "POST", Body: "this is not valid go ;;; {{{"},
		{Name: "", HTTPMethod: "POST", Body: "return nil, nil"},     // empty name dropped
		{Name: "Good", HTTPMethod: "POST", Body: "return nil, nil"}, // dup dropped
	}
	// Use a provider with no repair capability (replay) so a genuinely broken
	// body still falls straight to a stub: the original containment guarantee.
	out := sanitizeOperations("an orders app", e, []schema.Entity{e}, ops, ai.ReplayProvider())
	if len(out) != 2 {
		t.Fatalf("want 2 ops (Good + Bad-as-stub), got %d", len(out))
	}
	var bad ai.Operation
	for _, o := range out {
		if o.Name == "Bad" {
			bad = o
		}
	}
	if !strings.Contains(bad.Body, "not implemented") {
		t.Errorf("Bad op should have been dropped to a not-implemented stub, got body: %q", bad.Body)
	}
}
