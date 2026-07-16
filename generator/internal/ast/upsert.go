package ast

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// UpsertFunction:
// - if function exists → rewrite body
// - else → create new
func UpsertFunction(path, funcName, fullCode string, body string) {

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)

	if err != nil {
		appendRaw(path, fullCode)
		return
	}

	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Name.Name == funcName {
				RewriteFunctionBody(path, funcName, body)
				return
			}
		}
	}

	appendRaw(path, fullCode)

}
