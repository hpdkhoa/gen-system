package intelligence_test

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/intelligence"
)

// TestClassifyError_ImportError verifies "imported and not used" classification.
func TestClassifyError_ImportError_UnusedImport(t *testing.T) {
	result := intelligence.ClassifyError(`./handler.go:5:2: "fmt" imported and not used`)

	if result.ErrorType != intelligence.ImportError {
		t.Errorf("want ImportError, got %s", result.ErrorType)
	}
	if result.Strategy != intelligence.StrategyFixImports {
		t.Errorf("want StrategyFixImports, got %s", result.Strategy)
	}
	if result.Hint == "" {
		t.Error("hint should not be empty")
	}
}

// TestClassifyError_ImportError_MissingModule verifies "no required module" classification.
func TestClassifyError_ImportError_MissingModule(t *testing.T) {
	result := intelligence.ClassifyError(`no required module provides package github.com/lib/pq`)

	if result.ErrorType != intelligence.ImportError {
		t.Errorf("want ImportError, got %s", result.ErrorType)
	}
}

// TestClassifyError_UndefinedError verifies "undefined:" classification.
func TestClassifyError_UndefinedError(t *testing.T) {
	result := intelligence.ClassifyError(`./repo.go:12:5: undefined: User`)

	if result.ErrorType != intelligence.UndefinedError {
		t.Errorf("want UndefinedError, got %s", result.ErrorType)
	}
	if result.Strategy != intelligence.StrategyFixReference {
		t.Errorf("want StrategyFixReference, got %s", result.Strategy)
	}
}

// TestClassifyError_TypeError verifies "cannot use" classification.
func TestClassifyError_TypeError(t *testing.T) {
	result := intelligence.ClassifyError(`./service.go:30:15: cannot use "text" (untyped string constant) as int value`)

	if result.ErrorType != intelligence.TypeError {
		t.Errorf("want TypeError, got %s", result.ErrorType)
	}
	if result.Strategy != intelligence.StrategyFixTypes {
		t.Errorf("want StrategyFixTypes, got %s", result.Strategy)
	}
}

// TestClassifyError_SyntaxError verifies "syntax error" classification.
func TestClassifyError_SyntaxError(t *testing.T) {
	result := intelligence.ClassifyError(`./handler.go:45:1: syntax error: unexpected EOF, expecting }`)

	if result.ErrorType != intelligence.SyntaxError {
		t.Errorf("want SyntaxError, got %s", result.ErrorType)
	}
	if result.Strategy != intelligence.StrategySyntaxRepair {
		t.Errorf("want StrategySyntaxRepair, got %s", result.Strategy)
	}
}

// TestClassifyError_LogicError_DeclaredNotUsed verifies logic error classification.
func TestClassifyError_LogicError_DeclaredNotUsed(t *testing.T) {
	result := intelligence.ClassifyError(`./service.go:10:2: x declared and not used`)

	if result.ErrorType != intelligence.LogicError {
		t.Errorf("want LogicError, got %s", result.ErrorType)
	}
	if result.Strategy != intelligence.StrategyRewriteLogic {
		t.Errorf("want StrategyRewriteLogic, got %s", result.Strategy)
	}
}

// TestClassifyError_UnknownError verifies fallback classification.
func TestClassifyError_UnknownError(t *testing.T) {
	result := intelligence.ClassifyError(`some completely unrecognized error message`)

	if result.ErrorType != intelligence.UnknownError {
		t.Errorf("want UnknownError, got %s", result.ErrorType)
	}
	if result.Strategy != intelligence.StrategyGeneric {
		t.Errorf("want StrategyGeneric, got %s", result.Strategy)
	}
}

// TestClassifyError_ImportCheckedBeforeUndefined verifies ordering: import errors
// are checked before undefined errors (they can overlap).
func TestClassifyError_ImportCheckedBeforeUndefined(t *testing.T) {
	// This message contains both "imported and not used" (import) and could
	// match "undefined" heuristics — import should win because it's checked first.
	result := intelligence.ClassifyError(`"os" imported and not used`)
	if result.ErrorType != intelligence.ImportError {
		t.Errorf("import should be classified before undefined, got %s", result.ErrorType)
	}
}

// TestClassifyError_HintAlwaysPresent verifies that every classification
// produces a non-empty hint for AI prompt injection.
func TestClassifyError_HintAlwaysPresent(t *testing.T) {
	cases := []string{
		`"fmt" imported and not used`,
		`undefined: Foo`,
		`cannot use x as int`,
		`syntax error: unexpected }`,
		`declared and not used`,
		`completely unknown error`,
	}
	for _, c := range cases {
		result := intelligence.ClassifyError(c)
		if result.Hint == "" {
			t.Errorf("hint should not be empty for: %q (type=%s)", c, result.ErrorType)
		}
	}
}

// TestStrategyDescription_ReturnsNonEmpty verifies StrategyDescription()
// doesn't return an empty string for any known strategy.
func TestStrategyDescription_ReturnsNonEmpty(t *testing.T) {
	strats := []intelligence.ClassificationResult{
		intelligence.ClassifyError(`"fmt" imported and not used`),
		intelligence.ClassifyError(`undefined: X`),
		intelligence.ClassifyError(`cannot use x as int`),
		intelligence.ClassifyError(`syntax error: unexpected }`),
		intelligence.ClassifyError(`x declared and not used`),
		intelligence.ClassifyError(`unknown error text`),
	}
	for _, r := range strats {
		desc := r.StrategyDescription()
		if desc == "" {
			t.Errorf("StrategyDescription empty for strategy %s", r.Strategy)
		}
	}
}
