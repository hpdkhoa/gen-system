// D043: relTypeIRToSchema had no case for "N:1", the only relation type the
// compiler sets, so Task with userId got hasMany User instead of belongsTo.
package services

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
)

func TestIRToSchema_ManyToOneIsBelongsTo(t *testing.T) {
	ir := compiler.SchemaIR{
		Entities: []compiler.EntityIR{
			{Name: "User", Fields: []compiler.FieldIR{{Name: "id", Type: "int"}}},
			{Name: "Task", Fields: []compiler.FieldIR{{Name: "id", Type: "int"}, {Name: "userId", Type: "int"}}},
		},
		Relations: []compiler.RelationIR{
			{FromEntity: "Task", ToEntity: "User", FromField: "userId", ToField: "id", Type: "N:1"},
		},
	}
	for _, e := range irToSchemaEntities(ir) {
		if e.Name != "Task" {
			continue
		}
		if len(e.Relations) != 1 || e.Relations[0].Type != "belongsTo" || e.Relations[0].Target != "User" {
			t.Fatalf("want Task belongsTo User, got %+v", e.Relations)
		}
		return
	}
	t.Fatal("Task missing")
}
