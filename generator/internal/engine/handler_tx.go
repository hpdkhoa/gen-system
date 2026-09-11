package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func genHandlerTx(e schema.Entity, path string) {

	content := fmt.Sprintf(`


func Create%[1]s(w http.ResponseWriter, r *http.Request) {


var input map[string]interface{}
json.NewDecoder(r.Body).Decode(&input)

if err := service.Create%[1]s(db, input); err != nil {
	http.Error(w, err.Error(), 500)
	return
}

w.WriteHeader(201)


}
`, e.Name)

	file := filepath.Join(path, "backend/internal/handler/"+e.Name+"_create.go")
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	f.WriteString(content)
	f.Close()

}
