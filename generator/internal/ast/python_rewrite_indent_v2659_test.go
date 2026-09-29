// N10: the text fallback of the Python rewrite script (rewrite_text, used when
// ast.unparse is missing) wrote each new body line with its own indent
// stripped. The lines of an if or a loop body landed at the same depth as the
// if line, so the nesting was lost and the rewritten file did not even parse.
package ast

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPythonRewriteText_KeepsBodyNesting_v2659(t *testing.T) {
	py := findPython(t)
	script := buildPythonRewriteScript("unused.py", "f", "")
	start := strings.Index(script, "def rewrite_text(")
	end := strings.Index(script, "with open(FILE_PATH")
	if start < 0 || end < start {
		t.Fatal("rewrite_text not found in the rewrite script")
	}
	// The new body comes with a common indent of 8 spaces, which must go;
	// the if body is 4 spaces deeper than the if line, which must stay.
	body := "        if x:\n            y = 1\n        else:\n            y = 2\n        return y\n"
	// The script imports textwrap at its top; the driver takes only the function.
	driver := "import textwrap\n" + script[start:end] + `
import sys
src = "def f(x):\n    return 0\ndef g():\n    return 1\n"
out, err = rewrite_text(src, "f", ` + "sys.argv[1]" + `)
if out is None:
    print("ERROR:", err)
    sys.exit(1)
sys.stdout.write(out)
`
	dir := t.TempDir()
	drv := filepath.Join(dir, "driver.py")
	if err := os.WriteFile(drv, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(py, drv, body).CombinedOutput()
	if err != nil {
		t.Fatalf("rewrite_text failed: %v\n%s", err, out)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	want := "def f(x):\n    if x:\n        y = 1\n    else:\n        y = 2\n    return y\ndef g():\n    return 1\n"
	if got != want {
		t.Fatalf("rewrite_text result:\n%s\nwant:\n%s", got, want)
	}
	// The result must be valid Python that runs the nested branch.
	target := filepath.Join(dir, "mod.py")
	if err := os.WriteFile(target, []byte(got), 0o644); err != nil {
		t.Fatal(err)
	}
	check := "import runpy, sys; ns = runpy.run_path(sys.argv[1]); print(ns['f'](True), ns['f'](False))"
	res, err := exec.Command(py, "-c", check, target).CombinedOutput()
	if err != nil {
		t.Fatalf("rewritten file does not run: %v\n%s", err, res)
	}
	if s := strings.TrimSpace(string(res)); s != "1 2" {
		t.Errorf("f(True), f(False) printed %q, want \"1 2\"", s)
	}
}
