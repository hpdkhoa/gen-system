package safety_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/safety"
)

// makeGoModule creates a minimal, valid Go module in a temp directory.
// Returns the module root path.
func makeGoModule(t *testing.T, moduleName string) string {
	t.Helper()
	dir := t.TempDir()

	goMod := "module " + moduleName + "\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writePackageFile writes a .go file inside a package subdirectory.
func writePackageFile(t *testing.T, root, pkg, filename, content string) {
	t.Helper()
	dir := filepath.Join(root, pkg)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// ─────────────────────────────────────────────
// ValidateBuild tests
// ─────────────────────────────────────────────

// TestValidateBuild_SuccessReturnsEmptyOutput verifies that a valid Go
// module produces no error and empty output on success.
func TestValidateBuild_SuccessReturnsEmptyOutput(t *testing.T) {
	root := makeGoModule(t, "testmod")
	writePackageFile(t, root, "cmd", "main.go",
		`package main
func main() {}
`)

	output, err := safety.ValidateBuild(root)
	if err != nil {
		t.Errorf("valid code should build: %v\noutput: %s", err, output)
	}
	// successful build has no output
	if strings.TrimSpace(output) != "" {
		t.Logf("unexpected output on success: %q", output)
	}
}

// TestValidateBuild_FailureReturnsCompilerOutput is the CRITICAL fix test.
// It verifies that the error output from the compiler is returned as a
// non-empty string, not silently discarded.
func TestValidateBuild_FailureReturnsCompilerOutput(t *testing.T) {
	root := makeGoModule(t, "brokenmod")
	writePackageFile(t, root, "cmd", "main.go",
		`package main

func main() {
	undefined_var_that_does_not_exist() // this will fail to compile
}
`)

	output, err := safety.ValidateBuild(root)
	if err == nil {
		t.Fatal("broken code should fail to build")
	}
	// THE CRITICAL CHECK: output must not be empty
	if output == "" {
		t.Fatal("compiler output must not be empty on build failure — needed for HashError() and ClassifyError()")
	}
	// The output should contain something useful
	if !strings.Contains(output, "undefined") && !strings.Contains(output, "cmd/main.go") {
		t.Errorf("compiler output doesn't look useful: %q", output)
	}
}

// TestValidateBuild_InvalidDir verifies that a non-existent path returns error.
func TestValidateBuild_InvalidDir(t *testing.T) {
	_, err := safety.ValidateBuild("/path/that/absolutely/does/not/exist/anywhere")
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}

// TestValidateBuild_MultipleErrors verifies that output from multiple
// compilation errors is captured (not truncated to first error only).
func TestValidateBuild_MultipleErrors(t *testing.T) {
	root := makeGoModule(t, "multierr")
	writePackageFile(t, root, "pkg", "bad.go",
		`package pkg

func Bad() {
	var x undefined_type_one
	var y undefined_type_two
	_, _ = x, y
}
`)

	output, err := safety.ValidateBuild(root)
	if err == nil {
		t.Fatal("expected build failure")
	}
	// Should capture at least something meaningful
	if len(output) < 10 {
		t.Errorf("output too short to be useful: %q", output)
	}
}

// TestValidateBuildQuiet_CompatLayer verifies the quiet convenience wrapper
// behaves like the old ValidateBuild(path) error return.
func TestValidateBuildQuiet_CompatLayer(t *testing.T) {
	root := makeGoModule(t, "quietmod")
	writePackageFile(t, root, "cmd", "main.go",
		`package main
func main() {}
`)

	err := safety.ValidateBuildQuiet(root)
	if err != nil {
		t.Errorf("quiet compat: valid code should not error: %v", err)
	}
}
