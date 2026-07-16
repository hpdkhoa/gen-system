package cli

import (
	"fmt"
	"os"

	"gen-system/generator/internal/engine"
	"gen-system/generator/internal/schema"
)

func EvolveFromSchema() {

	if len(os.Args) < 3 {
		fmt.Println("gen evolve <repo-path> --schema schema.json")
		return
	}

	path := os.Args[2]
	schemaPath := GetSchemaPath()

	if schemaPath == "" {
		fmt.Println("❌ Missing --schema")
		return
	}

	fmt.Println("📂 Loading schema:", schemaPath)

	sfile, err := schema.LoadFromFile(schemaPath)
	if err != nil {
		fmt.Println("❌ Failed to load schema:", err)
		return
	}

	engine.RunAll(sfile.Entities, path)

}
