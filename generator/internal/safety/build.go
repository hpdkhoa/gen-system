package safety

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ValidateBuild runs `go build` over all packages in the given directory.
//
// Returns:
//   - output: combined stdout+stderr from the build command.
//     This is the raw compiler error text needed for
//     HashError() and the intelligence repair pipeline.
//   - error:  non-nil if the build failed.
//
// The output is always populated on failure so callers can pass
// it directly to intelligence.ClassifyError() and HashError().
//
// v23.29 (test-fix): build artifacts are written to a throwaway -o directory
// instead of the package directory. Plain `go build ./...` compiles main
// packages into the current directory named after the package's base dir, so a
// module whose main package lives in `cmd/` made Go fail with
// "build output cmd already exists and is a directory": a false build failure
// that had nothing to do with the code. Directing output to a temp dir (which
// `go build` happily overwrites) makes ValidateBuild a pure compile check that
// never collides with the source layout.
func ValidateBuild(path string) (string, error) {
	outDir, mkErr := os.MkdirTemp("", "gen-validate-build-*")
	if mkErr == nil {
		defer os.RemoveAll(outDir)
	}

	args := []string{"build"}
	if outDir != "" {
		// -o a directory: go build places each compiled binary there, keeping
		// the source tree untouched. Non-main packages still type-check.
		args = append(args, "-o", outDir+string(filepath.Separator))
	}
	args = append(args, "./...")

	cmd := exec.Command("go", args...)
	cmd.Dir = path

	out, err := cmd.CombinedOutput()
	combined := strings.TrimSpace(string(out))

	if err != nil {
		if combined == "" {
			combined = err.Error()
		}
		return combined, fmt.Errorf("build failed in %s: %w", path, err)
	}

	return combined, nil
}

// ValidateBuildQuiet is a convenience wrapper that returns only the error.
// Use this when you don't need the compiler output (e.g. simple pass/fail).
func ValidateBuildQuiet(path string) error {
	_, err := ValidateBuild(path)
	return err
}
