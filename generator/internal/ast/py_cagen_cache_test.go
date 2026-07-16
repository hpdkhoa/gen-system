package ast

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── Python frontend ──────────────────────────────────────────

func pyFE() Frontend { return ForLanguage(LangPython) }

func TestPyFrontend_Available(t *testing.T) {
	fe := pyFE()
	// Available() may be true or false depending on system Python.
	// We just verify it doesn't panic and returns a bool.
	_ = fe.Available()
}

func TestPyFrontend_Extensions(t *testing.T) {
	fe := pyFE()
	exts := fe.Extensions()
	if !contains(exts, ".py") {
		t.Errorf("Extensions() = %v, missing .py", exts)
	}
}

func TestPyFrontend_ParseSimple(t *testing.T) {
	if !pyFE().Available() {
		t.Skip("python3 not available")
	}
	src := `
def greet(name: str) -> str:
    """Return a greeting."""
    return f"Hello {name}"

class User:
    """A user model."""
    id: int
    name: str

    def __init__(self, id: int, name: str):
        self.id = id
        self.name = name
`
	m, err := pyFE().Parse("user.py", []byte(src))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if m == nil {
		t.Fatal("nil module")
	}
	if m.Language != LangPython {
		t.Errorf("Language = %q, want python", m.Language)
	}

	funcs := m.TopLevelByKind(NodeFunction)
	if len(funcs) == 0 {
		t.Error("expected at least the greet function")
	}
	var foundGreet bool
	for _, f := range funcs {
		if f.Name == "greet" {
			foundGreet = true
			if len(f.Params) == 0 {
				t.Error("greet should have a 'name' parameter")
			}
		}
	}
	if !foundGreet {
		t.Error("greet function not found")
	}

	classes := m.TopLevelByKind(NodeClass)
	if len(classes) == 0 {
		t.Error("expected User class")
	}
	var foundUser bool
	for _, c := range classes {
		if c.Name == "User" {
			foundUser = true
		}
	}
	if !foundUser {
		t.Error("User class not found")
	}
}

func TestPyFrontend_ParseImports(t *testing.T) {
	if !pyFE().Available() {
		t.Skip("python3 not available")
	}
	src := `import os
import json
from typing import List, Optional
from pathlib import Path
`
	m, err := pyFE().Parse("imports.py", []byte(src))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	imports := m.TopLevelByKind(NodeImport)
	if len(imports) < 2 {
		t.Errorf("expected at least 2 imports, got %d", len(imports))
	}
}

func TestPyFrontend_UnavailableGraceful(t *testing.T) {
	// Simulate unavailable by creating a fresh frontend with no python binary.
	// We can't easily mock this, so instead we test the module produced when
	// Available() returns false by checking the struct state.
	fe := &PyFrontend{pythonBin: "/nonexistent/python3"}
	if fe.Available() {
		// Binary exists? Skip (unusual environment)
		t.Skip("/nonexistent/python3 somehow exists")
	}
	m, err := fe.Parse("script.py", []byte("x = 1"))
	if err != nil {
		t.Fatalf("expected graceful fallback, got error: %v", err)
	}
	if m == nil {
		t.Fatal("expected module even when python unavailable")
	}
	unknowns := m.UnknownNodes()
	if len(unknowns) == 0 {
		t.Error("expected NodeUnknown for unavailable python runtime")
	}
	if len(m.ParseErrors) == 0 {
		t.Error("expected ParseError for unavailable python runtime")
	}
}

// ─── CA Gen frontend ──────────────────────────────────────────

func cagenFE() Frontend { return ForLanguage(LangCAGen) }

func TestCAGenFrontend_Available(t *testing.T) {
	if !cagenFE().Available() {
		t.Error("CAGenFrontend.Available() should always return true (pure Go)")
	}
}

