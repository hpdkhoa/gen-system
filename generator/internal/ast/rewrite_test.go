package ast_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

// writeGoFile writes a Go source file to a temp directory and returns its path.
func writeGoFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeGoFile: %v", err)
	}
	return path
}

// readFile reads a file and returns its contents as a string.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	return string(data)
}

// ─────────────────────────────────────────────
// RewriteFunctionBody tests
// ─────────────────────────────────────────────

// TestRewriteFunctionBody_PreservesSignature verifies the "genius touch":
// only the body is replaced, not the signature.
func TestRewriteFunctionBody_PreservesSignature(t *testing.T) {
	dir := t.TempDir()
	src := `package main

func GetAllUsers(db *sql.DB) ([]User, error) {
	return nil, nil
}
`
	path := writeGoFile(t, dir, "repo.go", src)

	newBody := `rows, err := db.Query("SELECT * FROM users")
if err != nil {
	return nil, err
}
defer rows.Close()
return nil, nil`

	if err := ast.RewriteFunctionBody(path, "GetAllUsers", newBody); err != nil {
		t.Fatalf("RewriteFunctionBody: %v", err)
	}

	result := readFile(t, path)

	// Signature must be preserved
	if !strings.Contains(result, "func GetAllUsers(db *sql.DB) ([]User, error)") {
		t.Error("function signature was not preserved")
	}
	// New body must be present
	if !strings.Contains(result, `SELECT * FROM users`) {
		t.Error("new body not written to file")
	}
}

// TestRewriteFunctionBody_CreatesStubIfMissing verifies that if the function
// doesn't exist, a stub is created (the "invent new business logic" feature).
func TestRewriteFunctionBody_CreatesStubIfMissing(t *testing.T) {
	dir := t.TempDir()
	src := `package main

func ExistingFunc() {
}
`
	path := writeGoFile(t, dir, "service.go", src)

	if err := ast.RewriteFunctionBody(path, "NewFunc", `return nil`); err != nil {
		t.Fatalf("RewriteFunctionBody stub creation: %v", err)
	}

	result := readFile(t, path)
	if !strings.Contains(result, "NewFunc") {
		t.Error("stub for missing function should have been created")
	}
	// Original function must be preserved
	if !strings.Contains(result, "ExistingFunc") {
		t.Error("existing function should not be removed when adding a stub")
	}
}

// TestRewriteFunctionBody_IdempotentBody verifies that applying the same
// body twice doesn't corrupt the file.
func TestRewriteFunctionBody_IdempotentBody(t *testing.T) {
	dir := t.TempDir()
	src := `package main

func Greet() string {
	return "hello"
}
`
	path := writeGoFile(t, dir, "greet.go", src)
	body := `return "hello world"`

	if err := ast.RewriteFunctionBody(path, "Greet", body); err != nil {
		t.Fatal(err)
	}
	if err := ast.RewriteFunctionBody(path, "Greet", body); err != nil {
		t.Fatal(err)
	}

	result := readFile(t, path)
	count := strings.Count(result, "func Greet")
	if count != 1 {
		t.Errorf("want exactly 1 Greet func, got %d", count)
	}
}

// TestRewriteFunctionBody_NonExistentFile verifies error on missing file.
func TestRewriteFunctionBody_NonExistentFile(t *testing.T) {
	err := ast.RewriteFunctionBody("/no/such/file.go", "Foo", `return nil`)
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

// TestRewriteFunctionBody_InvalidBody verifies that invalid Go syntax in
// the body returns an error rather than writing a broken file.
func TestRewriteFunctionBody_InvalidBody(t *testing.T) {
	dir := t.TempDir()
	src := `package main
func Foo() {}
`
	path := writeGoFile(t, dir, "foo.go", src)

	err := ast.RewriteFunctionBody(path, "Foo", `this is not valid go {{{`)
	if err == nil {
		t.Error("expected parse error for invalid body, got nil")
	}
}

// ─────────────────────────────────────────────
// UpsertRoute tests
// ─────────────────────────────────────────────

// TestUpsertRoute_AnchorInjection verifies Strategy 1: anchor comment injection.
func TestUpsertRoute_AnchorInjection(t *testing.T) {
	dir := t.TempDir()
	src := `package router

import "net/http"

func Register() {
	// ROUTES (auto-injected)
	_ = http.DefaultServeMux
}
`
	path := writeGoFile(t, dir, "router.go", src)

	ast.UpsertRoute(path, `http.HandleFunc("/users", handler)`, "handler")

	result := readFile(t, path)
	if !strings.Contains(result, `/users`) {
		t.Error("route not injected at anchor")
	}
}

// TestUpsertRoute_Idempotent verifies that injecting the same route twice
// doesn't duplicate it.
func TestUpsertRoute_Idempotent(t *testing.T) {
	dir := t.TempDir()
	src := `package router

func Register() {
	// ROUTES (auto-injected)
}
`
	path := writeGoFile(t, dir, "router.go", src)

	route := `http.HandleFunc("/users", handler)`
	ast.UpsertRoute(path, route, "handler")
	ast.UpsertRoute(path, route, "handler")

	result := readFile(t, path)
	count := strings.Count(result, "/users")
	if count != 1 {
		t.Errorf("route should appear exactly once, got %d", count)
	}
}

// TestUpsertRoute_FallbackInjection verifies Strategy 2: inject before
// last closing brace when anchor comment is missing.
func TestUpsertRoute_FallbackInjection(t *testing.T) {
	dir := t.TempDir()
	src := `package router

func Register() {
}
`
	path := writeGoFile(t, dir, "router.go", src)

	ast.UpsertRoute(path, `http.HandleFunc("/posts", postHandler)`, "postHandler")

	result := readFile(t, path)
	if !strings.Contains(result, "/posts") {
		t.Error("route not injected via fallback (no anchor present)")
	}
}
