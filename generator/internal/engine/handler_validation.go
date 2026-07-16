package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"gen-system/generator/internal/schema"
)

func genHandlerWithValidation(e schema.Entity, path string) {

	content := fmt.Sprintf(`


func Create%[1]s(w http.ResponseWriter, r *http.Request) {


var input map[string]interface{}
json.NewDecoder(r.Body).Decode(&input)

if err := validation.Validate%[1]s(input); err != nil {
	http.Error(w, err.Error(), 400)
	return
}

// TODO: insert logic

w.WriteHeader(201)


}
`, e.Name)

	file := filepath.Join(path, "backend/internal/handler/"+e.Name+"_create.go")
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	f.WriteString(content)
	f.Close()

}
