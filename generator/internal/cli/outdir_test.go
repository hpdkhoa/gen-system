// generator/internal/cli/outdir_test.go, v23.19: lock in the output-safety
// policy so a refactor can't silently let generation overwrite the source tree
// or an existing app.
package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLooksLikeGenSourceTree(t *testing.T) {
	// A dir containing a gen-system go.mod is the source tree.
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module gen-system\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !looksLikeGenSourceTree(src) {
		t.Error("expected gen-system go.mod dir to be detected as source tree")
	}
	// A subdir of it is still inside the source tree (walk-up).
	sub := filepath.Join(src, "generated-app")
	os.MkdirAll(sub, 0o755)
	if !looksLikeGenSourceTree(sub) {
		t.Error("expected subdir of source tree to be detected (walk-up)")
	}
	// The generator/internal/engine marker also counts.
	eng := t.TempDir()
	os.MkdirAll(filepath.Join(eng, "generator", "internal", "engine"), 0o755)
	if !looksLikeGenSourceTree(eng) {
		t.Error("expected generator/internal/engine marker to be detected")
	}
	// An ordinary app dir is not the source tree.
	app := t.TempDir()
	os.WriteFile(filepath.Join(app, "go.mod"), []byte("module app\n\ngo 1.22\n"), 0o644)
	if looksLikeGenSourceTree(app) {
		t.Error("an ordinary app dir must not be flagged as source tree")
	}
}

func TestHasGeneratedApp(t *testing.T) {
	d := t.TempDir()
	if hasGeneratedApp(d) {
		t.Error("empty dir should not look like a generated app")
	}
	os.MkdirAll(filepath.Join(d, "backend"), 0o755)
	os.WriteFile(filepath.Join(d, "backend", "main.go"), []byte("package main\n"), 0o644)
	if !hasGeneratedApp(d) {
		t.Error("dir with backend/main.go should be detected as a generated app")
	}
}

func TestResolveOutDir_RefusesSourceTree(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "go.mod"), []byte("module gen-system\n\ngo 1.22\n"), 0o644)
	_, ok := ResolveOutDir(OutDirOptions{Flag: src, DefaultName: "output", Command: "apply"})
	if ok {
		t.Error("ResolveOutDir must refuse the gen-system source tree")
	}
}

func TestResolveOutDir_FreshDirOK(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "myapp")
	got, ok := ResolveOutDir(OutDirOptions{Flag: dir, DefaultName: "output", Command: "apply"})
	if !ok || got != dir {
		t.Errorf("fresh dir should be accepted: ok=%v got=%q", ok, got)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("output dir should have been created: %v", err)
	}
}

func TestResolveOutDir_ForceOverwrite(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "myapp")
	os.MkdirAll(filepath.Join(dir, "backend"), 0o755)
	os.WriteFile(filepath.Join(dir, "backend", "main.go"), []byte("package main\n"), 0o644)
	// With Force, an existing app is overwritten without prompting.
	_, ok := ResolveOutDir(OutDirOptions{Flag: dir, DefaultName: "output", Command: "apply", Force: true})
	if !ok {
		t.Error("Force should allow overwriting an existing app")
	}
}

func TestResolveEvolveTarget_StagesByDefault(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "app")
	os.MkdirAll(filepath.Join(target, "backend"), 0o755)
	os.WriteFile(filepath.Join(target, "backend", "main.go"), []byte("package main\n"), 0o644)

	writeDir, inPlace, ok := ResolveEvolveTarget(target)
	if !ok || inPlace {
		t.Fatalf("default evolve should stage (ok=%v inPlace=%v)", ok, inPlace)
	}
	if writeDir == target {
		t.Error("staging dir must differ from the target source")
	}
	// The review dir is seeded from the target (the file was copied).
	if _, err := os.Stat(filepath.Join(writeDir, "backend", "main.go")); err != nil {
		t.Errorf("review dir should be seeded from target: %v", err)
	}
	// The original source is untouched.
	if _, err := os.Stat(filepath.Join(target, "backend", "main.go")); err != nil {
		t.Errorf("original source must remain: %v", err)
	}
}
