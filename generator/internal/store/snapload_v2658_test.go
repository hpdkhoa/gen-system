// D098: load and save dropped every error. A broken snapshots.json gave an
// empty store with no message, and the next save overwrote the old records.
// A failed write also gave no message.
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapStore_BrokenFileIsNotOverwritten(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gen", "snapshots.json")
	broken := `[{"id":"snap-1","name":"a"},{"id":"snap-2","na`
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(broken), 0644); err != nil {
		t.Fatal(err)
	}
	s := NewSnapshotStore(root)
	if s.LoadErr() == nil {
		t.Fatal("want a load error for a broken snapshots.json")
	}
	if _, err := s.Create("new", "", "you", "default", "", 0, 0, 0, false); err == nil {
		t.Fatal("want Create to refuse while the file is broken")
	}
	got, _ := os.ReadFile(path)
	if string(got) != broken {
		t.Fatalf("the broken file was overwritten: %s", got)
	}
}

func TestSnapStore_FailedWriteIsReported(t *testing.T) {
	root := t.TempDir()
	// .gen is a file, so snapshots.json cannot be written
	if err := os.WriteFile(filepath.Join(root, ".gen"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	s := NewSnapshotStore(root)
	if _, err := s.Create("new", "", "you", "default", "", 0, 0, 0, false); err == nil {
		t.Fatal("want an error when the file cannot be written")
	}
	if n := len(s.All("")); n != 0 {
		t.Fatalf("memory holds %d records the file does not", n)
	}
}

func TestSnapStore_SaveAndReload(t *testing.T) {
	root := t.TempDir()
	s := NewSnapshotStore(root)
	rec, err := s.Create("one", "", "you", "default", "", 0, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	again := NewSnapshotStore(root)
	if again.LoadErr() != nil || again.Find(rec.ID) == nil {
		t.Fatalf("reload lost the record (load error %v)", again.LoadErr())
	}
	if _, err := os.Stat(filepath.Join(root, ".gen", "snapshots.json.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind (err %v)", err)
	}
}
