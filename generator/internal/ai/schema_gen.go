// generator/internal/ai/schema_gen.go — v23.1 (round 3): GenerateSchema now
// constrains the model via AskLLMJSON(entitiesSchema) so output is valid JSON
// matching {entities:[{name,fields[]}]}. (round 1: model resolved via Model().)
package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GenerateSchema converts a natural language idea into a strict JSON schema
// compatible with the gen-system engine.
func GenerateSchema(input string) string {
	if strings.TrimSpace(input) == "" {
		return "{}"
	}

	prompt := buildSchemaPrompt(input)

	// v23.1 round 3: constrain the model to the entities JSON Schema so it
	// physically cannot emit malformed JSON. Falls back to a plain retry.
	resp, err := AskLLMJSON(prompt, ModelFor(RolePlan), entitiesSchema)
	if err != nil || !isValidJSON(extractJSON(resp)) {
		fmt.Println("⚠️ first attempt did not yield valid JSON, retrying once...")
		resp, err = AskLLMJSON(prompt, ModelFor(RolePlan), entitiesSchema)
		if err != nil {
			fmt.Println("❌ model call failed:", err)
			return "{}"
		}
	}

	// Ensure clean JSON output
	clean := extractJSON(resp)

	if !isValidJSON(clean) {
		fmt.Println("❌ invalid schema generated")
		return "{}"
	}

	return clean
}

// -------------------------
// Prompt Engineering Layer
// -------------------------
func buildSchemaPrompt(idea string) string {
	return fmt.Sprintf(`
You are a strict schema compiler for a Go full-stack generator.

Convert the idea into VALID JSON ONLY.

SCHEMA FORMAT:
{
  "entities": [
    {
      "name": "Entity",
      "fields": [
        {"name": "id", "type": "int|string|bool|float"},
        {"name": "field", "type": "string"}
      ]
    }
  ]
}

RULES:
- Output ONLY valid JSON
- No markdown
- No explanation
- At least 1 entity
- Prefer relational design if applicable

IDEA:
%s
`, idea)
}

// -------------------------
// Model abstraction layer
// -------------------------
func AskLLMWithModel(prompt, model string) (string, error) {
	return AskLLMWithModelRaw(prompt, model)
}

// -------------------------
// JSON safety utilities
// -------------------------
func isValidJSON(s string) bool {
	var js interface{}
	return json.Unmarshal([]byte(s), &js) == nil
}

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")

	if start == -1 || end == -1 || end <= start {
		return "{}"
	}

	return s[start : end+1]
}
