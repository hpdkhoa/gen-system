// evolve_keep_v2658_test.go — v26.58, the user's decision on follow up 8:
// gen evolve never drops a field that disappears from the schema. It keeps the
// field in the generated code and in .gen/ir_snapshot.json, and warns on every
// run. Before, the field stayed only while its entity was not regenerated: as
// soon as another change touched the entity, the generator used the schema as
// given and the field vanished from the code without a word.
package cli

import (
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestEvolve_KeepsRemovedFieldAndWarns(t *testing.T) {
	// The schema drops Post.body and adds Post.views in the same run, so Post
	// is regenerated.
	const s = `{"entities":[
 {"name":"User","fields":[{"name":"email","type":"string"}]},
 {"name":"Post","fields":[{"name":"title","type":"string"},{"name":"views","type":"int"}]}]}`
	for _, understand := range []bool{false, true} {
		snap := schema.SchemaFile{Entities: []schema.Entity{
			{Name: "User", Fields: []schema.Field{{Name: "email", Type: "string"}}},
			{Name: "Post", Fields: []schema.Field{{Name: "title", Type: "string"}, {Name: "body", Type: "string"}}},
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

		if !strings.Contains(out, "Post.body") || !strings.Contains(strings.ToLower(out), "keeps") {
			t.Errorf("understand=%v: no warning about Post.body:\n%s", understand, out)
		}
		if len(*plans) != 1 {
			t.Fatalf("understand=%v: want one agent run, got %d\n%s", understand, len(*plans), out)
		}
		var post *schema.Entity
		for i := range (*plans)[0].Entities {
			if (*plans)[0].Entities[i].Name == "Post" {
				post = &(*plans)[0].Entities[i]
			}
		}
		if post == nil || !hasField(*post, "body") || !hasField(*post, "views") {
			t.Errorf("understand=%v: Post must keep body and gain views, got %+v", understand, post)
		}
		ir, err := compiler.LoadPreviousIR()
		if err != nil {
			t.Fatal(err)
		}
		kept := false
		for _, e := range ir.Entities {
			for _, f := range e.Fields {
				if e.Name == "Post" && f.Name == "body" {
					kept = true
				}
			}
		}
		if !kept {
			t.Errorf("understand=%v: the snapshot lost Post.body, so a later run would drop it silently", understand)
		}
	}
}

func hasField(e schema.Entity, name string) bool {
	for _, f := range e.Fields {
		if f.Name == name {
			return true
		}
	}
	return false
}
