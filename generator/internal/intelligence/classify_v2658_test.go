// D078: ClassifyError sent return count errors ("not enough return values",
// "too many return values") to type_mismatch, because the compiler adds a
// "have (...)" line and the "have (" type rule ran before the logic rule.
package intelligence_test

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/intelligence"
)

func TestClassifyError_ReturnCountIsLogic(t *testing.T) {
	msgs := []string{
		"./service.go:12:2: not enough return values\n\thave (error)\n\twant (*User, error)",
		"./service.go:14:9: too many return values\n\thave (*User, error, error)\n\twant (*User, error)",
		"./repo.go:30:3: not enough return values\n\thave ()\n\twant (int, error)",
	}
	for _, m := range msgs {
		got := intelligence.ClassifyError(m)
		if got.ErrorType != intelligence.LogicError || got.Strategy != intelligence.StrategyRewriteLogic {
			t.Errorf("ClassifyError(%q) = %s / %s, want logic / rewrite_logic",
				m, got.ErrorType, got.Strategy)
		}
	}
}

// A wrong argument count also carries "have (" lines and must stay a type error.
func TestClassifyError_ArgumentCountStaysType(t *testing.T) {
	m := "./h.go:8:14: not enough arguments in call to svc.Get\n\thave (context.Context)\n\twant (context.Context, int)"
	if got := intelligence.ClassifyError(m); got.ErrorType != intelligence.TypeError {
		t.Errorf("got %s, want type_mismatch", got.ErrorType)
	}
}

// Syntax errors that say "unexpected" still classify as syntax after the
// redundant "unexpected " test is gone.
func TestClassifyError_UnexpectedIsSyntax(t *testing.T) {
	m := "./h.go:3:1: syntax error: unexpected newline, expected comma or )"
	if got := intelligence.ClassifyError(m); got.ErrorType != intelligence.SyntaxError {
		t.Errorf("got %s, want syntax", got.ErrorType)
	}
	m = "./h.go:3:1: unexpected semicolon or newline before {"
	if got := intelligence.ClassifyError(m); got.ErrorType != intelligence.SyntaxError {
		t.Errorf("got %s, want syntax", got.ErrorType)
	}
}
