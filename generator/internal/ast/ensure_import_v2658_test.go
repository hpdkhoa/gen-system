// D077: EnsureImport dropped the error from writeFile (and from parsing), so
// a failed write left the file with no import and the caller saw no message.
// The trigger named in the defect, a parenthesized import block, no longer
// fails since v26.57 (D012, TestAutoResolveImports_AddsPlainImports); these
// tests make the write fail on purpose.
package ast

import (
	"os"
	"path/filepath"
	"testing"
)

// readOnlyGoFile writes src to a file and makes it read only, or skips the
// test when the file can still be opened for writing (for example as root).
func readOnlyGoFile(t *testing.T, src string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.go")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	if f, err := os.OpenFile(p, os.O_WRONLY, 0); err == nil {
		f.Close()
		t.Skip("a read only file is still writable here")
	}
	return p
}

const blockImportSrc = "package x\n\nimport (\n\t\"fmt\"\n)\n\nfunc F() { fmt.Println() }\n"

func TestEnsureImport_ReturnsWriteError_v2658(t *testing.T) {
	p := readOnlyGoFile(t, blockImportSrc)
	if err := EnsureImport(p, "strings"); err == nil {
		t.Fatal("EnsureImport on a read only file returned nil, want the write error")
	}
	// An import that is already there needs no write and is not an error.
	if err := EnsureImport(p, "fmt"); err != nil {
		t.Fatalf("EnsureImport for a present import: %v", err)
	}
}

func TestEnsureImport_ReturnsParseError_v2658(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.go")
	if err := os.WriteFile(p, []byte("package x\n\nfunc F( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureImport(p, "strings"); err == nil {
		t.Fatal("EnsureImport on a file that does not parse returned nil")
	}
}

func TestAutoResolveImports_ReturnsError_v2658(t *testing.T) {
	p := readOnlyGoFile(t, blockImportSrc)
	if err := AutoResolveImports(p, "return strings.ToUpper(s)"); err == nil {
		t.Fatal("AutoResolveImports on a read only file returned nil, want the write error")
	}
}
