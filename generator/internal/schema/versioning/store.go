package versioning

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

const SNAPSHOT_FILE = ".gen/schema_snapshot.json"

func SaveSnapshot(basePath string, s schema.Entity) {
	os.MkdirAll(filepath.Join(basePath, ".gen"), 0755)

	data, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(filepath.Join(basePath, SNAPSHOT_FILE), data, 0644)

}

func LoadSnapshot(basePath string) (*schema.Entity, error) {
	file := filepath.Join(basePath, SNAPSHOT_FILE)

	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var s schema.Entity
	json.Unmarshal(data, &s)

	return &s, nil

}
