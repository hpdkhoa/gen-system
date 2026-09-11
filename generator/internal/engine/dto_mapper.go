package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func genDTOMapper(e schema.Entity, path string) {

	table := strings.ToLower(e.Name)

	content := fmt.Sprintf(`
package service

import (
	"database/sql"
	"backend/internal/dto"
)

func map%[1]sRows(rows *sql.Rows) ([]dto.%[1]sDTO, error) {

	defer rows.Close()

	var result []dto.%[1]sDTO

	for rows.Next() {

		var item dto.%[1]sDTO
`, e.Name)

	// variables
	for _, f := range e.Fields {
		content += fmt.Sprintf("\t\tvar %s %s\n", strings.ToLower(f.Name), mapGoType(f.Type))
	}

	// scan
	content += "\n\t\terr := rows.Scan("

	var scanVars []string
	for _, f := range e.Fields {
		scanVars = append(scanVars, "&"+strings.ToLower(f.Name))
	}

	content += strings.Join(scanVars, ", ")
	content += ")\n"

	content += `
		if err != nil {
			return nil, err
		}
`

	// mapping
	for _, f := range e.Fields {
		content += fmt.Sprintf(
			"\t\titem.%s = %s\n",
			f.Name,
			strings.ToLower(f.Name),
		)
	}

	content += `
		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}
`

	file := filepath.Join(path, "backend/internal/service/"+table+"_mapper.go")
	os.WriteFile(file, []byte(content), 0644)
}
