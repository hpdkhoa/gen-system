package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gen-system/generator/internal/schema"
)

func genService(s schema.Entity, path string) {
	file := filepath.Join(path, "backend/internal/service/"+strings.ToLower(s.Name)+"_service.go")

	content := fmt.Sprintf(`


package service

import (
"backend/internal/model"
"backend/internal/repository"
)

func Create%[1]s(item *model.%[1]s) error {
return repository.Create%[1]s(item)
}

func GetAll%[1]s() ([]model.%[1]s, error) {
return repository.GetAll%[1]s()
}
`, s.Name)

	os.WriteFile(file, []byte(content), 0644)

}
