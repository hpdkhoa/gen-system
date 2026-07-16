package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"gen-system/generator/internal/schema"
)

func genHandlerDTO(e schema.Entity, path string) {

	content := fmt.Sprintf(`


func GetAll%[1]s(w http.ResponseWriter, r *http.Request) {
data := service.GetAll%[1]sDTO()
json.NewEncoder(w).Encode(data)
}
`, e.Name)

	file := filepath.Join(path, "backend/internal/handler/"+e.Name+"_handler.go")
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	f.WriteString(content)
	f.Close()

}
