package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gen-system/generator/internal/schema"
)

func genRepoWithRelations(e schema.Entity, path string) {

	table := strings.ToLower(e.Name)

	query := fmt.Sprintf("SELECT * FROM %s", table)

	// naive join for belongsTo
	for _, r := range e.Relations {
		if r.Type == "belongsTo" {
			query += fmt.Sprintf(
				" LEFT JOIN %s ON %s.%s = %s.id",
				strings.ToLower(r.Target),
				table,
				strings.ToLower(r.ForeignKey),
				strings.ToLower(r.Target),
			)
		}
	}

	content := fmt.Sprintf(`


package repository

func GetAll%[1]s() string {
query := "%[2]s"
return query
}
`, e.Name, query)

	file := filepath.Join(path, "backend/internal/repository/"+strings.ToLower(e.Name)+"_repo.go")
	os.WriteFile(file, []byte(content), 0644)

}
