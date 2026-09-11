package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func genHandlerFull(e schema.Entity, path string) {

	content := fmt.Sprintf(`


func GetAll%[1]s(w http.ResponseWriter, r *http.Request) {


limit := 20
offset := 0

data, err := service.GetAll%[1]s(db, limit, offset)
if err != nil {
	http.Error(w, err.Error(), 500)
	return
}

json.NewEncoder(w).Encode(data)


}
`, e.Name)

	file := filepath.Join(path, "backend/internal/handler/"+e.Name+"_handler.go")
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	f.WriteString(content)
	f.Close()

}
