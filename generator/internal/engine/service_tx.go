package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"gen-system/generator/internal/schema"
)

func genServiceTx(e schema.Entity, path string) {

	content := fmt.Sprintf(`


package service

import (
"database/sql"
"app/internal/db"
"app/internal/repository"
)

func Create%[1]s(dbConn *sql.DB, input map[string]interface{}) error {


return db.WithTx(dbConn, func(tx *sql.Tx) error {

	if err := repository.Create%[1]s(tx, input); err != nil {
		return err
	}

	return nil
})


}
`, e.Name)

	file := filepath.Join(path, "backend/internal/service/"+e.Name+"_tx.go")
	os.WriteFile(file, []byte(content), 0644)

}
