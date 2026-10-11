// validate_v2657_test.go (v26.57, D006): validateIR must accept a schema that
// lists a child entity before its parent. It filled the entity set inside the
// same loop that checked foreign keys, so Post (userId) before User failed with
// "invalid FK reference: User" while the order User, Post compiled. The planner
// lets the model choose the order, so a valid plan could fail on order alone.
package compiler

import "testing"

func TestValidateIR_ChildBeforeParent(t *testing.T) {
	ir := SchemaIR{Entities: []EntityIR{
		{Name: "Post", Fields: []FieldIR{
			{Name: "id", Type: "int", IsPrimary: true},
			{Name: "userId", Type: "int", IsForeign: true, RefEntity: "User"},
		}},
		{Name: "User", Fields: []FieldIR{{Name: "id", Type: "int", IsPrimary: true}}},
	}}
	if err := validateIR(ir); err != nil {
		t.Fatalf("valid schema rejected: %v", err)
	}
}

func TestValidateIR_UnknownParentStillFails(t *testing.T) {
	ir := SchemaIR{Entities: []EntityIR{
		{Name: "Post", Fields: []FieldIR{
			{Name: "authorId", Type: "int", IsForeign: true, RefEntity: "Author"},
		}},
	}}
	if err := validateIR(ir); err == nil {
		t.Fatal("a foreign key to a missing entity must fail")
	}
}

func TestValidateIR_DuplicateEntityStillFails(t *testing.T) {
	ir := SchemaIR{Entities: []EntityIR{{Name: "Post"}, {Name: "Post"}}}
	if err := validateIR(ir); err == nil {
		t.Fatal("a duplicate entity must fail")
	}
}
