// generator/internal/ai/generate_schema_from_plan.go — v23.1 (round 3):
// constrain plan->schema decoding to the entities JSON Schema; prompt now asks
// for fields as an array of {name,type} to match.
package ai

import "strings"

// GenerateSchemaFromPlan asks the model to turn a plan into a schema.
// v26.58 (D049): it returned "" on a model error and dropped the error, so
// gen schema wrote an empty file and reported success. It now returns the
// error; callers that want the old fallback treat an error like "".
func GenerateSchemaFromPlan(plan string) (string, error) {
	prompt := buildSchemaFromPlanPrompt(plan)

	resp, err := AskLLMJSON(prompt, ModelFor(RolePlan), entitiesSchema)
	if err != nil {
		return "", err
	}

	return extractJSON(resp), nil
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
