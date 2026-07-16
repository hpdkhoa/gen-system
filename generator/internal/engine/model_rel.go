package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gen-system/generator/internal/schema"
)

func genModelWithRelations(e schema.Entity, path string) {

	var fields []string

	fields = append(fields, "ID int `json:\"id\"`")

	for _, f := range e.Fields {
		fields = append(fields,
			fmt.Sprintf("%s %s `json:\"%s\"`",
				f.Name,
				mapGoType(f.Type),
				strings.ToLower(f.Name),
			))
	}

	// relation fields
	for _, r := range e.Relations {

		if r.Type == "belongsTo" {
			fields = append(fields,
				fmt.Sprintf("%s int `json:\"%s\"`",
					r.ForeignKey,
					strings.ToLower(r.ForeignKey),
				))
		}

		if r.Type == "hasMany" {
			fields = append(fields,
				fmt.Sprintf("%s []%s `json:\"%s\"`",
					r.Target+"s",
					r.Target,
					strings.ToLower(r.Target)+"s",
				))
		}
	}

	content := fmt.Sprintf(`package model


type %s struct {
%s
}
`, e.Name, strings.Join(fields, "\n"))

	file := filepath.Join(path, "backend/internal/model/"+strings.ToLower(e.Name)+".go")
	os.WriteFile(file, []byte(content), 0644)

}
