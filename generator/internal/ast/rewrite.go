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

// FuncAtLine returns the name of the function or method whose declaration
// covers the given 1-based line of a Go file. ok is false when no function
// covers the line, or when two declarations in the file share that name, so a
// name-based rewrite could hit the wrong one. v26.57 (D011): the healer uses it
// to repair the function the compiler error points at.
func FuncAtLine(path string, line int) (name string, ok bool) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return "", false
	}
	count := map[string]int{}
	for _, decl := range node.Decls {
		fn, isFn := decl.(*ast.FuncDecl)
		if !isFn {
			continue
		}
		count[fn.Name.Name]++
		start, end := fset.Position(fn.Pos()).Line, fset.Position(fn.End()).Line
		if line >= start && line <= end {
			name = fn.Name.Name
		}
	}
	if name == "" || count[name] != 1 {
		return "", false
	}
	return name, true
}

// FuncNames returns the names of the functions and methods a Go file declares
// exactly once, in file order.
func FuncNames(path string) []string {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil
	}
	count := map[string]int{}
	var order []string
	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if count[fn.Name.Name] == 0 {
				order = append(order, fn.Name.Name)
			}
			count[fn.Name.Name]++
		}
	}
	var out []string
	for _, n := range order {
		if count[n] == 1 {
			out = append(out, n)
		}
	}
	return out
}

// HasFunc reports whether a Go file declares exactly one function or method
// with the given name.
func HasFunc(path, funcName string) bool {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return false
	}
	n := 0
	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == funcName {
			n++
		}
	}
	return n == 1
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
