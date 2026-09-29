package ai

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// ValidateFunctionBody checks that body is a Go function body (the statements
// between the braces) before the healer or the rewriter splices it in.
//
// v26.58 (D048): the old checks were substring tests. "return nil, nil" failed
// as "too short" (no line break), and a comment or string that held "package "
// or "func " failed as a code block or full function, so the healer threw valid
// fixes away. The body is now parsed with go/parser inside the same wrapper
// that ast.parseBody uses when it rewrites the function. A body that parses to
// exactly one function is valid; a full function, a package clause, markdown,
// prose, or a body that closes the wrapper and adds declarations is not.
func ValidateFunctionBody(body string) error {

	if strings.TrimSpace(body) == "" {
		return errors.New("empty AI output")
	}

	src := "package main\nfunc _() {\n" + body + "\n}"
	file, err := parser.ParseFile(token.NewFileSet(), "", src, 0)
	if err == nil {
		if len(file.Decls) == 1 {
			if _, ok := file.Decls[0].(*ast.FuncDecl); ok {
				return nil
			}
		}
		return errors.New("AI output adds declarations outside the function body")
	}

	// Name the usual failure shapes, as the old messages did.
	trimmed := strings.TrimSpace(body)
	switch {
	case strings.Contains(body, "```"):
		return errors.New("AI returned markdown")
	case strings.HasPrefix(trimmed, "package "):
		return errors.New("invalid code block")
	case strings.HasPrefix(trimmed, "func "):
		return errors.New("AI returned full function")
	}
	return fmt.Errorf("AI output is not a Go function body: %w", err)
}
