// D037: Evolve saved the IR snapshot in the working directory after a staged
// run too, although the source was untouched. A later gen migrate then saw no
// change and applied no SQL for it.
package cli

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestEvolve_StagedRunKeepsSnapshot(t *testing.T) {
	const s = `{"entities":[{"name":"User","fields":[{"name":"email","type":"string"},{"name":"age","type":"int"}]}]}`
	for _, understand := range []bool{false, true} {
		snap := schema.SchemaFile{Entities: []schema.Entity{
			{Name: "User", Fields: []schema.Field{{Name: "email", Type: "string"}}},
		}}
		plans := evolveFixture(t, s, &snap)
		args := []string{"gen", "evolve", "app", "--schema", "s.json"}
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
		ir, err := compiler.LoadPreviousIR()
		if err != nil {
			t.Fatal(err)
		}
		if len(ir.Entities) != 1 || len(ir.Entities[0].Fields) != len(mustCompile(t, snap).Entities[0].Fields) {
			t.Errorf("understand=%v: a staged run changed the snapshot to %+v", understand, ir.Entities)
		}
	}
}

func mustCompile(t *testing.T, sf schema.SchemaFile) compiler.SchemaIR {
	t.Helper()
	ir, err := compiler.Compile(sf)
	if err != nil {
		t.Fatal(err)
	}
	return ir
}
