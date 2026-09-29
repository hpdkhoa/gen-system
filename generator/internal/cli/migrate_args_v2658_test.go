// D023: RunMigrate indexed schemaPath[0] without a length check, so
// `gen migrate ""` (for example an unset shell variable) panicked with
// "index out of range" instead of printing the usage text.
package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn and returns what it printed to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() { os.Stdout = old }()
	fn()
	w.Close()
	os.Stdout = old
	return <-done
}

func TestRunMigrate_EmptySchemaPath(t *testing.T) {
	t.Chdir(t.TempDir())
	var out string
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("RunMigrate([\"\"]) panicked: %v", r)
			}
		}()
		out = captureStdout(t, func() { RunMigrate([]string{""}) })
	}()
	if !strings.Contains(out, "Usage: gen migrate") {
		t.Errorf("want the usage text, got %q", out)
	}
}
