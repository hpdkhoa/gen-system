// D105: PyFrontend.Parse cut the path only at "/", so on Windows the path
// C:\proj\app\main.py gave the PackageName C:\proj\app\main and the module
// symbol took the whole path as its name.
package ast

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestPyFrontend_PackageNameIsBaseName_v2658(t *testing.T) {
	paths := []string{filepath.Join("proj", "app", "main.py"), "main.pyw"}
	if runtime.GOOS == "windows" {
		paths = append(paths, `C:\proj\app\main.py`, `app\main.py`)
	}
	for _, p := range paths {
		m, err := ForLanguage(LangPython).Parse(p, []byte("x = 1\n"))
		if err != nil {
			t.Fatalf("Parse(%q): %v", p, err)
		}
		if m.PackageName != "main" {
			t.Errorf("Parse(%q) PackageName = %q, want main", p, m.PackageName)
		}
	}
}
