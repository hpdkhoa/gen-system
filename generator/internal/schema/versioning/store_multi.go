package versioning

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func SaveEntitySnapshot(basePath string, entity schema.Entity) {

	dir := filepath.Join(basePath, ".gen", "entities")
	os.MkdirAll(dir, 0755)

	data, _ := json.MarshalIndent(entity, "", "  ")
	file := filepath.Join(dir, entity.Name+".json")

	os.WriteFile(file, data, 0644)

}

func LoadEntitySnapshot(basePath, name string) (*schema.Entity, error) {

	file := filepath.Join(basePath, ".gen", "entities", name+".json")

	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var s schema.Entity
	json.Unmarshal(data, &s)

	return &s, nil

}
