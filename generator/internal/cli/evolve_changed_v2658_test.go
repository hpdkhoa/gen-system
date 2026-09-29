// D071: Evolve and EvolveWithUnderstanding built changedSet from
// buildPlan.Entities and buildPlan.Changed, and Changed held every entity of
// the old snapshot, so a second run with the same schema regenerated every
// entity and never printed "No changes detected".
package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// evolveFixture prepares a working dir with an app dir, a schema file and,
// when snap is not nil, an IR snapshot. It stubs the agent and returns the
// plans it receives.
func evolveFixture(t *testing.T, schemaJSON string, snap *schema.SchemaFile) *[]planner.Plan {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("app", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("s.json", []byte(schemaJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if snap != nil {
		ir, err := compiler.Compile(*snap)
		if err != nil {
			t.Fatal(err)
		}
		if err := compiler.SaveIR(ir); err != nil {
			t.Fatal(err)
		}
	}
	var plans []planner.Plan
	old := evolveRunFromPlan
	evolveRunFromPlan = func(p planner.Plan, root string) error {
		plans = append(plans, p)
		return nil
	}
	t.Cleanup(func() { evolveRunFromPlan = old })
	return &plans
}

func setArgs(t *testing.T, args ...string) {
	t.Helper()
	old := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = old })
}

const twoEntitySchema = `{"entities":[
 {"name":"User","fields":[{"name":"email","type":"string"}]},
 {"name":"Post","fields":[{"name":"title","type":"string"}]}]}`

func TestEvolve_SameSchemaNoChanges(t *testing.T) {
	for _, understand := range []bool{false, true} {
		sf, err := schema.LoadFromFile(writeTemp(t, twoEntitySchema))
		if err != nil {
			t.Fatal(err)
		}
		plans := evolveFixture(t, twoEntitySchema, &sf)
		args := []string{"gen", "evolve", "app", "--schema", "s.json", "--apply", "--force"}
		run := Evolve
		if understand {
			args = append(args, "--understand")
			run = EvolveWithUnderstanding
		}
		setArgs(t, args...)
		out := captureStdout(t, run)
		if len(*plans) != 0 {
			var names []string
			for _, task := range (*plans)[0].Tasks {
				names = append(names, task.ID)
			}
			t.Errorf("understand=%v: same schema ran the agent with tasks %v", understand, names)
		}
		if !strings.Contains(out, "No changes detected") {
			t.Errorf("understand=%v: want \"No changes detected\", got %q", understand, out)
		}
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "schema-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}
