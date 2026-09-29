// D076: buildPythonRewriteScript escaped the new body by hand and then quoted
// it with %q, so the body reached Python escaped twice. rewrite_ast failed to
// parse it, rewrite_text wrote it as one line with literal \n and \" text, and
// the script still printed OK over a broken Python file.
package ast

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// findPython returns a Python interpreter on PATH, or skips the test.
func findPython(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			if out, err := exec.Command(p, "--version").Output(); err == nil && strings.HasPrefix(string(out), "Python 3") {
				return p
			}
		}
	}
	t.Skip("no Python 3 on PATH")
	return ""
}

func TestBuildPythonRewriteScript_BodyReachesPythonOnce_v2658(t *testing.T) {
	py := findPython(t)
	cases := []struct {
		name, body, want string
	}{
		{"defect trigger", "x = 1\nprint(\"hi\")\nreturn x", "hi\n1"},
		// a backslash escape in Python code, JSON escaped <>&, and non ASCII text
		{"hard characters", "s = 'a\\tb<c>&é\U0001F600'\nreturn len(s)", "9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "mod.py")
			if err := os.WriteFile(target, []byte("def f():\n    return 0\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			script := filepath.Join(dir, "rewrite.py")
			if err := os.WriteFile(script, []byte(buildPythonRewriteScript(target, "f", tc.body)), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(py, script)
			cmd.Env = append(os.Environ(), "PYTHONUTF8=1")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("rewrite script failed: %v\n%s", err, out)
			}
			src, _ := os.ReadFile(target)
			if strings.Contains(string(src), `\n`) || strings.Contains(string(src), `\"`) {
				t.Errorf("rewritten file holds escape text:\n%s", src)
			}
			check := "import runpy, sys; print(runpy.run_path(sys.argv[1])['f']())"
			run := exec.Command(py, "-c", check, target)
			run.Env = append(os.Environ(), "PYTHONUTF8=1")
			out, err := run.CombinedOutput()
			if err != nil {
				t.Fatalf("rewritten file does not run: %v\n%s\nfile:\n%s", err, out, src)
			}
			if got := strings.ReplaceAll(strings.TrimSpace(string(out)), "\r\n", "\n"); got != tc.want {
				t.Errorf("f() printed %q, want %q\nfile:\n%s", got, tc.want, src)
			}
		})
	}
}
