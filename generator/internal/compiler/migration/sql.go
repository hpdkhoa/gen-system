package migration

import (
	"fmt"
	"strings"
)

// GenerateSQL converts MigrationIR into UP SQL statements
func GenerateSQL(m MigrationIR) []string {
	var sql []string

	for _, step := range m.Steps {
		switch step.Type {

		case "CREATE_TABLE":
			// v26.57 (D007): one column per field of the new entity, typed the
			// same way ADD_COLUMN types it.
			cols := []string{"id SERIAL PRIMARY KEY"}
			for _, c := range step.Columns {
				cols = append(cols, c.Name+" "+mapType(c.Type))
			}
			sql = append(sql,
				fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s);", step.Entity, strings.Join(cols, ", ")),
			)

		case "DROP_TABLE":
			sql = append(sql,
				fmt.Sprintf("DROP TABLE IF EXISTS %s;", step.Entity),
			)

		case "ADD_COLUMN":
			sql = append(sql,
				fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s;",
					step.Entity,
					step.Column,
					mapType(step.ColumnType),
				),
			)

		case "ALTER_COLUMN":
			sql = append(sql,
				fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s;",
					step.Entity,
					step.Column,
					mapType(step.ColumnType),
				),
			)

		case "DROP_COLUMN":
			sql = append(sql,
				fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s;",
					step.Entity,
					step.Column,
				),
			)
		}
	}

	return sql
}

// GenerateRollbackSQL generates DOWN SQL to undo a migration
func GenerateRollbackSQL(m MigrationIR) []string {
	var sql []string

	// Reverse order for rollback
	for i := len(m.Steps) - 1; i >= 0; i-- {
		step := m.Steps[i]
		switch step.Type {

		case "CREATE_TABLE":
			sql = append(sql,
				fmt.Sprintf("DROP TABLE IF EXISTS %s;", step.Entity),
			)

		case "DROP_TABLE":
			// Cannot auto-restore a dropped table without schema snapshot
			sql = append(sql,
				fmt.Sprintf("-- MANUAL RESTORE REQUIRED for table: %s", step.Entity),
			)

		case "ADD_COLUMN":
			sql = append(sql,
				fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s;",
					step.Entity,
					step.Column,
				),
			)

		case "ALTER_COLUMN":
			sql = append(sql,
				fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s; -- was %s",
					step.Entity,
					step.Column,
					mapType(step.OldType),
					step.ColumnType,
				),
			)

		case "DROP_COLUMN":
			sql = append(sql,
				fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s; -- restored",
					step.Entity,
					step.Column,
					mapType(step.ColumnType),
				),
			)
		}
	}

	return sql
}

func mapType(t string) string {
	switch t {
	case "int":
		return "INTEGER"
	case "bool":
		return "BOOLEAN"
	case "time":
		return "TIMESTAMPTZ"
	case "float":
		return "NUMERIC"
	default:
		return "TEXT"
	}
}
