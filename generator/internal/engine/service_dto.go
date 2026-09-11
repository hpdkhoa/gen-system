package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func genServiceDTO(e schema.Entity, path string) {

	content := fmt.Sprintf(`


package service

func GetAll%[1]sDTO() interface{} {
// TODO: map DB rows → DTO with nested relations
return nil
}
`, e.Name)

	file := filepath.Join(path, "backend/internal/service/"+e.Name+"_service.go")
	os.WriteFile(file, []byte(content), 0644)

}
