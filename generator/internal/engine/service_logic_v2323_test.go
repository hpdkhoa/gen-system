package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ai"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// repairingProvider returns a body that does not compile on the first call
// (uses the payload without a type assertion). When asked to repair with the
// build error, it returns a correct body. This proves the v23.23 loop turns a
// near-miss into real logic instead of stubbing it.
type repairingProvider struct{ repairs int }

func (p *repairingProvider) Operations(idea string, e schema.Entity, all []schema.Entity) ([]ai.Operation, error) {
	// Broken: in["amount"] is any; can't add to int p.Stock without assertion.
	return []ai.Operation{{
		Name: "AdjustStock", HTTPMethod: "POST", Subpath: "adjust-stock",
		Body: "p, ok := s.store.Get(id)\n" +
			"if !ok {\nreturn nil, errors.New(\"not found\")\n}\n" +
			"p.Stock += in[\"amount\"]\n" + // type error on purpose
			"return p, nil",
	}}, nil
}

func (p *repairingProvider) RepairOperation(idea string, e schema.Entity, all []schema.Entity, op ai.Operation, buildErr string) (ai.Operation, error) {
	p.repairs++
	// The "model" reads the error and fixes the assertion.
	op.Body = "p, ok := s.store.Get(id)\n" +
		"if !ok {\nreturn nil, errors.New(\"not found\")\n}\n" +
		"raw, ok2 := in[\"amount\"].(float64)\n" +
		"if !ok2 {\nreturn nil, errors.New(\"amount required\")\n}\n" +
		"p.Stock += int(raw)\n" +
		"if p.Stock < 0 {\nreturn nil, errors.New(\"stock cannot go negative\")\n}\n" +
		"updated, _ := s.store.Update(id, p)\n" +
		"return updated, nil"
	return op, nil
}

func TestRepairLoop_FixesInsteadOfStub_v2323(t *testing.T) {
	dir := t.TempDir()
	product := schema.Entity{Name: "Product", Fields: []schema.Field{
		{Name: "Name", Type: "string"},
		{Name: "Price", Type: "float"},
		{Name: "Stock", Type: "int"},
	}}
	prov := &repairingProvider{}

	GenerateServiceLogic("orders app: products have stock that can't go negative",
		product, []schema.Entity{product}, prov, dir)

	svc := filepath.Join(dir, "backend/internal/service/product_service.go")
	data, err := os.ReadFile(svc)
	if err != nil {
		t.Fatalf("service file not written: %v", err)
	}
	src := string(data)
	if strings.Contains(src, "not implemented") {
		t.Errorf("repair loop should have fixed AdjustStock, but it was stubbed:\n%s", src)
	}
	if !strings.Contains(src, "stock cannot go negative") {
		t.Errorf("repaired body not present; got:\n%s", src)
	}
	if prov.repairs == 0 {
		t.Error("repair was never attempted")
	}
}

// A provider with no repair capability (replay) must still stub a broken body.
// The containment guarantee is unchanged when repair isn't available.
func TestNoRepairer_StillStubs_v2323(t *testing.T) {
	e := schema.Entity{Name: "Order", Fields: []schema.Field{{Name: "Total", Type: "float"}}}
	ops := []ai.Operation{{Name: "Bad", HTTPMethod: "POST", Body: "p.Stock += in[\"x\"]"}}
	out := sanitizeOperations("idea", e, []schema.Entity{e}, ops, ai.ReplayProvider())
	if len(out) != 1 || !strings.Contains(out[0].Body, "not implemented") {
		t.Errorf("broken body with no repairer should stub; got: %+v", out)
	}
}
