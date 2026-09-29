// D072: Plan compared entity names only (EntityIR held no fields), so every
// entity present in both the old and the new IR landed in Changed, and the
// callers regenerated unchanged entities on every run.
package buildplan

import (
	"reflect"
	"testing"
)

func TestPlan_UnchangedEntityNotChanged(t *testing.T) {
	a := EntityIR{Name: "A", Fields: []FieldIR{{Name: "id", Type: "int"}, {Name: "title", Type: "string"}}}
	b := EntityIR{Name: "B", Fields: []FieldIR{{Name: "id", Type: "int"}}}
	s := SchemaIR{Entities: []EntityIR{a, b}}

	p := Plan(s, s)
	if len(p.Changed) != 0 || len(p.Entities) != 0 {
		t.Errorf("same IR as old and new: got Entities=%v Changed=%v, want both empty", p.Entities, p.Changed)
	}

	// Field order does not matter; a new field or a new type does.
	a2 := EntityIR{Name: "A", Fields: []FieldIR{{Name: "title", Type: "string"}, {Name: "id", Type: "int"}, {Name: "body", Type: "string"}}}
	b2 := EntityIR{Name: "B", Fields: []FieldIR{{Name: "id", Type: "int"}}}
	c := EntityIR{Name: "C"}
	p = Plan(s, SchemaIR{Entities: []EntityIR{b2, a2, c}})
	if !reflect.DeepEqual(p.Changed, []string{"A"}) || !reflect.DeepEqual(p.Entities, []string{"C"}) {
		t.Errorf("got Entities=%v Changed=%v, want Entities=[C] Changed=[A]", p.Entities, p.Changed)
	}

	b3 := EntityIR{Name: "B", Fields: []FieldIR{{Name: "id", Type: "string"}}}
	p = Plan(s, SchemaIR{Entities: []EntityIR{a, b3}})
	if !reflect.DeepEqual(p.Changed, []string{"B"}) {
		t.Errorf("type change: got Changed=%v, want [B]", p.Changed)
	}
}
