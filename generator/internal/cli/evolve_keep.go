// evolve_keep.go (v26.58): gen evolve never drops a field.
//
// The user decided that a field which disappears from the schema must stay in
// the generated app, with a warning. Before, the evolution merge kept the field
// in the IR, but the generator worked from the schema file as given: the field
// stayed only until its entity was regenerated for another change, and then it
// vanished from the code without a word. The snapshot saved after the run also
// lost it, so later runs could not see the difference any more.
//
// keepRemovedFields puts every removed field back into the schema that drives
// generation and the snapshot, and prints one warning per field on every run,
// so the gap between the schema and the code stays visible. The explicit way
// to drop a column is gen migrate --force, which records the removal in
// .gen/ir_snapshot.json.
package cli

import (
	"fmt"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler/diff"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// keepRemovedFields adds each field that the change set reports as removed
// from an entity back into sfile, and returns the kept fields as
// "Entity.field". The built-in id is never listed.
func keepRemovedFields(sfile *schema.SchemaFile, cs diff.ChangeSet) []string {
	var kept []string
	for _, mod := range cs.Modified {
		for _, f := range mod.Removed {
			if strings.EqualFold(f.Name, "id") {
				continue
			}
			for i := range sfile.Entities {
				e := &sfile.Entities[i]
				if e.Name != mod.EntityName || entityHasField(*e, f.Name) {
					continue
				}
				e.Fields = append(e.Fields, schema.Field{Name: f.Name, Type: f.Type})
				kept = append(kept, mod.EntityName+"."+f.Name)
			}
		}
	}
	return kept
}

func entityHasField(e schema.Entity, name string) bool {
	for _, f := range e.Fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

// keepFieldsAndReplan applies keepRemovedFields and, when it kept anything,
// prints the warnings and compiles the schema again, so the build plan and
// the snapshot saved after the run hold the kept fields.
func keepFieldsAndReplan(sfile *schema.SchemaFile, oldIR, newIR compiler.SchemaIR, cs diff.ChangeSet) (compiler.SchemaIR, error) {
	kept := keepRemovedFields(sfile, cs)
	if len(kept) == 0 {
		return newIR, nil
	}
	for _, k := range kept {
		fmt.Printf("⚠️  %s is not in the schema any more. gen evolve keeps it in the code; "+
			"to drop the column on purpose, run gen migrate --force with this schema.\n", k)
	}
	return compiler.Compile(*sfile)
}
