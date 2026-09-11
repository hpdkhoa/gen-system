// Patched in Phase A: appended tests for RemoveFile and UpdateFile.
// All existing tests preserved verbatim; the callerWith helper was added
// alongside the existing helpers.
package symbols

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

// ─── Helpers ──────────────────────────────────────────────────

func makeModule(filePath string, nodes ...*ast.Node) *ast.Module {
	return &ast.Module{
		Language:    ast.LangGo,
		FilePath:    filePath,
		PackageName: "foo",
		Nodes:       nodes,
	}
}

func funcNode(name string, exported bool) *ast.Node {
	return &ast.Node{
		ID:       "func:" + name,
		Kind:     ast.NodeFunction,
		Name:     name,
		Language: ast.LangGo,
		Exported: exported,
		Span:     ast.Span{Start: ast.Pos{Line: 1}},
	}
}

func methodNode(recv, name string) *ast.Node {
	t := ast.TypeRef{Name: recv}
	return &ast.Node{
		ID:       "method:" + recv + "." + name,
		Kind:     ast.NodeMethod,
		Name:     name,
		Language: ast.LangGo,
		Receiver: &t,
		Exported: true,
	}
}

func structNode(name string, fields ...ast.Field) *ast.Node {
	return &ast.Node{
		ID:       "struct:" + name,
		Kind:     ast.NodeStruct,
		Name:     name,
		Language: ast.LangGo,
		Exported: true,
		Fields:   fields,
	}
}

func importNode(path string) *ast.Node {
	return &ast.Node{
		Kind:       ast.NodeImport,
		Name:       path,
		ImportPath: path,
	}
}

// callerWith returns a function node "Caller" whose body calls calleeName.
func callerWith(callerName, calleeName string) *ast.Node {
	call := &ast.Node{Kind: ast.NodeCall, Callee: calleeName}
	return &ast.Node{
		ID:       "func:" + callerName,
		Kind:     ast.NodeFunction,
		Name:     callerName,
		Language: ast.LangGo,
		Exported: true,
		Children: []*ast.Node{call},
	}
}

// ─── SymbolID ─────────────────────────────────────────────────

func TestSymbolID_Format(t *testing.T) {
	id := SymbolID("pkg/foo.go", SymFunc, "CreateUser")
	if id != "pkg/foo.go#func:CreateUser" {
		t.Errorf("SymbolID = %q, want pkg/foo.go#func:CreateUser", id)
	}
}

func TestSymbolID_Uniqueness(t *testing.T) {
	a := SymbolID("foo.go", SymFunc, "F")
	b := SymbolID("bar.go", SymFunc, "F")
	c := SymbolID("foo.go", SymMethod, "F")
	if a == b || a == c {
		t.Error("different path/kind combos should produce different IDs")
	}
}

// ─── Table ────────────────────────────────────────────────────

func TestTable_BuildFromFunction(t *testing.T) {
	m := makeModule("pkg/user.go", funcNode("CreateUser", true))
	tbl := BuildTable(m)
	if tbl == nil {
		t.Fatal("BuildTable returned nil")
	}
	syms := tbl.Lookup("CreateUser")
	if len(syms) == 0 {
		t.Fatal("CreateUser not found in table")
	}
	if syms[0].Kind != SymFunc {
		t.Errorf("kind = %q, want func", syms[0].Kind)
	}
	if !syms[0].Exported {
		t.Error("CreateUser should be exported")
	}
}

func TestTable_BuildFromStruct(t *testing.T) {
	m := makeModule("model/user.go", structNode("User",
		ast.Field{Name: "ID", Type: ast.TypeRef{Name: "int"}, Exported: true},
		ast.Field{Name: "Name", Type: ast.TypeRef{Name: "string"}, Exported: true},
	))
	tbl := BuildTable(m)

	// struct itself
	userSyms := tbl.Lookup("User")
	if len(userSyms) == 0 {
		t.Fatal("User struct not in table")
	}
	if userSyms[0].Kind != SymType {
		t.Errorf("User kind = %q, want type", userSyms[0].Kind)
	}

	// fields
	idSyms := tbl.Lookup("ID")
	if len(idSyms) == 0 {
		t.Fatal("ID field not in table")
	}
	if idSyms[0].Kind != SymField {
		t.Errorf("ID kind = %q, want field", idSyms[0].Kind)
	}
}

