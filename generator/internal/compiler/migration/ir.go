package migration

type MigrationIR struct {
	Steps []MigrationStep
}

type MigrationStep struct {
	Type        string // CREATE_TABLE, ALTER_TABLE, DROP_COLUMN, ADD_COLUMN
	Entity      string
	Column      string
	ColumnType  string
	OldType     string
	Constraints string
	// Columns holds the fields of a CREATE_TABLE step, besides the built-in
	// id. v26.57 (D007): before, a new table got the id column alone.
	Columns []Column
}

// Column is one field of a table that a CREATE_TABLE step creates.
type Column struct {
	Name string
	Type string
}