func TestCAGenFrontend_ParseProcedure(t *testing.T) {
	src := `PROCEDURE CreateOrder
  READ EACH CUSTOMER WHERE CUSTOMER.ID = CUSTOMER-ID
  IF CUSTOMER.STATUS = 'ACTIVE'
    WRITE ORDER
    CALL ValidateOrder USING ORDER-VIEW
  ELSE
    ESCAPE READ EACH
  END
END
`
	m, err := cagenFE().Parse("order.ad", []byte(src))
	assertNoError(t, err)
	if m.Language != LangCAGen {
		t.Errorf("Language = %q, want cagen", m.Language)
	}
	procs := m.TopLevelByKind(NodeProcedure)
	if len(procs) == 0 {
		t.Fatal("expected at least one procedure node")
	}
	var found bool
	for _, p := range procs {
		if p.Name == "CreateOrder" {
			found = true
		}
	}
	if !found {
		t.Error("CreateOrder procedure not found")
	}
	if m.PackageName != "CreateOrder" {
		t.Errorf("PackageName = %q, want CreateOrder", m.PackageName)
	}
}

func TestCAGenFrontend_ParseReadEach(t *testing.T) {
	src := `PROCEDURE GetOrders
  READ EACH ORDER SORTED BY ORDER.DATE
    SET RESULT TO ORDER.ID
  END
END
`
	m, err := cagenFE().Parse("order.ad", []byte(src))
	assertNoError(t, err)
	procs := m.TopLevelByKind(NodeProcedure)
	if len(procs) == 0 {
		t.Fatal("expected procedure")
	}
	var hasLoop bool
	for _, child := range procs[0].Children {
		if child.Kind == NodeLoop {
			hasLoop = true
			break
		}
	}
	if !hasLoop {
		t.Error("READ EACH should produce NodeLoop child")
	}
}

func TestCAGenFrontend_ParseCallStatement(t *testing.T) {
	src := `PROCEDURE InvokeHelper
  CALL HelperProc USING HELPER-VIEW
END
`
	m, err := cagenFE().Parse("helper.cagen", []byte(src))
	assertNoError(t, err)
	procs := m.TopLevelByKind(NodeProcedure)
	if len(procs) == 0 {
		t.Fatal("expected procedure")
	}
	var foundCall bool
	for _, child := range procs[0].Children {
		if child.Kind == NodeCall && child.Callee == "HelperProc" {
			foundCall = true
			break
		}
	}
	if !foundCall {
		t.Error("CALL HelperProc not found as NodeCall child")
	}
}

func TestCAGenFrontend_ParseIfElse(t *testing.T) {
	src := `PROCEDURE CheckStatus
  IF STATUS = 'OK'
    WRITE LOG
  ELSE
    EXIT PROCEDURE
  END
END
`
	m, err := cagenFE().Parse("check.ad", []byte(src))
	assertNoError(t, err)
	procs := m.TopLevelByKind(NodeProcedure)
	if len(procs) == 0 {
		t.Fatal("expected procedure")
	}
	var hasBranch bool
	for _, child := range procs[0].Children {
		if child.Kind == NodeBranch {
			hasBranch = true
			break
		}
	}
	if !hasBranch {
		t.Error("IF statement should produce NodeBranch child")
	}
}

func TestCAGenFrontend_UnknownStatement(t *testing.T) {
	src := `PROCEDURE TestUnknown
  SOME_UNKNOWN_KEYWORD argument1 argument2
END
`
	m, err := cagenFE().Parse("test.ad", []byte(src))
	assertNoError(t, err)
	unknowns := m.UnknownNodes()
	if len(unknowns) == 0 {
		t.Error("unknown keyword should produce NodeUnknown")
	}
}

func TestCAGenFrontend_CommentLines(t *testing.T) {
	src := `* This is a comment line
* Another comment
PROCEDURE NoComments
  WRITE ENTITY
END
`
	m, err := cagenFE().Parse("commented.ad", []byte(src))
	assertNoError(t, err)
	procs := m.TopLevelByKind(NodeProcedure)
	if len(procs) != 1 {
		t.Errorf("expected 1 procedure (comments stripped), got %d", len(procs))
	}
}

// ─── Cache ────────────────────────────────────────────────────

