// bodyprovider_v2658_test.go — v26.58 (D017): bodyProviderFor cached the bytes
// of each file for the life of the closure and also cached a failed read. With
// --enrich-inline --watch, an edit to a source file updated the index, but the
// provider cut the new span from the old bytes, so the enricher got empty or
// shifted text and a wrong source hash.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func parseInto(t *testing.T, idx *symbols.Index, path string, update bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ast.ParseFile(path, data)
	if err != nil || m == nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if update {
		idx.UpdateFile(path, m)
	} else {
		idx.AddModule(m)
	}
}

func targetID(t *testing.T, idx *symbols.Index) string {
	t.Helper()
	for _, s := range idx.ByName("Target") {
		if s.Kind == symbols.SymFunc {
			return s.ID
		}
	}
	t.Fatal("Target not in index")
	return ""
}

func TestBodyProvider_ReadsFileAgainAfterEdit(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	if err := os.WriteFile(p, []byte("package a\n\nfunc Target() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := symbols.NewIndex()
	parseInto(t, idx, p, false)
	body := bodyProviderFor(idx)
	if got := body(targetID(t, idx)); !strings.Contains(got, "return 1") {
		t.Fatalf("first body = %q, want the original body", got)
	}

	// The watcher sees an edit that moves Target and changes its body.
	edited := "package a\n\n// Helper comes first now.\nfunc Helper() {}\n\nfunc Target() int {\n\treturn 2222\n}\n"
	if err := os.WriteFile(p, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	parseInto(t, idx, p, true)
	got := body(targetID(t, idx))
	if !strings.Contains(got, "return 2222") || !strings.HasPrefix(got, "func Target") {
		t.Errorf("body after edit = %q, want the new body of Target", got)
	}
}

func TestBodyProvider_DoesNotCacheFailedRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	src := []byte("package a\n\nfunc Target() int { return 7 }\n")
	if err := os.WriteFile(p, src, 0o644); err != nil {
		t.Fatal(err)
	}
	idx := symbols.NewIndex()
	parseInto(t, idx, p, false)
	id := targetID(t, idx)
	body := bodyProviderFor(idx)

	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if got := body(id); got != "" {
		t.Fatalf("body of a missing file = %q, want empty", got)
	}
	if err := os.WriteFile(p, src, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := body(id); !strings.Contains(got, "return 7") {
		t.Errorf("body after the file came back = %q, want the body", got)
	}
}
