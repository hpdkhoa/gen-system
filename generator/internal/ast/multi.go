// multi.go — v23.12 (Phase 4): LangJava added to DetectLanguage + RewriteFunction (parse-only).
// Patched in Phase A (revision: includes COBOL detection):
//   - Added typed error ErrRewriterNotImplemented so callers can distinguish
//     "we don't know this language" from "we read it but can't write it yet."
//   - DetectLanguage now recognizes .cob/.cbl/.cpy/.cobol → LangCOBOL.
//   - LangTS, LangCAGen, and LangCOBOL return ErrRewriterNotImplemented
//     explicitly instead of falling through to the generic LangUnknown default.
//   - LangCOBOL is declared in cobol_frontend.go (same package); referencing
//     it here is safe.
package ast

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Language represents a supported source language for AST rewriting.
type Language string

const (
	LangGo      Language = "go"
	LangPython  Language = "python"
	LangTS      Language = "typescript" // v15 addition
	LangCAGen   Language = "cagen"      // v15 addition
	LangUnknown Language = "unknown"
)

// ErrRewriterNotImplemented signals that the language is recognized by the
// system but no AST-level rewriter exists for it yet. Read-only frontends
// (TS, CA Gen as of Phase A) return this from RewriteFunction.
//
// Callers can do:
//
//	if errors.Is(err, ast.ErrRewriterNotImplemented) { ... }
//
// to handle the "parse-yes, rewrite-no" case distinctly from the
// "unknown extension" case.
var ErrRewriterNotImplemented = errors.New("ast: rewriter not implemented for language")

// DetectLanguage infers the language from a file's extension.
func DetectLanguage(filePath string) Language {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return LangGo
	case ".py", ".pyw":
		return LangPython
	case ".ts", ".tsx": // v15
		return LangTS
	case ".ad", ".cagen", ".gel": // v15
		return LangCAGen
	case ".cob", ".cbl", ".cpy", ".cobol": // Phase A: COBOL detection (parse-only)
		return LangCOBOL
	case ".java": // v23.12 Phase 4: Java 17 detection (parse-only)
		return LangJava
	default:
		return LangUnknown
	}
}

// RewriteFunction is the unified multi-language dispatcher for function body rewriting.
// It routes to the correct AST backend based on the file extension:
//   - .go  → RewriteFunctionBody (go/ast)
//   - .py  → RewritePythonFunction (python3 subprocess + ast module)
//   - .ts/.tsx, .ad/.cagen/.gel → ErrRewriterNotImplemented (parse-only as of Phase A)
//   - other → typed "unsupported language" error
//
// This is the function the healer should call instead of RewriteFunctionBody
// directly, so that the healing pipeline works across all supported projects.
func RewriteFunction(filePath, funcName, newBody string) error {
	lang := DetectLanguage(filePath)
	switch lang {
	case LangGo:
		return RewriteFunctionBody(filePath, funcName, newBody)
	case LangPython:
		if !CheckPython3Available() {
			return fmt.Errorf("python3 >= 3.9 required for Python AST rewriting — not found on PATH")
		}
		return RewritePythonFunction(filePath, funcName, newBody)
	case LangTS, LangCAGen, LangCOBOL, LangJava:
		return fmt.Errorf("%w: %s (file: %s)", ErrRewriterNotImplemented, lang, filePath)
	default:
		return fmt.Errorf("unsupported language for AST rewriting: %s (file: %s)",
			filepath.Ext(filePath), filePath)
	}
}
