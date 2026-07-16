package core

import (
	"fmt"
	"os"
	"path/filepath"

	"gen-system/generator/internal/engine"
	"gen-system/generator/internal/schema"
)

func InitProject(path string) {
	fmt.Println("🚀 Init project:", path)

	dirs := []string{
		"backend/cmd",
		"backend/internal/model",
		"backend/internal/handler",
		"backend/internal/service",
		"backend/internal/repository",
		"backend/internal/router",
		"frontend/src/pages",
		"frontend/src/components",
		"frontend/src/api",
		"mobile",
		"db/migrations",
	}

	for _, d := range dirs {
		os.MkdirAll(filepath.Join(path, d), 0755)
	}

	engine.WriteBaseProject(path)

}

func Generate(entity, schemaStr, path string) {
	fmt.Println("⚡ Generate:", entity)

	s := schema.Parse(entity, schemaStr)

	entities := []schema.Entity{
		{
			Name:   s.Name,
			Fields: s.Fields,
		},
	}

	engine.RunAll(entities, path)

}

func Sync(path string) {
	fmt.Println("🔁 Sync project:", path)
	engine.Sync(path)
}

func Doctor(path string) {
	fmt.Println("🩺 Doctor:", path)
	engine.Doctor(path)
}

func Plan(schemaStr string) {
	fmt.Println("🧠 Planning...")
	fmt.Println(engine.Plan(schemaStr))
}

func ApplySchema(schemaStr string, path string) {
	fmt.Println("⚡ ApplySchema")

	// Parse full schema file
	sf := schema.ParseFile(schemaStr)

	// Run full pipeline
	engine.RunAll(sf.Entities, path)
}
