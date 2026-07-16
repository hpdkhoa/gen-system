package engine

import (
	"fmt"
	"os"
	"path/filepath"
)

func genMigration(name string, sql string) error {

	dir := "migrations"
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	files, _ := os.ReadDir("migrations")
	count := len(files)

	filename := fmt.Sprintf(
		"%s_%03d_alter_%s.sql",
		timestamp(),
		count+1,
		safeName(name),
	)

	path := filepath.Join(dir, filename)

	err = os.WriteFile(path, []byte(sql), 0644)
	if err != nil {
		return err
	}

	fmt.Println("📝 migration written:", path)
	return nil
}

func mapSQLType(t string) string {
	switch t {
	case "int":
		return "INTEGER"
	case "bool":
		return "BOOLEAN"
	default:
		return "TEXT"
	}
}