func TestCache_ChecksumBytes(t *testing.T) {
	s1 := ChecksumBytes([]byte("hello"))
	s2 := ChecksumBytes([]byte("hello"))
	s3 := ChecksumBytes([]byte("world"))
	if s1 != s2 {
		t.Error("same content should produce same checksum")
	}
	if s1 == s3 {
		t.Error("different content should produce different checksum")
	}
	if len(s1) != 64 { // SHA-256 hex = 64 chars
		t.Errorf("checksum length = %d, want 64", len(s1))
	}
}

func TestCache_GetPutMiss(t *testing.T) {
	c := NewCache("")
	_, ok := c.Get("nonexistent.go", "abc123")
	if ok {
		t.Error("Get on empty cache should return false")
	}
}

func TestCache_PutGet(t *testing.T) {
	c := NewCache("")
	m := &Module{
		FilePath: "foo.go",
		Checksum: "checksum123",
		Language: LangGo,
	}
	c.Put(m)
	got, ok := c.Get("foo.go", "checksum123")
	if !ok {
		t.Fatal("Get after Put should return true")
	}
	if got.FilePath != "foo.go" {
		t.Errorf("got.FilePath = %q, want foo.go", got.FilePath)
	}
}

func TestCache_WrongChecksumMisses(t *testing.T) {
	c := NewCache("")
	m := &Module{FilePath: "foo.go", Checksum: "abc"}
	c.Put(m)
	_, ok := c.Get("foo.go", "different")
	if ok {
		t.Error("Get with wrong checksum should miss")
	}
}

func TestCache_Invalidate(t *testing.T) {
	c := NewCache("")
	m1 := &Module{FilePath: "bar.go", Checksum: "sum1"}
	m2 := &Module{FilePath: "bar.go", Checksum: "sum2"}
	c.Put(m1)
	c.Put(m2)
	c.Invalidate("bar.go")
	_, ok1 := c.Get("bar.go", "sum1")
	_, ok2 := c.Get("bar.go", "sum2")
	if ok1 || ok2 {
		t.Error("Invalidate should remove all entries for the file")
	}
}

func TestCache_DiskPersistence(t *testing.T) {
	dir := t.TempDir()
	diskDir := filepath.Join(dir, "ast_cache")

	c1 := NewCache(diskDir)
	m := &Module{
		FilePath:    "persisted.go",
		Checksum:    "persistedchecksum",
		Language:    LangGo,
		PackageName: "mypackage",
		Nodes:       []*Node{{Kind: NodeFunction, Name: "Foo"}},
	}
	c1.Put(m)

	// New cache instance should load from disk
	c2 := NewCache(diskDir)
	got, ok := c2.Get("persisted.go", "persistedchecksum")
	if !ok {
		t.Fatal("disk-persisted entry should be found by new cache instance")
	}
	if got.PackageName != "mypackage" {
		t.Errorf("PackageName = %q, want mypackage", got.PackageName)
	}
}

func TestCache_ParseOrCache_NilOnUnknownExtension(t *testing.T) {
	c := NewCache("")
	// Write a temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "file.unknown")
	_ = os.WriteFile(path, []byte("data"), 0644)

	m, err := c.ParseOrCache(path)
	if err != nil {
		t.Fatalf("ParseOrCache on unknown extension should not error: %v", err)
	}
	if m != nil {
		t.Error("ParseOrCache on unknown extension should return nil")
	}
}

func TestCache_ParseOrCache_Go(t *testing.T) {
	c := NewCache("")
	dir := t.TempDir()
	path := filepath.Join(dir, "simple.go")
	_ = os.WriteFile(path, []byte("package simple\nfunc F(){}"), 0644)

	m, err := c.ParseOrCache(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected module")
	}
	if m.PackageName != "simple" {
		t.Errorf("PackageName = %q, want simple", m.PackageName)
	}

	// Second call should hit cache
	m2, err2 := c.ParseOrCache(path)
	if err2 != nil {
		t.Fatalf("second call error: %v", err2)
	}
	if m2 == nil {
		t.Fatal("second call returned nil")
	}
	if m.Checksum != m2.Checksum {
		t.Error("second call should return cached module with same checksum")
	}
}
