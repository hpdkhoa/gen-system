package migration

import (
	"gen-system/generator/internal/compiler/diff"
)

func BuildMigration(cs diff.ChangeSet) MigrationIR {

	var m MigrationIR

	// CREATE TABLES
	for _, e := range cs.AddEntities {
		m.Steps = append(m.Steps, MigrationStep{
			Type:   "CREATE_TABLE",
			Entity: e.Name,
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
