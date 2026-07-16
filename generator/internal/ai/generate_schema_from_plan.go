// generator/internal/ai/generate_schema_from_plan.go — v23.1 (round 3):
// constrain plan->schema decoding to the entities JSON Schema; prompt now asks
// for fields as an array of {name,type} to match.
package ai

import "strings"

func GenerateSchemaFromPlan(plan string) string {
	prompt := buildSchemaFromPlanPrompt(plan)

	resp, err := AskLLMJSON(prompt, ModelFor(RolePlan), entitiesSchema)
	if err != nil {
		return ""
	}

	return extractJSON(resp)
}

func buildSchemaFromPlanPrompt(plan string) string {
	return `
Convert the following PLAN into a database schema.

Rules:
- Output ONLY valid JSON, no explanation, no markdown.
- Use field types only from: int, string, bool, time, float.
- "fields" MUST be an array of objects, each {"name": "...", "type": "..."}.

Format:
{
  "entities": [
    {
      "name": "User",
      "fields": [
        { "name": "id", "type": "int" },
        { "name": "email", "type": "string" }
      ]
    }
  ]
}

PLAN:
` + strings.TrimSpace(plan)
}
