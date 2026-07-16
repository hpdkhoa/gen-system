package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"gen-system/generator/internal/schema"
)

func genServiceFull(e schema.Entity, path string) {

	content := fmt.Sprintf(`


package service

import (
"database/sql"
"app/internal/repository"
)

func GetAll%[1]s(db *sql.DB, limit, offset int) (interface{}, error) {


rows, err := repository.GetAll%[1]s(db, limit, offset)
if err != nil {
	return nil, err
}
defer rows.Close()

return map%[1]sRows(rows)


}
`, e.Name)

	file := filepath.Join(path, "backend/internal/service/"+e.Name+"_service.go")
	os.WriteFile(file, []byte(content), 0644)

}
