// rollback_v2658_test.go (v26.58).
//
// D099: RestoreSnapshot copied the snapshot files back but never listed root,
// so files the failed run created stayed. RunFromPlan then printed "Rolled back
// to last working state" for a mix of old and new files.
package safety

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreSnapshot_RemovesFilesTheRunCreated(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	mainGo := filepath.Join(root, "main.go")
	must(os.WriteFile(mainGo, []byte("package main\n"), 0o644))
	must(os.MkdirAll(filepath.Join(root, ".gen"), 0o755))
	must(os.WriteFile(filepath.Join(root, ".gen", "memory.json"), []byte("{}"), 0o644))

	snap, err := SnapshotProject(root)
	if err != nil {
		t.Fatalf("SnapshotProject: %v", err)
	}

	// The failed run changes one file, adds one, and the healer writes state.
	must(os.WriteFile(mainGo, []byte("package main\nfunc broken( {\n"), 0o644))
	added := filepath.Join(root, "internal", "handler", "post_handler.go")
	must(os.MkdirAll(filepath.Dir(added), 0o755))
	must(os.WriteFile(added, []byte("package handler\nfunc x( {\n"), 0o644))
	genState := filepath.Join(root, ".gen", "trace.log")
	must(os.WriteFile(genState, []byte("log\n"), 0o644))

	if err := RestoreSnapshot(root, snap); err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	if got, _ := os.ReadFile(mainGo); string(got) != "package main\n" {
		t.Errorf("main.go not restored: %q", got)
	}
	if _, err := os.Stat(added); err == nil {
		t.Errorf("post_handler.go, created by the failed run, is still there")
	}
	// The .gen folder holds the snapshots and the tool's own state; keep it.
	if _, err := os.Stat(genState); err != nil {
		t.Errorf(".gen/trace.log was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snap, "main.go")); err != nil {
		t.Errorf("the snapshot itself was damaged: %v", err)
	}
}
