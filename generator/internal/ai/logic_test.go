// generator/internal/ai/logic_test.go — v23.10 (Phase 4-B): lock the operations
// parse contract (envelope, bare array, fenced) + the replay provider.
package ai

import (
	"testing"

	"gen-system/generator/internal/schema"
)

func TestParseOperations_EnvelopeArrayAndFence(t *testing.T) {
	cases := []string{
		`{"operations":[{"name":"Checkout","http_method":"POST","body":"return nil,nil"}]}`,
		`[{"name":"Checkout","http_method":"POST","body":"return nil,nil"}]`,
		"```json\n{\"operations\":[{\"name\":\"Checkout\",\"http_method\":\"POST\",\"body\":\"return nil,nil\"}]}\n```",
	}
	for i, raw := range cases {
		ops, err := ParseOperations(raw)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if len(ops) != 1 || ops[0].Name != "Checkout" {
			t.Errorf("case %d: want 1 op named Checkout, got %+v", i, ops)
		}
	}
}

func TestReplayProvider_ReturnsRealOps(t *testing.T) {
	p := ReplayProvider()
	ops, err := p.Operations("an orders app", schema.Entity{Name: "Order"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) < 1 {
		t.Fatal("replay provider should return Order operations")
	}
	// Unknown entity → no extra ops (CRUD only), not an error.
	ops2, err := p.Operations("x", schema.Entity{Name: "Nonexistent"}, nil)
	if err != nil || len(ops2) != 0 {
		t.Errorf("unknown entity should yield 0 ops and no error, got %d, %v", len(ops2), err)
	}
}
