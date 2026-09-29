// D101: resolveImportPath ranged over Table.All, a map, and matched the last
// path segment of every import, aliased or not. With text/template and
// htmpl "html/template" in one file, template.New resolved to html/template on
// some runs. Resolve steps 2 to 4 also took the first match in map order.
package symbols

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

func aliasedImport(alias, path string) *ast.Node {
	n := importDecl("template", path)
	n.ImportAlias = alias
	return n
}

func TestD101_AliasedImportDoesNotClaimLastSegment(t *testing.T) {
	want := SymbolID("/goroot/src/text/template/x.go", SymFunc, "New")
	for i := 0; i < 100; i++ {
		idx := NewIndex()
		idx.AddModule(makeModule("/goroot/src/text/template/x.go", funcNode("New", true)))
		idx.AddModule(makeModule("/goroot/src/html/template/y.go", funcNode("New", true)))
		idx.AddModule(makeModule("app/web/page.go",
			aliasedImport("htmpl", "html/template"),
			importDecl("template", "text/template"),
			funcWithCalls("Render", "template.New", "htmpl.New"),
		))
		idx.ResolveAll()
		got := edgesFrom(idx, SymbolID("app/web/page.go", SymFunc, "Render"))
		if got["template.New"] != want {
			t.Fatalf("run %d: template.New resolved to %q, want %q", i, got["template.New"], want)
		}
		if p := idx.resolveImportPath("template", "app/web/page.go"); p != "text/template" {
			t.Fatalf("run %d: resolveImportPath(template) = %q, want text/template", i, p)
		}
		if p := idx.resolveImportPath("htmpl", "app/web/page.go"); p != "html/template" {
			t.Fatalf("run %d: resolveImportPath(htmpl) = %q, want html/template", i, p)
		}
	}
	// A file that imports html/template only under an alias has no "template".
	idx := NewIndex()
	idx.AddModule(makeModule("app/web/only.go", aliasedImport("htmpl", "html/template")))
	if p := idx.resolveImportPath("template", "app/web/only.go"); p != "" {
		t.Fatalf("resolveImportPath(template) = %q for an aliased import, want empty", p)
	}
}

func TestD101_ResolveIsDeterministic(t *testing.T) {
	for i := 0; i < 100; i++ {
		idx := NewIndex()
		// Step 2: two other files in the same directory define Foo.
		idx.AddModule(makeModule("pkg/b.go", funcNode("Foo", false)))
		idx.AddModule(makeModule("pkg/a.go", funcNode("Foo", false)))
		idx.AddModule(makeModule("pkg/c.go"))
		// Step 4: two exported Helper symbols in other packages.
		idx.AddModule(makeModule("b/y.go", funcNode("Helper", true)))
		idx.AddModule(makeModule("a/x.go", funcNode("Helper", true)))
		idx.AddModule(makeModule("c/z.go"))

		if s, ok := idx.Resolve("Foo", "pkg/c.go"); !ok || s.DefinedIn != "pkg/a.go" {
			t.Fatalf("run %d: step 2 gave %+v, want pkg/a.go", i, s)
		}
		if s, ok := idx.Resolve("Helper", "c/z.go"); !ok || s.DefinedIn != "a/x.go" {
			t.Fatalf("run %d: step 4 gave %+v, want a/x.go", i, s)
		}
	}
}

func TestD101_TableAllSortedByID(t *testing.T) {
	tb := BuildTable(makeModule("p/f.go", funcNode("Zed", true), funcNode("Alpha", true), funcNode("Mid", true)))
	all := tb.All()
	for i := 1; i < len(all); i++ {
		if all[i-1].ID > all[i].ID {
			t.Fatalf("Table.All not sorted by ID: %q before %q", all[i-1].ID, all[i].ID)
		}
	}
}
