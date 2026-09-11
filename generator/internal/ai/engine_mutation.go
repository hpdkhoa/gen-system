package ai

import (
	"fmt"
	"path/filepath"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

// MutateHandler uses the AI to rewrite the GetAll handler function body.
// Previously lived in engine/engine_ai.go (package engine).
// Moved here because: the engine package should never import ai.
//
// Called by: gen ai <path> CLI command (not by engine.RunAll).
func MutateHandler(entityName, path string) {
	file := filepath.Join(path, "backend/internal/handler/"+entityName+"_handler.go")

	// ✅ ensure valid Go file exists before mutation
	ast.EnsureGoFile(file, "handler")

	funcName := "GetAll" + entityName

	trace := newTraceID()
	fmt.Println("🧬 AI mutation trace:", trace)

	newBody, err := RewriteFunctionWithAI(file, funcName, entityName)
	fmt.Println("🔧 Rewriting function with preserved signature:", funcName)

	if err != nil {
		fmt.Println("AI skip:", err, "trace:", trace)
		return
	}

	if err := ast.RewriteFunctionBody(file, funcName, newBody); err != nil {
		fmt.Println("AST rewrite failed (skipped):", err, "trace:", trace)
	}
}

// RunAIMutation is the entry point for `gen ai <path>` — runs AI mutation
// for a single entity. engine package is not involved.
func RunAIMutation(entityName, path string) {
	fmt.Println("🤖 AI mutation layer...")
	MutateHandler(entityName, path)
}

// newTraceID generates a short identifier for logging.
func newTraceID() string {
	// simple counter-based ID — replace with UUID if needed
	return fmt.Sprintf("ai-%d", traceCounter())
}

var _traceCount int

func traceCounter() int {
	_traceCount++
	return _traceCount
}
