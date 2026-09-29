// snapshot_vcs_v2659_test.go: v26.59 (N02).
//
// SnapshotProject copied .git and node_modules, and RestoreSnapshot copied
// them back and then deleted every file the snapshot lacked. A git commit or a
// package install made after the snapshot was lost on a rollback. The delete
// walk also left the folders the failed run created, empty.
package safety

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreSnapshot_LeavesGitAndNodeModulesAlone(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel, body string) string {
		t.Helper()
		p := filepath.Join(root, rel)
		must(os.MkdirAll(filepath.Dir(p), 0o755))
		must(os.WriteFile(p, []byte(body), 0o644))
		return p
	}
	write("main.go", "package main\n")
	head := write(filepath.Join(".git", "HEAD"), "ref: refs/heads/main\n")
	write(filepath.Join(".git", "refs", "heads", "main"), "aaaa\n")
	write(filepath.Join("node_modules", "left-pad", "index.js"), "v1\n")
	write(filepath.Join("web", "node_modules", "x", "index.js"), "v1\n")

	snap, err := SnapshotProject(root)
	if err != nil {
		t.Fatalf("SnapshotProject: %v", err)
	}
	for _, rel := range []string{".git", "node_modules", filepath.Join("web", "node_modules")} {
		if _, err := os.Stat(filepath.Join(snap, rel)); err == nil {
			t.Errorf("the snapshot holds %s", rel)
		}
	}

	// After the snapshot the user commits and installs packages, and the
	// failed run creates a file in new folders.
	must(os.WriteFile(head, []byte("ref: refs/heads/feature\n"), 0o644))
	ref := write(filepath.Join(".git", "refs", "heads", "main"), "bbbb\n")
	obj := write(filepath.Join(".git", "objects", "ab", "cdef"), "blob\n")
	pkg := write(filepath.Join("node_modules", "left-pad", "index.js"), "v2\n")
	newPkg := write(filepath.Join("node_modules", "is-odd", "index.js"), "v1\n")
	nested := write(filepath.Join("web", "node_modules", "y", "index.js"), "v1\n")
	added := write(filepath.Join("internal", "handler", "post_handler.go"), "package handler\n")

	if err := RestoreSnapshot(root, snap); err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}

	check := func(p, want string) {
		t.Helper()
		got, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s was removed: %v", p, err)
			return
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", p, got, want)
		}
	}
	check(head, "ref: refs/heads/feature\n")
	check(ref, "bbbb\n")
	check(obj, "blob\n")
	check(pkg, "v2\n")
	check(newPkg, "v1\n")
	check(nested, "v1\n")

	if _, err := os.Stat(added); err == nil {
		t.Errorf("post_handler.go, created by the failed run, is still there")
	}
	for _, rel := range []string{filepath.Join("internal", "handler"), "internal"} {
		if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
			t.Errorf("empty folder %s, created by the failed run, is still there", rel)
		}
	}
	check(filepath.Join(root, "main.go"), "package main\n")
}

// A folder the snapshot holds stays, even when it is empty after the restore.
func TestRestoreSnapshot_KeepsEmptyFolderFromSnapshot(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "assets")
	if err := os.MkdirAll(keep, 0o755); err != nil {
		t.Fatal(err)
	}
	snap, err := SnapshotProject(root)
	if err != nil {
		t.Fatalf("SnapshotProject: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keep, "new.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RestoreSnapshot(root, snap); err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("assets, held by the snapshot, was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(keep, "new.png")); err == nil {
		t.Errorf("assets/new.png, created after the snapshot, is still there")
	}
}
