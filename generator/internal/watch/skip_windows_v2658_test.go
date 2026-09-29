// D107: filepath.Rel gives backslashes on Windows, but builtinSkip listed
// .gen/snapshots and .gen/entities with a forward slash. The watcher parsed every
// source copied into a snapshot, so each project symbol showed up twice.
package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func TestD107_BuiltinSkipOSPaths(t *testing.T) {
	for _, rel := range []string{
		filepath.Join(".gen", "snapshots", "20260101_000000", "main.go"),
		filepath.Join(".gen", "entities", "user.go"),
		filepath.Join("vendor", "a.go"),
	} {
		if !builtinSkip(rel) {
			t.Errorf("builtinSkip(%q) = false, want true", rel)
		}
	}
}

func TestD107_ScanDirSkipsSnapshotCopies(t *testing.T) {
	root := t.TempDir()
	snap := filepath.Join(root, ".gen", "snapshots", "20260101_000000")
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	src := []byte("package main\n\nfunc main() {}\n")
	for _, p := range []string{filepath.Join(root, "main.go"), filepath.Join(snap, "main.go")} {
		if err := os.WriteFile(p, src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w := New(symbols.NewIndex(), root, time.Hour)
	w.scanDir(false)
	st := w.Snapshot()
	if _, ok := st[filepath.Join(snap, "main.go")]; ok {
		t.Errorf("scanDir recorded the snapshot copy; state: %v", st)
	}
	if _, ok := st[filepath.Join(root, "main.go")]; !ok {
		t.Errorf("scanDir missed the project file; state: %v", st)
	}
}
