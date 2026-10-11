// symbols/index_removefile_test.go: tests for Index.RemoveFile
// Tests for open item 2: the incremental rebuild delete path.
package symbols

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

func TestRemoveFile_RemovesSymbols(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("pkg/a.go",
		funcNode("Alpha", true),
		funcNode("Beta", true),
	))
	idx.AddModule(makeModule("pkg/b.go",
		funcNode("Gamma", true),
	))

	// Sanity: both files indexed.
	if syms := idx.ByName("Alpha"); len(syms) == 0 {
		t.Fatal("Alpha should be indexed before RemoveFile")
	}
	if syms := idx.ByName("Gamma"); len(syms) == 0 {
		t.Fatal("Gamma should be indexed before RemoveFile")
	}

	// Remove one file.
	idx.RemoveFile("pkg/a.go")

	// Alpha and Beta from a.go should be gone.
	if syms := idx.ByName("Alpha"); len(syms) != 0 {
		t.Errorf("Alpha should be removed after RemoveFile, still found %d", len(syms))
	}
	if syms := idx.ByName("Beta"); len(syms) != 0 {
		t.Errorf("Beta should be removed after RemoveFile, still found %d", len(syms))
	}

	// Gamma from b.go should survive.
	if syms := idx.ByName("Gamma"); len(syms) == 0 {
		t.Error("Gamma from b.go should survive RemoveFile(pkg/a.go)")
	}
}

func TestRemoveFile_TableFor(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("pkg/gone.go", funcNode("F", true)))

	if _, ok := idx.TableFor("pkg/gone.go"); !ok {
		t.Fatal("table should exist before RemoveFile")
	}

	idx.RemoveFile("pkg/gone.go")

	if _, ok := idx.TableFor("pkg/gone.go"); ok {
		t.Error("table should be gone after RemoveFile")
	}
}

func TestRemoveFile_Idempotent(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("pkg/a.go", funcNode("X", true)))
	// Calling twice must not panic.
	idx.RemoveFile("pkg/a.go")
	idx.RemoveFile("pkg/a.go")
}

func TestRemoveFile_NonExistentFile(t *testing.T) {
	idx := NewIndex()
	// Removing a file that was never added must not panic.
	idx.RemoveFile("pkg/nonexistent.go")
}

func TestRemoveFile_PrunesCallEdges(t *testing.T) {
	caller, _ := ast.ForLanguage(ast.LangGo).Parse("pkg/caller.go", []byte(
		"package p\nfunc A() { B() }\n",
	))
	callee, _ := ast.ForLanguage(ast.LangGo).Parse("pkg/callee.go", []byte(
		"package p\nfunc B() {}\n",
	))
	idx := BuildFromModules([]*ast.Module{caller, callee})

	callerID := SymbolID("pkg/caller.go", SymFunc, "A")
	calleeID := SymbolID("pkg/callee.go", SymFunc, "B")

	// Before: callers of B should include A (if call edge resolved).
	_ = callerID
	_ = calleeID

	// Remove the callee file.
	idx.RemoveFile("pkg/callee.go")

	// Callee symbol should be gone.
	if syms := idx.ByName("B"); len(syms) != 0 {
		t.Errorf("B should be removed, found %d", len(syms))
	}

	// Call edges involving the removed file should be pruned.
	for _, e := range idx.AllCallEdges() {
		callerFile := fileFromSymbolID(e.CallerID)
		calleeFile := fileFromSymbolID(e.CalleeID)
		if callerFile == "pkg/callee.go" || calleeFile == "pkg/callee.go" {
			t.Errorf("call edge involving removed file should have been pruned: %+v", e)
		}
	}
}

func TestRemoveFile_PrunesReferences(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("pkg/a.go", funcNode("Foo", true)))

	// Manually add a reference so we can test pruning.
	fooID := SymbolID("pkg/a.go", SymFunc, "Foo")
	idx.mu.Lock()
	idx.refs[fooID] = []Reference{
		{SymbolID: fooID, In: "pkg/b.go"},
		{SymbolID: fooID, In: "pkg/a.go"},
	}
	idx.mu.Unlock()

	idx.RemoveFile("pkg/a.go")

	refs := idx.References(fooID)
	for _, r := range refs {
		if r.In == "pkg/a.go" {
			t.Errorf("reference from removed file should be pruned: %+v", r)
		}
	}
}

func TestFileFromSymbolID(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"pkg/foo.go#func:Bar", "pkg/foo.go"},
		{"handler.go#method:Repo.Save", "handler.go"},
		{"#func:X", ""},
		{"nohash", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := fileFromSymbolID(c.id)
		if got != c.want {
			t.Errorf("fileFromSymbolID(%q) = %q, want %q", c.id, got, c.want)
		}
	}
}