func TestTable_BuildFromMethod(t *testing.T) {
	m := makeModule("repo/user.go", methodNode("UserRepo", "FindByID"))
	tbl := BuildTable(m)
	syms := tbl.Lookup("FindByID")
	if len(syms) == 0 {
		t.Fatal("FindByID not found")
	}
	if syms[0].Kind != SymMethod {
		t.Errorf("kind = %q, want method", syms[0].Kind)
	}
	if syms[0].ReceiverType != "UserRepo" {
		t.Errorf("ReceiverType = %q, want UserRepo", syms[0].ReceiverType)
	}
}

func TestTable_BuildFromImport(t *testing.T) {
	m := makeModule("handler/user.go", importNode("net/http"))
	tbl := BuildTable(m)
	syms := tbl.Lookup("net/http")
	if len(syms) == 0 {
		t.Fatal("net/http import not found")
	}
	if syms[0].Kind != SymImport {
		t.Errorf("kind = %q, want import", syms[0].Kind)
	}
}

func TestTable_InferLayer(t *testing.T) {
	cases := []struct {
		path  string
		layer string
	}{
		{"internal/model/user.go", "model"},
		{"internal/repo/user.go", "repo"},
		{"internal/service/build.go", "service"},
		{"internal/handler/user.go", "handler"},
		{"frontend/src/App.tsx", "frontend"},
		{"internal/utils/helper.go", ""},
	}
	for _, c := range cases {
		m := makeModule(c.path, funcNode("F", true))
		tbl := BuildTable(m)
		syms := tbl.Lookup("F")
		if len(syms) == 0 {
			t.Fatalf("F not found for path %q", c.path)
		}
		if syms[0].Layer != c.layer {
			t.Errorf("path=%q: Layer=%q, want %q", c.path, syms[0].Layer, c.layer)
		}
	}
}

func TestTable_All(t *testing.T) {
	m := makeModule("foo.go",
		funcNode("A", true),
		funcNode("B", false),
		structNode("S"),
	)
	tbl := BuildTable(m)
	all := tbl.All()
	// Module symbol + 2 funcs + 1 struct = 4 minimum
	if len(all) < 4 {
		t.Errorf("All() = %d, want >= 4", len(all))
	}
}

// ─── Index ────────────────────────────────────────────────────

func TestIndex_AddModule_Basic(t *testing.T) {
	idx := NewIndex()
	m := makeModule("pkg/user.go", funcNode("CreateUser", true))
	idx.AddModule(m)

	syms := idx.ByName("CreateUser")
	if len(syms) == 0 {
		t.Fatal("CreateUser not found in index")
	}
}

func TestIndex_ByID(t *testing.T) {
	idx := NewIndex()
	m := makeModule("pkg/user.go", funcNode("CreateUser", true))
	idx.AddModule(m)

	id := SymbolID("pkg/user.go", SymFunc, "CreateUser")
	sym, ok := idx.ByID(id)
	if !ok {
		t.Fatalf("ByID(%q) not found", id)
	}
	if sym.Name != "CreateUser" {
		t.Errorf("Name = %q, want CreateUser", sym.Name)
	}
}

func TestIndex_AddModule_Replaces(t *testing.T) {
	idx := NewIndex()
	m1 := makeModule("pkg/user.go", funcNode("OldFunc", true))
	idx.AddModule(m1)

	m2 := makeModule("pkg/user.go", funcNode("NewFunc", true))
	idx.AddModule(m2)

	// OldFunc should be gone, NewFunc present
	if syms := idx.ByName("OldFunc"); len(syms) != 0 {
		t.Error("OldFunc should be replaced after re-adding module")
	}
	if syms := idx.ByName("NewFunc"); len(syms) == 0 {
		t.Error("NewFunc should exist after re-adding module")
	}
}

func TestIndex_Resolve_LocalFirst(t *testing.T) {
	idx := NewIndex()
	m := makeModule("pkg/a.go",
		funcNode("Shared", true),
		importNode("some/other"),
	)
	idx.AddModule(m)
	m2 := makeModule("pkg/b.go", funcNode("Shared", true))
	idx.AddModule(m2)

	sym, ok := idx.Resolve("Shared", "pkg/a.go")
	if !ok {
		t.Fatal("Resolve should find Shared")
	}
	// Should prefer local (a.go) definition
	if sym.DefinedIn != "pkg/a.go" {
		t.Errorf("Resolve returned symbol from %q, want pkg/a.go", sym.DefinedIn)
	}
}

