package ai

import (
	"strings"
)

func GeneratePlan(input string) string {
	prompt := buildPlanPrompt(input)

	resp, err := AskLLM(prompt)
	if err != nil {
		return ""
	}

	return extractJSON(resp)
}

func buildPlanPrompt(input string) string {
	return `
You are a backend system architect.

Convert the following idea into a structured PLAN.

Rules:
- Output ONLY valid JSON
- No explanation
- Be concise but complete

Format:
{
  "features": [
    {
      "name": "...",
      "entities": [],
      "actions": [],
      "relations": []
    }
  ],
  "entities": [
    {
      "name": "...",
      "fields": []
    }
  ],
  "notes": []
}

Idea:
` + strings.TrimSpace(input)
}
