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
}