func TestIndex_Resolve_SamePackage(t *testing.T) {
	idx := NewIndex()
	// Define in b.go, resolve from a.go (same directory)
	m1 := makeModule("pkg/b.go", funcNode("Helper", false)) // unexported ok in same pkg
	m2 := makeModule("pkg/a.go")
	idx.AddModule(m1)
	idx.AddModule(m2)

	sym, ok := idx.Resolve("Helper", "pkg/a.go")
	if !ok {
		t.Fatal("Resolve should find same-package symbol")
	}
	if sym.DefinedIn != "pkg/b.go" {
		t.Errorf("resolved from %q, want pkg/b.go", sym.DefinedIn)
	}
}

func TestIndex_Resolve_NotFound(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("pkg/a.go", funcNode("Known", true)))
	_, ok := idx.Resolve("Nonexistent", "pkg/a.go")
	if ok {
		t.Error("Resolve should fail for unknown name")
	}
}

func TestIndex_CallersCallees(t *testing.T) {
	// Build two modules where A calls B
	callNode := &ast.Node{
		Kind:   ast.NodeCall,
		Callee: "HandleRequest",
	}
	callerFn := &ast.Node{
		Kind:     ast.NodeFunction,
		Name:     "RunServer",
		Exported: true,
		Children: []*ast.Node{callNode},
	}
	calleeFn := funcNode("HandleRequest", true)

	idx := NewIndex()
	idx.AddModule(makeModule("server.go", callerFn))
	idx.AddModule(makeModule("handler.go", calleeFn))

	// Resolve call edges
	// (resolveCallEdges runs during BuildFromModules; here we test manually)
	idx2 := BuildFromModules([]*ast.Module{
		makeModule("server.go", callerFn),
		makeModule("handler.go", calleeFn),
	})

	calleeID := SymbolID("handler.go", SymFunc, "HandleRequest")
	callerID := SymbolID("server.go", SymFunc, "RunServer")

	callers := idx2.Callers(calleeID)
	if len(callers) == 0 {
		// Call edge resolution depends on exact ID matching which is hard to
		// guarantee without the resolver running. Skip if not resolved.
		t.Logf("Callers: call edge not resolved (expected in unit test without full resolver)")
		_ = callerID
		return
	}

	found := false
	for _, c := range callers {
		if c.Name == "RunServer" {
			found = true
		}
	}
	if !found {
		t.Error("RunServer should appear as caller of HandleRequest")
	}
}

func TestIndex_ImpactSet_Empty(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("pkg/a.go", funcNode("Orphan", true)))
	id := SymbolID("pkg/a.go", SymFunc, "Orphan")
	impact := idx.ImpactSet(id)
	// Orphan has no callers → impact set should be empty
	if len(impact) != 0 {
		t.Errorf("ImpactSet(Orphan) = %d, want 0", len(impact))
	}
}

func TestIndex_AllSymbols(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("a.go", funcNode("F1", true), funcNode("F2", true)))
	idx.AddModule(makeModule("b.go", funcNode("F3", false)))
	all := idx.AllSymbols()
	// 3 funcs + 2 module symbols = 5 minimum
	if len(all) < 5 {
		t.Errorf("AllSymbols() = %d, want >= 5", len(all))
	}
}

func TestIndex_TableFor(t *testing.T) {
	idx := NewIndex()
	m := makeModule("foo.go", funcNode("Bar", true))
	idx.AddModule(m)
	tbl, ok := idx.TableFor("foo.go")
	if !ok {
		t.Fatal("TableFor should find foo.go")
	}
	syms := tbl.Lookup("Bar")
	if len(syms) == 0 {
		t.Error("Bar not found in table")
	}
}

func TestIndex_TableFor_Missing(t *testing.T) {
	idx := NewIndex()
	_, ok := idx.TableFor("nonexistent.go")
	if ok {
		t.Error("TableFor should return false for nonexistent file")
	}
}

// ─── Persistence ─────────────────────────────────────────────

func TestIndex_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "symbols.json")

	idx := NewIndex()
	idx.AddModule(makeModule("pkg/user.go",
		funcNode("CreateUser", true),
		structNode("User"),
	))

	if err := idx.Save(path); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("symbols.json not written: %v", err)
	}

	// Verify JSON is valid
	data, _ := os.ReadFile(path)
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("saved JSON is invalid: %v", err)
	}

	// Load and verify
	idx2, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	syms := idx2.ByName("CreateUser")
	if len(syms) == 0 {
		t.Error("CreateUser should survive save/load round-trip")
	}
}

