package ast

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"strings"
)

// RewriteFunctionBody preserves signature and replaces only body
//
// v26.58 (OPEN-REWRITE): the new body was parsed in a second file set and
// printed with the file's positions, so line breaks inside a statement moved
// and other declarations lost their layout. The body text is now spliced
// into the source between the old braces, or appended as a stub, and the
// whole file goes through go/format, so the output is gofmt clean and the
// rest of the file keeps its text.
func RewriteFunctionBody(path, funcName, newBody string) error {

	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	fset := token.NewFileSet()

	node, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return err
	}

	if _, err := parseBody(newBody); err != nil {
		return err
	}

	block := "{\n" + newBody + "\n}"
	var out []byte

	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		if fn.Name.Name == funcName {
			// ✅ preserve signature, only replace body
			if fn.Body != nil {
				lb := fset.Position(fn.Body.Lbrace).Offset
				rb := fset.Position(fn.Body.Rbrace).Offset
				out = spliceBytes(src, lb, rb+1, block)
			} else {
				// a declaration without a body gets one after its signature
				at := fset.Position(fn.Type.End()).Offset
				out = spliceBytes(src, at, at, " "+block)
			}
			break
		}
	}

	// ------------------------------------------
	// If function not found → create stub
	// ------------------------------------------
	if out == nil {
		out = append([]byte{}, src...)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
		out = append(out, "\nfunc "+funcName+"() "+block+"\n"...)
	}

	formatted, err := format.Source(out)
	if err != nil {
		return err
	}

	return os.WriteFile(path, formatted, 0644)
}

// spliceBytes returns src with src[from:to] replaced by text.
func spliceBytes(src []byte, from, to int, text string) []byte {
	out := make([]byte, 0, len(src)-(to-from)+len(text))
	out = append(out, src[:from]...)
	out = append(out, text...)
	return append(out, src[to:]...)
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

// FuncBodyText returns the source text between the braces of the one function
// or method with the given name, trimmed. ok is false when the file does not
// parse or does not declare exactly one such function. v26.58.
func FuncBodyText(path, funcName string) (string, bool) {
	src, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return "", false
	}
	var found *ast.FuncDecl
	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == funcName {
			if found != nil {
				return "", false
			}
			found = fn
		}
	}
	if found == nil || found.Body == nil {
		return "", false
	}
	lb := fset.Position(found.Body.Lbrace).Offset
	rb := fset.Position(found.Body.Rbrace).Offset
	if lb < 0 || rb > len(src) || lb+1 > rb {
		return "", false
	}
	return strings.TrimSpace(string(src[lb+1 : rb])), true
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

			// clone function without body
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

	// v26.58 (OPEN-REWRITE): RewriteFunctionBody splices the body text into
	// the file, so a body that closes the function and opens another
	// declaration would add that declaration. The wrapper must hold exactly
	// one declaration.
	if len(node.Decls) != 1 {
		return nil, fmt.Errorf("new body for a function must not close the function: it gives %d declarations", len(node.Decls))
	}

	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return fn.Body, nil
		}
	}

	return nil, nil
}
