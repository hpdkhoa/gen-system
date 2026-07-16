package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gen-system/generator/internal/schema"
)

func genRepoTx(e schema.Entity, path string) {

	table := strings.ToLower(e.Name)

	content := fmt.Sprintf(`


package repository

import "database/sql"

func Create%[1]s(tx *sql.Tx, data map[string]interface{}) error {


query := "INSERT INTO %[2]s DEFAULT VALUES"
_, err := tx.Exec(query)
return err


}
`, e.Name, table)

	file := filepath.Join(path, "backend/internal/repository/"+table+"_tx.go")
	os.WriteFile(file, []byte(content), 0644)

}
