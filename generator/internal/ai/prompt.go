package ai

import (
	"fmt"
)

func BuildFunctionRewritePrompt(funcName, oldCode, schema string) string {

	return fmt.Sprintf(`


You are a senior Go backend engineer.

TASK:
Rewrite the function "%s" based on new schema.

STRICT RULES:

* Output ONLY function body (no func signature)
* Do NOT explain
* Keep error handling
* Keep style clean

SCHEMA:
%s

CURRENT FUNCTION:
%s

OUTPUT:
`, funcName, schema, oldCode)
}
