// D096: SnapshotService.Create replaced a failed project copy with an empty
// SnapPath and a nil error, so a later restore changed nothing, and it never
// passed the auto flag, so automatic snapshots carried auto=false.
package services

import (
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/api/sse"
	"github.com/hpdkhoa/gen-system/generator/internal/store"
)

func TestSnapshotCreate_FailedCopyIsAnError(t *testing.T) {
	base := t.TempDir()
	st := store.NewSnapshotStore(base)
	missing := filepath.Join(base, "no-such-root")
	s := NewSnapshotService(st, sse.NewBroker(), missing)

	rec, err := s.Create("snap", "", "you", "default", true)
	if err == nil {
		t.Fatalf("want an error when the project copy fails, got record %+v", rec)
	}
	if n := len(st.All("")); n != 0 {
		t.Fatalf("a failed copy stored %d records", n)
	}
}

func TestSnapshotCreate_KeepsAutoFlag(t *testing.T) {
	root := t.TempDir()
	s := NewSnapshotService(store.NewSnapshotStore(root), sse.NewBroker(), root)

	rec, err := s.Create("auto", "", "agent", "default", true)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Auto || rec.SnapPath == "" {
		t.Fatalf("want Auto true and a SnapPath, got %+v", rec)
	}
	// the flag must also reach the stored file
	again := store.NewSnapshotStore(root).Find(rec.ID)
	if again == nil || !again.Auto {
		t.Fatalf("stored record lost the auto flag: %+v", again)
	}
}
