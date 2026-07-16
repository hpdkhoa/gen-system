// generator/internal/engine/preserve_ops_v2324_test.go — v23.24: continue must
// not drop an entity's existing operations when adding a new one.
package engine

import "testing"

func TestParseExistingOps_RecoversMethods(t *testing.T) {
	src := `package service
func (s *OrderService) Checkout(id int, in map[string]any) (any, error) {
	o, ok := s.store.Get(id)
	if !ok { return nil, errors.New("not found") }
	return o, nil
}
func (s *OrderService) ApplyDiscount(id int, in map[string]any) (any, error) {
	return nil, nil
}
`
	ops := parseExistingOps(src, "Order")
	if len(ops) != 2 {
		t.Fatalf("expected 2 ops, got %d", len(ops))
	}
	names := map[string]bool{}
	for _, o := range ops {
		names[o.Name] = true
		if o.Body == "" {
			t.Errorf("op %s has empty body", o.Name)
		}
	}
	if !names["Checkout"] || !names["ApplyDiscount"] {
		t.Errorf("missing ops: %v", names)
	}
	// route derivation
	for _, o := range ops {
		if o.Name == "ApplyDiscount" && o.Subpath != "discount" {
			t.Errorf("ApplyDiscount subpath = %q, want discount", o.Subpath)
		}
	}
}
