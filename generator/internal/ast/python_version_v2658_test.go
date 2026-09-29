// D075: CheckPython3Available compared the minor version as a string, so
// "14" >= "9" was false and Python 3.10 and later counted as missing.
package ast

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakePython3 puts a python3 on PATH that prints the given version line.
func fakePython3(t *testing.T, version string) {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		body := "@echo off\r\necho " + version + "\r\n"
		if err := os.WriteFile(filepath.Join(dir, "python3.bat"), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATHEXT", ".BAT")
	} else {
		body := "#!/bin/sh\necho '" + version + "'\n"
		if err := os.WriteFile(filepath.Join(dir, "python3"), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestCheckPython3Available_ComparesNumbers_v2658(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"Python 3.14.0", true},
		{"Python 3.10.2", true},
		{"Python 3.9.18", true},
		{"Python 3.8.10", false},
		{"Python 2.7.18", false},
		{"Python 3.x", false},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			fakePython3(t, tc.version)
			if got := CheckPython3Available(); got != tc.want {
				t.Errorf("CheckPython3Available with %q = %v, want %v", tc.version, got, tc.want)
			}
		})
	}
}
