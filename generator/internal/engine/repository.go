package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gen-system/generator/internal/schema"
)

func genRepository(s schema.Entity, path string) {
	file := filepath.Join(path, "backend/internal/repository/"+strings.ToLower(s.Name)+"_repo.go")

	var fields []string
	var placeholders []string
	var scan []string

	for i, f := range s.Fields {
		fields = append(fields, strings.ToLower(f.Name))
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		scan = append(scan, "&item."+f.Name)
	}

	content := fmt.Sprintf(`


package repository

import (
"backend/internal/db"
"backend/internal/model"
)

func Create%[1]s(item *model.%[1]s) error {
query := "INSERT INTO %[2]s (%[3]s) VALUES (%[4]s)"
_, err := db.DB.Exec(query, %[5]s)
return err
}

func GetAll%[1]s() ([]model.%[1]s, error) {
rows, err := db.DB.Query("SELECT %[3]s FROM %[2]s")
if err != nil {
return nil, err
}
defer rows.Close()


var list []model.%[1]s

for rows.Next() {
	var item model.%[1]s
	err := rows.Scan(%[6]s)
	if err != nil {
		return nil, err
	}
	list = append(list, item)
}
return list, nil


}
`,
		s.Name,
		strings.ToLower(s.Name),
		strings.Join(fields, ","),
		strings.Join(placeholders, ","),
		buildInsertArgs(s),
		strings.Join(scan, ","),
	)

	os.WriteFile(file, []byte(content), 0644)

}

func buildInsertArgs(s schema.Entity) string {
	var args []string
	for _, f := range s.Fields {
		args = append(args, "item."+f.Name)
	}
	return strings.Join(args, ",")
}
