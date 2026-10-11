// autoimport_v2657_test.go (v26.57, D012): AutoResolveImports must add each
// missing import once, with one pair of quote marks. The table stored quoted
// paths and EnsureImport quoted them again, so a file got the line
// import ""fmt"" and no longer parsed; with a parenthesized block the write
// failed and no import was added at all.
package ast

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func importsOf(t *testing.T, path string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		src, _ := os.ReadFile(path)
		t.Fatalf("file no longer parses: %v\n%s", err, src)
	}
	var out []string
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		out = append(out, p)
	}
	return out
}

func TestAutoResolveImports_AddsPlainImports(t *testing.T) {
	cases := map[string]string{
		"none":   "package x\n\nfunc F() {}\n",
		"single": "package x\n\nimport \"fmt\"\n\nfunc F() { fmt.Println() }\n",
		"block":  "package x\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc F() { fmt.Println(os.Args) }\n",
	}
	body := "s := strings.TrimSpace(\" a \")\n\tfmt.Println(s)"
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "f.go")
			if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			AutoResolveImports(p, body)
			count := map[string]int{}
			for _, im := range importsOf(t, p) {
				count[im]++
			}
			if count["strings"] != 1 || count["fmt"] != 1 {
				t.Fatalf("want strings and fmt once each, got %v", count)
			}
		})
	}
}
