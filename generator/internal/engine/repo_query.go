package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func genRepoQuery(e schema.Entity, path string) {

	table := strings.ToLower(e.Name)

	content := fmt.Sprintf(`


package repository

import (
	"os"
"app/internal/query"
)

func GetAll%[1]s(db *sql.DB, limit, offset int) (*sql.Rows, error) {


q := query.New("%[2]s").
	Select("%[2]s.*")


`, e.Name, table)

	// joins
	for _, r := range e.Relations {
		if r.Type == "belongsTo" {
			content += fmt.Sprintf(`
q.Join("LEFT JOIN %s ON %s.%s = %s.id")
q.Select("%s.*")


`, strings.ToLower(r.Target), table, strings.ToLower(r.ForeignKey), strings.ToLower(r.Target), strings.ToLower(r.Target))
		}
	}

	content += `
q.Paginate(limit, offset)

queryStr := q.Build()

return db.Query(queryStr)


}
`

	file := filepath.Join(path, "backend/internal/repository/"+table+"_repo.go")
	os.WriteFile(file, []byte(content), 0644)

}
