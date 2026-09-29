// D044: Evolve went on after a failed IR compile and passed only the
// processed entities as plan.Entities. The agent prunes the files of every
// entity outside plan.Entities, so the files of an unprocessed schema entity
// were deleted and main.go lost its routes.
package cli

import (
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestEvolve_CompileErrorStops(t *testing.T) {
	// v26.57 (D006) made the FK order of the report valid, so a duplicate
	// field makes Compile fail here.
	const s = `{"entities":[
 {"name":"Post","fields":[{"name":"title","type":"string"},{"name":"title","type":"string"}]},
 {"name":"User","fields":[{"name":"email","type":"string"}]}]}`
	for _, understand := range []bool{false, true} {
		snap := schema.SchemaFile{Entities: []schema.Entity{
			{Name: "User", Fields: []schema.Field{{Name: "email", Type: "string"}}},
		}}
		plans := evolveFixture(t, s, &snap)
		args := []string{"gen", "evolve", "app", "--schema", "s.json", "--apply", "--force"}
		run := Evolve
		if understand {
			args = append(args, "--understand")
			run = EvolveWithUnderstanding
		}
		setArgs(t, args...)
		out := captureStdout(t, run)
		if len(*plans) != 0 {
			t.Errorf("understand=%v: the agent ran after a compile error with entities %v", understand, entityNames((*plans)[0].Entities))
		}
		if !strings.Contains(out, "IR compile error") || strings.Contains(out, "No changes detected") {
			t.Errorf("understand=%v: want a stop on the compile error, got %q", understand, out)
		}
	}
}

func TestEvolve_PlanKeepsEveryEntity(t *testing.T) {
	const s = `{"entities":[
 {"name":"User","fields":[{"name":"email","type":"string"}]},
 {"name":"Post","fields":[{"name":"title","type":"string"},{"name":"body","type":"string"}]}]}`
	for _, understand := range []bool{false, true} {
		snap := schema.SchemaFile{Entities: []schema.Entity{
			{Name: "User", Fields: []schema.Field{{Name: "email", Type: "string"}}},
			{Name: "Post", Fields: []schema.Field{{Name: "title", Type: "string"}}},
		}}
		plans := evolveFixture(t, s, &snap)
		args := []string{"gen", "evolve", "app", "--schema", "s.json", "--apply", "--force"}
		run := Evolve
		if understand {
			args = append(args, "--understand")
			run = EvolveWithUnderstanding
		}
		setArgs(t, args...)
		captureStdout(t, run)
		if len(*plans) != 1 {
			t.Fatalf("understand=%v: want one agent run, got %d", understand, len(*plans))
		}
		got := strings.Join(entityNames((*plans)[0].Entities), ",")
		if got != "User,Post" {
			t.Errorf("understand=%v: plan.Entities = %s, want User,Post so the agent keeps the User files", understand, got)
		}
		for _, task := range (*plans)[0].Tasks {
			if task.EntityName == "User" {
				t.Errorf("understand=%v: unchanged User got task %s", understand, task.ID)
			}
		}
	}
}

func entityNames(es []schema.Entity) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Name)
	}
	return out
}
