// generator/internal/ai/validate_v2658_test.go — v26.58.
//
// D048: ValidateFunctionBody used substring tests. It rejected the valid one
// line body "return nil, nil" as "too short", and rejected valid bodies whose
// comments or strings held the words package or func. The healer threw those
// valid fixes away. The body is now parsed as a Go function body.
package ai

import "testing"

func TestValidateFunctionBody_ValidBodiesPass(t *testing.T) {
	cases := map[string]string{
		"one line":        "return nil, nil",
		"package in text": "// reuse the package level store\nreturn store.Get(id)",
		"func in string":  "return nil, errors.New(\"no such func here\")",
		"closure":         "sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })\nreturn xs",
		"multi line":      "if id == \"\" {\n\treturn nil, ErrNotFound\n}\nreturn store[id], nil",
		"package in var":  "x := \"package main\"\n_ = x",
	}
	for name, body := range cases {
		if err := ValidateFunctionBody(body); err != nil {
			t.Errorf("%s: valid body %q rejected: %v", name, body, err)
		}
	}
}

func TestValidateFunctionBody_BadBodiesFail(t *testing.T) {
	cases := map[string]string{
		"empty":         "   \n",
		"full function": "func Get(id string) (*Order, error) {\n\treturn nil, nil\n}",
		"markdown":      "```go\nreturn nil, nil\n```",
		"package":       "package main\n\nfunc Get() {}",
		"not go":        "Here is the fixed code: return the order",
		"escapes body":  "return nil\n}\n\nfunc Extra() {",
		"unbalanced":    "if x {\nreturn nil",
	}
	for name, body := range cases {
		if err := ValidateFunctionBody(body); err == nil {
			t.Errorf("%s: invalid body %q accepted", name, body)
		}
	}
}
