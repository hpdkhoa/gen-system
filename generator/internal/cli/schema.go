package cli

import (
	"fmt"
	"os"

	"gen-system/generator/internal/ai"
)

func Schema() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: gen schema <plan.json>")
		return
	}

	path := os.Args[2]

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("Error reading plan:", err)
		return
	}

	schema := ai.GenerateSchemaFromPlan(string(data))

	err = os.WriteFile("schema.generated.json", []byte(schema), 0644)
	if err != nil {
		fmt.Println("Error writing schema:", err)
		return
	}

	fmt.Println("Schema generated → schema.generated.json")
}
