package ast

import (
	"os"
	"path/filepath"
	"testing"
)

// v26.74: a disk entry written by another parser version is not loaded. The cache keyed only on path and checksum,
// so after a parser fix a run in the same folder got the old parse of every unchanged file.
func TestCacheSkipsOtherParserVersion(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.go")
	if err := os.WriteFile(src, []byte("package a\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewCache(filepath.Join(dir, "cache"))
	m, err := c.ParseOrCache(src)
	if err != nil || m == nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := NewCache(filepath.Join(dir, "cache")).Get(m.FilePath, m.Checksum); !ok {
		t.Fatal("an entry of this version was not loaded")
	}
	// rewrite every entry as if an older parser had written it
	files, _ := filepath.Glob(filepath.Join(dir, "cache", "*.json"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		b = []byte(string(b[:6]) + "old" + string(b[6+len(CacheVersion):]))
		_ = os.WriteFile(f, b, 0o644)
	}
	if _, ok := NewCache(filepath.Join(dir, "cache")).Get(m.FilePath, m.Checksum); ok {
		t.Fatal("an entry of another parser version was loaded")
	}
}
