// D072: toBuildPlanIR passed only entity names to buildplan.Plan, so CompileV3
// with the same IR as old and new put every entity into Changed. With names
// only, a real field change could not be seen either.
package compiler

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestCompileV3_SameIRHasNoChanged(t *testing.T) {
	t.Chdir(t.TempDir()) // no .gen/diff_intent.json
	ir, err := Compile(schema.SchemaFile{Entities: []schema.Entity{
		{Name: "User", Fields: []schema.Field{{Name: "email", Type: "string"}}},
		{Name: "Post", Fields: []schema.Field{{Name: "title", Type: "string"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, plan, _ := CompileV3(ir, ir)
	if len(plan.Changed) != 0 || len(plan.Entities) != 0 {
		t.Errorf("same IR: got Entities=%v Changed=%v, want both empty", plan.Entities, plan.Changed)
	}

	ir2, err := Compile(schema.SchemaFile{Entities: []schema.Entity{
		{Name: "User", Fields: []schema.Field{{Name: "email", Type: "string"}, {Name: "age", Type: "int"}}},
		{Name: "Post", Fields: []schema.Field{{Name: "title", Type: "string"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, plan, _ = CompileV3(ir, ir2)
	if len(plan.Changed) != 1 || plan.Changed[0] != "User" {
		t.Errorf("new field on User: got Changed=%v, want [User]", plan.Changed)
	}
}