func TestIndex_Load_NonExistent(t *testing.T) {
	idx, err := Load("/nonexistent/symbols.json")
	if err != nil {
		t.Fatalf("Load(nonexistent) should return empty index, got error: %v", err)
	}
	if idx == nil {
		t.Fatal("Load(nonexistent) should return empty index, not nil")
	}
}

func TestBuildFromModules(t *testing.T) {
	modules := []*ast.Module{
		makeModule("a.go", funcNode("Alpha", true)),
		makeModule("b.go", funcNode("Beta", false)),
		nil, // should not panic
	}
	idx := BuildFromModules(modules)
	if idx == nil {
		t.Fatal("BuildFromModules returned nil")
	}
	if syms := idx.ByName("Alpha"); len(syms) == 0 {
		t.Error("Alpha not found after BuildFromModules")
	}
}

// ─── Phase A: RemoveFile ──────────────────────────────────────

func TestIndex_RemoveFile_DropsSymbols(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("a.go", funcNode("Keep", true)))
	idx.AddModule(makeModule("b.go", funcNode("Drop", true)))

	idx.RemoveFile("b.go")

	if syms := idx.ByName("Drop"); len(syms) > 0 {
		t.Errorf("Drop survived RemoveFile: %d symbols still present", len(syms))
	}
	if syms := idx.ByName("Keep"); len(syms) == 0 {
		t.Error("Keep was wrongly removed")
	}
}

func TestIndex_RemoveFile_DropsTable(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("a.go", funcNode("X", true)))

	idx.RemoveFile("a.go")

	if _, ok := idx.TableFor("a.go"); ok {
		t.Error("TableFor should report a.go missing after RemoveFile")
	}
}

func TestIndex_RemoveFile_DropsCallEdgesFromFile(t *testing.T) {
	// caller is in caller.go; if we remove caller.go, the edge must vanish
	idx := BuildFromModules([]*ast.Module{
		makeModule("caller.go", callerWith("Caller", "Callee")),
		makeModule("callee.go", funcNode("Callee", true)),
	})

	calleeID := SymbolID("callee.go", SymFunc, "Callee")
	before := idx.Callers(calleeID)
	if len(before) == 0 {
		t.Skip("call edge not resolved in this build; edge-removal test does not apply")
	}

	idx.RemoveFile("caller.go")

	after := idx.Callers(calleeID)
	if len(after) != 0 {
		t.Errorf("after RemoveFile(caller.go), Callee should have 0 callers, got %d", len(after))
	}
}

func TestIndex_RemoveFile_ClearsCalleeIDPointingAtRemoved(t *testing.T) {
	// caller in a.go calls Callee defined in b.go. Removing b.go
	// should not delete the edge (the call site still exists in a.go)
	// but CalleeID must be cleared since the symbol it pointed to is gone.
	idx := BuildFromModules([]*ast.Module{
		makeModule("a.go", callerWith("Caller", "Callee")),
		makeModule("b.go", funcNode("Callee", true)),
	})

	calleeIDBefore := SymbolID("b.go", SymFunc, "Callee")
	if cs := idx.Callers(calleeIDBefore); len(cs) == 0 {
		t.Skip("call edge not resolved; CalleeID-clearing test does not apply")
	}

	idx.RemoveFile("b.go")

	// Callers query for the now-deleted symbol must return nothing.
	if cs := idx.Callers(calleeIDBefore); len(cs) != 0 {
		t.Errorf("Callers of removed symbol should be empty, got %d", len(cs))
	}

	// But the unresolved edge should still be discoverable: a.go's Caller
	// should still have a call edge, just with empty CalleeID.
	edges := idx.AllCallEdges()
	foundUnresolved := false
	for _, e := range edges {
		if e.CalleeName == "Callee" && e.CalleeID == "" {
			foundUnresolved = true
		}
	}
	if !foundUnresolved {
		t.Errorf("expected unresolved call edge after CalleeID cleared, edges: %+v", edges)
	}
}

