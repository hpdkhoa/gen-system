package ai

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"
)

// ExtractFunction returns only the function body (without signature)
func ExtractFunction(path, funcName string) string {
	fset := token.NewFileSet()

	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		if fn.Name.Name != funcName {
			continue
		}

		// ✅ safety check
		if fn.Body == nil {
			return ""
		}

		start := fset.Position(fn.Body.Pos()).Offset
		end := fset.Position(fn.Body.End()).Offset

		return string(data[start:end])
	}

	return ""
}

func ExtractBodyOnly(raw string) string {

	re := regexp.MustCompile(`func\s+\w+[^{]*\{([\s\S]*)\}`)

	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}

	raw = strings.TrimPrefix(raw, "```go")
	raw = strings.TrimSuffix(raw, "```")

	return strings.TrimSpace(raw)
}
