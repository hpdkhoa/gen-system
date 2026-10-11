// state_v2659_test.go (N11): LoadIRAt treated every read failure as a first
// run. Only a missing snapshot is a first run; a snapshot that exists but
// cannot be read must reach the caller as an error.
package compiler

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIRAt_MissingIsFirstRun(t *testing.T) {
	ir, err := LoadIRAt(t.TempDir())
	if err != nil || len(ir.Entities) != 0 {
		t.Fatalf("missing snapshot: got %d entities, err %v; want an empty IR and no error", len(ir.Entities), err)
	}
}

func TestLoadIRAt_UnreadableSnapshotIsAnError(t *testing.T) {
	root := t.TempDir()
	// A folder where the snapshot file must be: it exists, but ReadFile fails.
	if err := os.MkdirAll(filepath.Join(root, ".gen", "ir_snapshot.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIRAt(root); err == nil {
		t.Fatal("LoadIRAt read an unreadable snapshot as a first run; want an error")
	}
}
