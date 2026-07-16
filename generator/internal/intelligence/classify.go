package intelligence

import "strings"

// ─────────────────────────────────────────────
// ErrorType represents the class of compilation/runtime error.
// The classifier maps raw error strings to typed decisions so
// the repair prompt can be guided rather than generic.
// ─────────────────────────────────────────────
type ErrorType string

// RepairStrategy is the repair approach best suited for each error class.
type RepairStrategy string

const (
	// Error classification
	SyntaxError    ErrorType = "syntax"
	UndefinedError ErrorType = "undefined"
	TypeError      ErrorType = "type_mismatch"
	ImportError    ErrorType = "import"
	LogicError     ErrorType = "logic"
	UnknownError   ErrorType = "unknown"
)

const (
	// Repair strategies mapped to error types
	StrategySyntaxRepair  RepairStrategy = "syntax_repair"
	StrategyFixReference  RepairStrategy = "fix_reference"
	StrategyFixTypes      RepairStrategy = "fix_types"
	StrategyFixImports    RepairStrategy = "fix_imports"
	StrategyRewriteLogic  RepairStrategy = "rewrite_logic"
	StrategyGeneric       RepairStrategy = "generic_repair"
)

// ClassificationResult bundles the error type, strategy choice,
// and a human-readable hint injected into the AI repair prompt.
type ClassificationResult struct {
	ErrorType ErrorType
	Strategy  RepairStrategy
	// Hint is appended to the AI prompt to guide the repair.
	Hint string
}

// ClassifyError reads a Go compiler error string and returns a
// typed classification with a repair strategy and prompt hint.
//
// Pattern matching is intentionally broad — substring matching is
// sufficient because Go compiler output is highly structured.
func ClassifyError(errLog string) ClassificationResult {
	lower := strings.ToLower(errLog)

	switch {

	// ── IMPORT ERRORS (check before undefined — overlaps) ──
	case strings.Contains(lower, "imported and not used"):
		return ClassificationResult{
			ErrorType: ImportError,
			Strategy:  StrategyFixImports,
			Hint: "Remove the unused import statement from the import block. " +
				"Do not add a blank identifier (_) — delete the import line entirely.",
		}
	case strings.Contains(lower, "cannot find package") ||
		strings.Contains(lower, "no required module") ||
		strings.Contains(lower, "module provides package"):
		return ClassificationResult{
			ErrorType: ImportError,
			Strategy:  StrategyFixImports,
			Hint: "Add the missing import. Check the package path against the module name in go.mod. " +
				"Use the correct gen-system/generator/internal/... path format.",
		}

	// ── UNDEFINED / MISSING IDENTIFIER ──
	case strings.Contains(lower, "undefined: ") ||
		strings.Contains(lower, "not declared") ||
		strings.Contains(lower, "undeclared name"):
		return ClassificationResult{
			ErrorType: UndefinedError,
			Strategy:  StrategyFixReference,
			Hint: "An identifier is undefined. Either the import is missing, the type name is wrong, " +
				"or a variable/function was not declared. Check imports first, then check that " +
				"all referenced identifiers exist in scope.",
		}

	// ── TYPE ERRORS ──
	case strings.Contains(lower, "cannot use") ||
		strings.Contains(lower, "cannot convert") ||
		strings.Contains(lower, "incompatible type") ||
		strings.Contains(lower, "have (") ||
		strings.Contains(lower, "mismatched types"):
		return ClassificationResult{
			ErrorType: TypeError,
			Strategy:  StrategyFixTypes,
			Hint: "Type mismatch detected. Check function return types, parameter types, and type " +
				"assertions. Ensure struct field types match the schema definition.",
		}

	// ── SYNTAX ERRORS ──
	case strings.Contains(lower, "syntax error") ||
		strings.Contains(lower, "unexpected ") ||
		strings.Contains(lower, "expected ") ||
		strings.Contains(lower, "non-declaration statement"):
		return ClassificationResult{
			ErrorType: SyntaxError,
			Strategy:  StrategySyntaxRepair,
			Hint: "Syntax error in the function body. Check for: unmatched braces/parentheses, " +
				"missing semicolons, incorrect if/for/switch syntax, or misplaced return statements.",
		}

	// ── LOGIC / SEMANTIC ERRORS ──
	case strings.Contains(lower, "declared and not used") ||
		strings.Contains(lower, "no new variables") ||
		strings.Contains(lower, "not enough return values") ||
		strings.Contains(lower, "too many return values") ||
		strings.Contains(lower, "multiple-value") && strings.Contains(lower, "used in single-value"):
		return ClassificationResult{
			ErrorType: LogicError,
			Strategy:  StrategyRewriteLogic,
			Hint: "Semantic error: either a variable is declared but never used, return values " +
				"don't match the function signature, or a multi-value expression is misused. " +
				"Rewrite the function body to match the exact return signature.",
		}

	default:
		return ClassificationResult{
			ErrorType: UnknownError,
			Strategy:  StrategyGeneric,
			Hint: "Review the function body for correctness. Ensure all types, identifiers, " +
				"and control flow are valid Go.",
		}
	}
}

// StrategyDescription returns a one-line description for logging.
func (r ClassificationResult) StrategyDescription() string {
	switch r.Strategy {
	case StrategySyntaxRepair:
		return "syntax repair (check braces/keywords)"
	case StrategyFixReference:
		return "reference fix (imports + identifier scope)"
	case StrategyFixTypes:
		return "type correction (signatures + assertions)"
	case StrategyFixImports:
		return "import fix (add missing / remove unused)"
	case StrategyRewriteLogic:
		return "logic rewrite (return values + variable usage)"
	default:
		return "generic AI repair"
	}
}