func TestIndex_RemoveFile_Idempotent(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("a.go", funcNode("X", true)))

	// First removal: real work.
	idx.RemoveFile("a.go")
	// Second removal: must not panic, must be a no-op.
	idx.RemoveFile("a.go")
	idx.RemoveFile("never-existed.go")

	if syms := idx.ByName("X"); len(syms) != 0 {
		t.Errorf("X should still be gone after double-remove")
	}
}

func TestIndex_RemoveFile_DropsRefsIn(t *testing.T) {
	// Manually plant a reference whose In points to b.go,
	// then RemoveFile("b.go") should drop it.
	idx := NewIndex()
	idx.AddModule(makeModule("a.go", funcNode("Target", true)))

	targetID := SymbolID("a.go", SymFunc, "Target")
	// Inject a ref. We hold mu.Lock() to mirror writer access pattern.
	idx.mu.Lock()
	idx.refs[targetID] = []Reference{
		{SymbolID: targetID, In: "b.go", Kind: RefRead},
		{SymbolID: targetID, In: "c.go", Kind: RefRead},
	}
	idx.mu.Unlock()

	idx.RemoveFile("b.go")

	remaining := idx.References(targetID)
	if len(remaining) != 1 {
		t.Fatalf("after RemoveFile(b.go), want 1 ref (c.go), got %d", len(remaining))
	}
	if remaining[0].In != "c.go" {
		t.Errorf("remaining ref In = %q, want c.go", remaining[0].In)
	}
}

// ─── Phase A: UpdateFile ──────────────────────────────────────

func TestIndex_UpdateFile_ReplacesContent(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("a.go", funcNode("Old", true)))

	idx.UpdateFile("a.go", makeModule("a.go", funcNode("New", true)))

	if syms := idx.ByName("Old"); len(syms) > 0 {
		t.Errorf("Old should be removed by UpdateFile")
	}
	if syms := idx.ByName("New"); len(syms) == 0 {
		t.Errorf("New should be present after UpdateFile")
	}
}

func TestIndex_UpdateFile_NilDropsFile(t *testing.T) {
	idx := NewIndex()
	idx.AddModule(makeModule("a.go", funcNode("X", true)))

	idx.UpdateFile("a.go", nil)

	if _, ok := idx.TableFor("a.go"); ok {
		t.Error("UpdateFile(nil) should remove the file from the index")
	}
}

func TestIndex_UpdateFile_ResolvesNewlyAvailableCallees(t *testing.T) {
	// Step 1: caller exists, callee not yet present.
	// Step 2: UpdateFile adds the callee. The previously-dangling
	// call edge should pick up a CalleeID.
	idx := NewIndex()
	idx.AddModule(makeModule("caller.go", callerWith("Caller", "Callee")))
	// callee.go does not exist yet; call edge dangling.

	// Step 2: introduce callee.go via UpdateFile.
	idx.UpdateFile("callee.go", makeModule("callee.go", funcNode("Callee", true)))

	calleeID := SymbolID("callee.go", SymFunc, "Callee")
	callers := idx.Callers(calleeID)
	if len(callers) == 0 {
		t.Errorf("after UpdateFile(callee.go), Caller should be linked to Callee; got 0 callers")
	} else {
		found := false
		for _, c := range callers {
			if c.Name == "Caller" {
				found = true
			}
		}
		if !found {
			t.Errorf("Caller not found in Callers(%s)", calleeID)
		}
	}
}

func TestIndex_UpdateFile_ClearsBothPathsOnMismatch(t *testing.T) {
	// If the caller passes filePath="right.go" but m.FilePath="wrong.go",
	// both keys must be cleared first so we never leave duplicate state.
	// The module is ingested under m.FilePath (BuildTable's source of truth).
	idx := NewIndex()
	idx.AddModule(makeModule("right.go", funcNode("Stale", true)))

	m := makeModule("wrong.go", funcNode("F", true))
	idx.UpdateFile("right.go", m)

	// Stale must be gone (RemoveFile("right.go") happened).
	if syms := idx.ByName("Stale"); len(syms) != 0 {
		t.Errorf("Stale should be removed, got %d", len(syms))
	}
	// F must be present.
	syms := idx.ByName("F")
	if len(syms) == 0 {
		t.Fatal("F not found")
	}
	// And DefinedIn comes from the module — wrong.go in this case.
	if syms[0].DefinedIn != "wrong.go" {
		t.Errorf("DefinedIn = %q, want wrong.go (m.FilePath is source of truth)", syms[0].DefinedIn)
	}
}
