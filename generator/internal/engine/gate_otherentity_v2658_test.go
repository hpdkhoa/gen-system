// generator/internal/engine/gate_otherentity_v2658_test.go — v26.58 (D073):
// operationCompiles held a stand-in model for one entity only and replaced the
// prefix model.Order inside longer names, so a body for Order that used
// model.OrderItem failed the gate with "undefined: model_OrderItem" and ended as
// a "not implemented" stub, although it compiles in the real package.
package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ai"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

type otherEntityProvider struct{}

func (otherEntityProvider) Operations(idea string, e schema.Entity, all []schema.Entity) ([]ai.Operation, error) {
	if e.Name != "Order" {
		return nil, nil
	}
	return []ai.Operation{{
		Name: "AddLine", HTTPMethod: "POST", Subpath: "add-line",
		Body: "o, ok := s.store.Get(id)\n" +
			"if !ok {\nreturn nil, errors.New(\"order not found\")\n}\n" +
			"item := model.OrderItem{Qty: 2}\n" +
			"o.Total = o.Total + item.Qty\n" +
			"updated, _ := s.store.Update(id, o)\n" +
			"return updated, nil",
	}}, nil
}

func TestServiceLogic_BodyUsingOtherEntityModelCompiles(t *testing.T) {
	t.Setenv("GEN_BACKEND", "")
	root := t.TempDir()
	order := schema.Entity{Name: "Order", Fields: []schema.Field{{Name: "Total", Type: "int"}}}
	item := schema.Entity{Name: "OrderItem", Fields: []schema.Field{{Name: "Qty", Type: "int"}}}
	all := []schema.Entity{order, item}

	generateBackend(t, all, root)
	for _, e := range all {
		if err := GenerateServiceLogic("an orders app", e, all, otherEntityProvider{}, root); err != nil {
			t.Fatalf("GenerateServiceLogic(%s): %v", e.Name, err)
		}
	}
	src, err := os.ReadFile(filepath.Join(root, "backend/internal/service/order_service.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "not implemented") || !strings.Contains(string(src), "model.OrderItem{Qty: 2}") {
		t.Fatalf("the AddLine body was stubbed by the gate:\n%s", src)
	}
	// The body the gate accepted compiles in the real package.
	vetGeneratedApp(t, root)
}
