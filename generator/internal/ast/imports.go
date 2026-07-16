package ast

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"sort"
)

func NormalizeImports(path string) {

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return
	}

	importMap := map[string]bool{}

	for _, im := range node.Imports {
		importMap[im.Path.Value] = true
	}

	var keys []string
	for k := range importMap {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var specs []ast.Spec
	for _, k := range keys {
		specs = append(specs, &ast.ImportSpec{
			Path: &ast.BasicLit{
				Value: k,
			},
		})
	}

	// replace import block
	node.Imports = nil
	node.Decls = append([]ast.Decl{
		&ast.GenDecl{
			Tok:   token.IMPORT,
			Specs: specs,
		},
	}, node.Decls...)

	var buf bytes.Buffer
	format.Node(&buf, fset, node)
	os.WriteFile(path, buf.Bytes(), 0644)

}
