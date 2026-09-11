package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ai"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// realFieldProvider emits bodies that read the entity's REAL fields — exactly
// the kind of correct business logic that v23.18's fields-less stand-in
// (struct{ID int}) wrongly rejected, stubbing 100% of real logic. v23.22 makes
// the compile gate carry the real fields, so these must now PASS the gate.
type realFieldProvider struct{}

func (realFieldProvider) Operations(idea string, e schema.Entity, all []schema.Entity) ([]ai.Operation, error) {
	switch e.Name {
	case "Order":
		return []ai.Operation{
			{ // reads o.Total and o.Status — real fields
				Name: "Checkout", HTTPMethod: "POST", Subpath: "checkout",
				Body: "o, ok := s.store.Get(id)\n" +
					"if !ok {\nreturn nil, errors.New(\"order not found\")\n}\n" +
					"if o.Status == \"paid\" {\nreturn nil, errors.New(\"order already paid\")\n}\n" +
					"o.Status = \"paid\"\n" +
					"updated, _ := s.store.Update(id, o)\n" +
					"return updated, nil",
			},
			{ // mutates o.Total — real field
				Name: "ApplyDiscount", HTTPMethod: "POST", Subpath: "discount",
				Body: "o, ok := s.store.Get(id)\n" +
					"if !ok {\nreturn nil, errors.New(\"order not found\")\n}\n" +
					"o.Total = o.Total * 0.9\n" +
					"updated, _ := s.store.Update(id, o)\n" +
					"return updated, nil",
			},
		}, nil
	case "Product":
		return []ai.Operation{
			{ // reads/guards p.Stock — the "can't go negative" rule
				Name: "AdjustStock", HTTPMethod: "POST", Subpath: "adjust-stock",
				Body: "p, ok := s.store.Get(id)\n" +
					"if !ok {\nreturn nil, errors.New(\"product not found\")\n}\n" +
					"p.Stock = p.Stock - 5\n" +
					"if p.Stock < 0 {\nreturn nil, errors.New(\"stock cannot go negative\")\n}\n" +
					"updated, _ := s.store.Update(id, p)\n" +
					"return updated, nil",
			},
		}, nil
	}
	return nil, nil
}

func TestServiceLogic_RealFieldBodiesCompile_v2322(t *testing.T) {
	dir := t.TempDir()
	order := schema.Entity{Name: "Order", Fields: []schema.Field{
		{Name: "Total", Type: "float"},
		{Name: "Status", Type: "string"},
	}}
	product := schema.Entity{Name: "Product", Fields: []schema.Field{
		{Name: "Name", Type: "string"},
		{Name: "Price", Type: "float"},
		{Name: "Stock", Type: "int"},
	}}
	all := []schema.Entity{order, product}

	GenerateServiceLogic("an orders app", order, all, realFieldProvider{}, dir)
	GenerateServiceLogic("an orders app", product, all, realFieldProvider{}, dir)

	checkNotStubbed := func(file string, methods ...string) {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("service file not written: %v (%s)", err, file)
		}
		src := string(data)
		for _, m := range methods {
			// The method must exist...
			if !strings.Contains(src, "func (s *") || !strings.Contains(src, m+"(id int") {
				t.Errorf("%s: method %s missing", filepath.Base(file), m)
			}
		}
		// ...and the real logic must NOT have been stubbed out.
		if strings.Contains(src, "not implemented") {
			t.Errorf("%s: a real-field body was wrongly stubbed (v23.22 regression). src:\n%s",
				filepath.Base(file), src)
		}
	}

	checkNotStubbed(filepath.Join(dir, "backend/internal/service/order_service.go"), "Checkout", "ApplyDiscount")
	checkNotStubbed(filepath.Join(dir, "backend/internal/service/product_service.go"), "AdjustStock")
}

// standInModelStruct must mirror the real model: ID + the entity's fields with
// the same goType mapping, so the gate compiles bodies identically to the app.
func TestStandInModelStruct_HasRealFields_v2322(t *testing.T) {
	e := schema.Entity{Name: "Order", Fields: []schema.Field{
		{Name: "Total", Type: "float"},
		{Name: "Status", Type: "string"},
	}}
	got := standInModelStruct(e, "Order")
	for _, want := range []string{"type model_Order struct", "ID int", "Total float64", "Status string"} {
		if !strings.Contains(got, want) {
			t.Errorf("stand-in missing %q; got:\n%s", want, got)
		}
	}
}
