package migration

import (
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler/diff"
)

func BuildMigration(cs diff.ChangeSet) MigrationIR {

	var m MigrationIR

	// CREATE TABLES
	for _, e := range cs.AddEntities {
		// v26.57 (D007): carry the fields, so GenerateSQL writes one column per
		// field. The id field is the built-in SERIAL key, not a second column.
		var cols []Column
		for _, f := range e.Fields {
			if strings.EqualFold(f.Name, "id") {
				continue
			}
			cols = append(cols, Column{Name: f.Name, Type: f.Type})
		}
		m.Steps = append(m.Steps, MigrationStep{
			Type:    "CREATE_TABLE",
			Entity:  e.Name,
			Columns: cols,
		})
	}

	// DROP TABLES
	for _, e := range cs.RemoveEntities {
		m.Steps = append(m.Steps, MigrationStep{
			Type:   "DROP_TABLE",
			Entity: e.Name,
		})
	}

	// FIELD CHANGES
	for _, mod := range cs.Modified {
		for _, add := range mod.Added {
			m.Steps = append(m.Steps, MigrationStep{
				Type:       "ADD_COLUMN",
				Entity:     mod.EntityName,
				Column:     add.Name,
				ColumnType: add.Type,
			})
		}

		for _, ch := range mod.Modified {
			m.Steps = append(m.Steps, MigrationStep{
				Type:       "ALTER_COLUMN",
				Entity:     mod.EntityName,
				Column:     ch.Field,
				OldType:    ch.OldType,
				ColumnType: ch.NewType,
			})
		}
	}

	return m
}
