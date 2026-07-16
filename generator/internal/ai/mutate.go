package ai

import (
	"fmt"
)

func RewriteFunctionWithAI(path, funcName, schema string) (string, error) {

	old := ExtractFunction(path, funcName)

	// ------------------------------------------
	// CASE 1: function not found → generate new
	// ------------------------------------------
	if old == "" {
		fmt.Println("⚠️ Function not found, AI generating new one...")

		prompt := BuildFunctionRewritePrompt(funcName, "empty", schema)

		resp, err := AskLLM(prompt)
		if err != nil {
			return "", err
		}

		resp = ExtractBodyOnly(resp)

		if err := ValidateFunctionBody(resp); err != nil {
			return "", err
		}

		return resp, nil
	}

	// ------------------------------------------
	// CASE 2: rewrite existing
	// ------------------------------------------
	prompt := BuildFunctionRewritePrompt(funcName, old, schema)

	resp, err := AskLLM(prompt)
	if err != nil {
		return "", err
	}

	resp = ExtractBodyOnly(resp)

	if err := ValidateFunctionBody(resp); err != nil {
		return "", err
	}

	return resp, nil
}
