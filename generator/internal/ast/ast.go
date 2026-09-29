package ast

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

// =========================================================
// PARSE FILE
// =========================================================
func parseFile(path string) (*ast.File, *token.FileSet, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	return node, fset, err
}

// =========================================================
// WRITE FILE (FORMATTED)
// =========================================================
func writeFile(path string, fset *token.FileSet, node *ast.File) error {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, node); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}

// =========================================================
// ENSURE IMPORT (NO DUPLICATE)
// =========================================================
//
// v26.58 (D077): EnsureImport returns the parse or write error. It dropped
// both, so a failed write left the file with no import and no message.
func EnsureImport(path, pkg string) error {
	// v26.57 (D012): accept a path with or without quote marks.
	pkg = strings.Trim(pkg, "`\"")
	node, fset, err := parseFile(path)
	if err != nil {
		return fmt.Errorf("ensure import %s: %w", pkg, err)
	}

	for _, im := range node.Imports {
		if im.Path.Value == `"`+pkg+`"` {
			return nil
		}
	}

	newImport := &ast.ImportSpec{
		Path: &ast.BasicLit{
			Kind:  token.STRING,
			Value: `"` + pkg + `"`,
		},
	}

	if len(node.Imports) == 0 {
		gen := &ast.GenDecl{
			Tok: token.IMPORT,
			Specs: []ast.Spec{
				newImport,
			},
		}
		node.Decls = append([]ast.Decl{gen}, node.Decls...)
	} else {
		for _, d := range node.Decls {
			if gen, ok := d.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
				gen.Specs = append(gen.Specs, newImport)
				break
			}
		}
	}

	if err := writeFile(path, fset, node); err != nil {
		return fmt.Errorf("ensure import %s: %w", pkg, err)
	}
	return nil
}

// =========================================================
// MERGE STRUCT (ADD MISSING FIELDS)
// =========================================================
func MergeStruct(path, structName string, fields map[string]string) {
	node, fset, err := parseFile(path)
	if err != nil {
		return
	}

	var target *ast.StructType

	for _, decl := range node.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if ts.Name.Name == structName {
				if st, ok := ts.Type.(*ast.StructType); ok {
					target = st
				}
			}
		}
	}

	if target == nil {
		return
	}

	existing := map[string]bool{}
	for _, f := range target.Fields.List {
		for _, name := range f.Names {
			existing[name.Name] = true
		}
	}

	for name, typ := range fields {
		if existing[name] {
			continue
		}

		field := &ast.Field{
			Names: []*ast.Ident{ast.NewIdent(name)},
			Type:  ast.NewIdent(typ),
		}
		target.Fields.List = append(target.Fields.List, field)
	}

	writeFile(path, fset, node)

}

// =========================================================
// ADD STRUCT IF NOT EXISTS
// =========================================================
func EnsureStruct(path, structName string, fields map[string]string) {
	node, fset, err := parseFile(path)
	if err != nil {
		createStructFile(path, structName, fields)
		return
	}

	for _, decl := range node.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if ok && ts.Name.Name == structName {
				return
			}
		}
	}

	var fieldList []*ast.Field
	for name, typ := range fields {
		fieldList = append(fieldList, &ast.Field{
			Names: []*ast.Ident{ast.NewIdent(name)},
			Type:  ast.NewIdent(typ),
		})
	}

	st := &ast.StructType{
		Fields: &ast.FieldList{
			List: fieldList,
		},
	}

	ts := &ast.TypeSpec{
		Name: ast.NewIdent(structName),
		Type: st,
	}

	gen := &ast.GenDecl{
		Tok:   token.TYPE,
		Specs: []ast.Spec{ts},
	}

	node.Decls = append(node.Decls, gen)

	writeFile(path, fset, node)

}

// =========================================================
// CREATE FILE WITH STRUCT
// =========================================================
func createStructFile(path, structName string, fields map[string]string) {
	var buf bytes.Buffer
	buf.WriteString("package model\n\n")
	buf.WriteString("type " + structName + " struct {\n")
	for k, v := range fields {
		buf.WriteString("  " + k + " " + v + "\n")
	}
	buf.WriteString("}\n")
	os.WriteFile(path, buf.Bytes(), 0644)
}

// =========================================================
// ENSURE FUNCTION (NO DUPLICATE)
// =========================================================
func EnsureFunction(path, funcName string, body string) {
	node, _, err := parseFile(path) // ← fix here
	if err != nil {
		println("⚠️ skipped invalid AST write for function:", funcName)
		return
	}

	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Name.Name == funcName {
				return
			}
		}
	}

	println("⚠️ skipped invalid AST write for function:", funcName)
}

// =========================================================
// RAW APPEND (SAFE FALLBACK)
// =========================================================
func appendRaw(path, content string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("⚠️ appendRaw: could not open file:", path, err)
		return
	}
	defer f.Close()

	_, writeErr := f.WriteString("\n\n" + content)
	if writeErr != nil {
		fmt.Println("⚠️ appendRaw: write failed:", path, writeErr)
	}
}

// =========================================================
// ENSURE ROUTE REGISTRATION
// =========================================================
func EnsureRoute(path, routeLine string) {
	data, err := os.ReadFile(path)
	if err != nil {
		appendRaw(path, routeLine)
		return
	}

	if bytes.Contains(data, []byte(routeLine)) {
		return
	}

	appendRaw(path, routeLine)

}

func EnsureOrCreateFunction(path, funcName, body string) error {

	node, _, err := parseFile(path)
	if err != nil {
		// file invalid → fallback raw write
		return appendFunctionSafe(path, funcName, body)
	}

	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Name.Name == funcName {
				return RewriteFunctionBody(path, funcName, body)
			}
		}
	}

	// function does not exist → safe insert
	return appendFunctionSafe(path, funcName, body)
}

func appendFunctionSafe(path, funcName, body string) error {

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString("\n\nfunc " + funcName + "() {\n" + body + "\n}\n")
	return err
}
