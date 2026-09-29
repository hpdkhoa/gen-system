package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/ai"
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

	// v26.58 (D049): a model error or an empty answer wrote an empty
	// schema.generated.json and printed success. Stop with the reason instead.
	schema, err := ai.GenerateSchemaFromPlan(string(data))
	if err != nil {
		fmt.Println("❌ Schema generation failed:", err)
		return
	}
	if strings.TrimSpace(schema) == "" {
		fmt.Println("❌ Schema generation failed: the model returned no schema")
		return
	}

	err = os.WriteFile("schema.generated.json", []byte(schema), 0644)
	if err != nil {
		fmt.Println("Error writing schema:", err)
		return
	}

	fmt.Println("Schema generated → schema.generated.json")
}
