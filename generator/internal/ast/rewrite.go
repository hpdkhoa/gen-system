package ast

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
)

// RewriteFunctionBody preserves signature and replaces ONLY body
func RewriteFunctionBody(path, funcName, newBody string) error {

	fset := token.NewFileSet()

	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	block, err := parseBody(newBody)
	if err != nil {
		return err
	}

	found := false

	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		if fn.Name.Name == funcName {
			// ✅ preserve signature, only replace body
			fn.Body = block
			found = true
			break
		}
	}

	// ------------------------------------------
	// If function not found → create stub
	// ------------------------------------------
	if !found {
		fn := &ast.FuncDecl{
			Name: ast.NewIdent(funcName),
			Type: &ast.FuncType{
				Params:  &ast.FieldList{},
				Results: &ast.FieldList{},
			},
			Body: block,
		}

		node.Decls = append(node.Decls, fn)
	}

	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, node); err != nil {
		return err
	}

	return os.WriteFile(path, buf.Bytes(), 0644)
}

// ------------------------------------------
// Extract full function signature as string
// ------------------------------------------
func ExtractFunctionSignature(path, funcName string) string {

	fset := token.NewFileSet()

	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return ""
	}

	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		if fn.Name.Name == funcName {

			var buf bytes.Buffer

			// clone function WITHOUT body
			tmp := *fn
			tmp.Body = nil

			printer.Fprint(&buf, fset, &tmp)

			return buf.String()
		}
	}

	return ""
}

// ------------------------------------------
// Helper: parse body safely
// ------------------------------------------
func parseBody(body string) (*ast.BlockStmt, error) {

	src := "package main\nfunc _() {\n" + body + "\n}"

	fset := token.NewFileSet()

	node, err := parser.ParseFile(fset, "", src, 0)
	if err != nil {
		return nil, err
	}

	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return fn.Body, nil
		}
	}

	return nil, nil
}
