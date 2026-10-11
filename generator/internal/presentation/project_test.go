// project_test.go (v24): security tests for dynamic-project root validation.
// The load endpoint must only accept roots inside the configured projects base;
// path traversal and symlink escapes must be rejected.
package presentation

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func newTestPM(base string) *ProjectManager {
	// srv/loader/watcher are nil; validateRoot doesn't touch them.
	return NewProjectManager(nil, base, nil, nil)
}

func TestValidateRoot_AcceptsDescendant_v24(t *testing.T) {
	base := t.TempDir()
	proj := filepath.Join(base, "sample-go")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	pm := newTestPM(base)
	got, err := pm.validateRoot(proj)
	if err != nil {
		t.Fatalf("expected descendant to be accepted, got error: %v", err)
	}
	// EvalSymlinks may prepend /private on macOS; compare via EvalSymlinks.
	want, _ := filepath.EvalSymlinks(proj)
	if got != want {
		t.Errorf("resolved root: got %q want %q", got, want)
	}
}

func TestValidateRoot_AcceptsBaseItself_v24(t *testing.T) {
	base := t.TempDir()
	pm := newTestPM(base)
	if _, err := pm.validateRoot(base); err != nil {
		t.Errorf("base itself should be accepted, got: %v", err)
	}
}

func TestValidateRoot_RejectsOutside_v24(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir() // a sibling temp dir, not under base
	pm := newTestPM(base)
	if _, err := pm.validateRoot(outside); err == nil {
		t.Error("expected a root outside the base to be rejected")
	}
}

func TestValidateRoot_RejectsTraversal_v24(t *testing.T) {
	base := t.TempDir()
	// base/sub exists; base/sub/../../escape tries to climb out.
	sub := filepath.Join(base, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	pm := newTestPM(base)
	traversal := filepath.Join(sub, "..", "..", filepath.Base(base)+"-sibling")
	if _, err := pm.validateRoot(traversal); err == nil {
		t.Error("expected a traversal path climbing out of base to be rejected")
	}
}

func TestValidateRoot_RejectsEmpty_v24(t *testing.T) {
	pm := newTestPM(t.TempDir())
	if _, err := pm.validateRoot(""); err == nil {
		t.Error("expected empty root to be rejected")
	}
	if _, err := pm.validateRoot("   "); err == nil {
		t.Error("expected whitespace root to be rejected")
	}
}

func TestValidateRoot_RejectsFile_v24(t *testing.T) {
	base := t.TempDir()
	f := filepath.Join(base, "notadir.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pm := newTestPM(base)
	if _, err := pm.validateRoot(f); err == nil {
		t.Error("expected a file (non-directory) root to be rejected")
	}
}

func TestValidateRoot_RejectsSymlinkEscape_v24(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	base := t.TempDir()
	outside := t.TempDir()
	// base/link → outside : a symlink inside base pointing out of it.
	link := filepath.Join(base, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlink in this environment: %v", err)
	}
	pm := newTestPM(base)
	if _, err := pm.validateRoot(link); err == nil {
		t.Error("expected a symlink escaping the base to be rejected (EvalSymlinks must resolve it)")
	}
}
