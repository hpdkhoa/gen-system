package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gen-system/generator/internal/schema"
)

func genTableWithRelations(e schema.Entity, path string) {

	table := strings.ToLower(e.Name)

	var cols []string
	cols = append(cols, "id SERIAL PRIMARY KEY")

	for _, f := range e.Fields {
		cols = append(cols,
			fmt.Sprintf("%s %s",
				strings.ToLower(f.Name),
				mapSQLType(f.Type),
			))
	}

	// relations → foreign keys
	for _, r := range e.Relations {
		if r.Type == "belongsTo" {
			cols = append(cols,
				fmt.Sprintf("%s INTEGER REFERENCES %s(id)",
					strings.ToLower(r.ForeignKey),
					strings.ToLower(r.Target),
				))
		}
	}

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n%s\n);",
		table,
		strings.Join(cols, ",\n"),
	)

	file := filepath.Join(path, "db/migrations/init_"+table+".sql")
	os.WriteFile(file, []byte(sql), 0644)

}
