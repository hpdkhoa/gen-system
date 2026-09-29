// snapshot_test.go — v26.57 (D015): a snapshot must never copy the snapshot
// folder. The skip test compared the path with ".gen/snapshots" and a forward
// slash; filepath.Walk gives backslashes on Windows, so the walk entered
// .gen\snapshots, copied the snapshot being written into itself, and nested
// until the path grew too long. RunFromPlan then ran with no rollback.
package safety

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotProject_SkipsTheSnapshotFolder(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644))
	old := filepath.Join(root, ".gen", "snapshots", "20260101_000000")
	must(os.MkdirAll(old, 0o755))
	must(os.WriteFile(filepath.Join(old, "main.go"), []byte("package old\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, ".gen", "memory.json"), []byte("{}"), 0o644))

	snap, err := SnapshotProject(root)
	if err != nil {
		t.Fatalf("SnapshotProject: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snap, "main.go")); err != nil {
		t.Errorf("project file missing from the snapshot: %v", err)
	}
	// Other .gen state is still part of the project and must be kept.
	if _, err := os.Stat(filepath.Join(snap, ".gen", "memory.json")); err != nil {
		t.Errorf(".gen/memory.json missing from the snapshot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snap, ".gen", "snapshots")); err == nil {
		t.Errorf("the snapshot holds a copy of the snapshot folder")
	}
}
