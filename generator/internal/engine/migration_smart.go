package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gen-system/generator/internal/schema/smartdiff"
)

func genSmartMigration(entity string, diff smartdiff.Diff, path string) {

	table := strings.ToLower(entity)
	file := filepath.Join(path, "db/migrations/"+timestamp()+"_smart_"+table+".sql")

	var queries []string

	// rename (Postgres supports this)
	for _, r := range diff.RenamedFields {
		queries = append(queries,
			fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;",
				table,
				strings.ToLower(r.From.Name),
				strings.ToLower(r.To.Name),
			))
	}

	// relation add
	for _, r := range diff.RelationAdded {
		queries = append(queries,
			fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s INTEGER REFERENCES %s(id);",
				table,
				strings.ToLower(r.ForeignKey),
				strings.ToLower(r.Target),
			))
	}

	// relation remove
	for _, r := range diff.RelationRemove {
		queries = append(queries,
			fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;",
				table,
				strings.ToLower(r.ForeignKey),
			))
	}

	if len(queries) == 0 {
		return
	}

	os.WriteFile(file, []byte(strings.Join(queries, "\n")), 0644)

}
