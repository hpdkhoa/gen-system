package ast

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RewritePythonFunction replaces the body of a Python function while
// preserving its signature, decorators, and surrounding code.
//
// Strategy: generate a small Python script that uses the ast module
// to parse the file, locate the target function by name, replace its
// body statements with the newBody string, then unparse back to source.
//
// Requires: Python ≥ 3.9 (ast.unparse was added in 3.9).
// Falls back to a regex-based approach for older Python versions.
func RewritePythonFunction(filePath, funcName, newBody string) error {
	// Verify the file exists
	if _, err := os.Stat(filePath); err != nil {
		return fmt.Errorf("python rewrite: file not found: %s", filePath)
	}

	// The rewriter script runs as a Python subprocess.
	// It reads the source, rewrites the function body, writes back.
	script := buildPythonRewriteScript(filePath, funcName, newBody)

	tmpScript := filepath.Join(os.TempDir(), "gen_py_rewrite.py")
	if err := os.WriteFile(tmpScript, []byte(script), 0600); err != nil {
		return fmt.Errorf("python rewrite: write script: %w", err)
	}
	defer os.Remove(tmpScript)

	cmd := exec.Command("python3", tmpScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("python rewrite: script failed: %s\n%s", err, string(out))
	}

	output := strings.TrimSpace(string(out))
	if strings.HasPrefix(output, "ERROR:") {
		return fmt.Errorf("python rewrite: %s", output)
	}

	fmt.Printf("  [ast/python] rewrote %s.%s\n", filepath.Base(filePath), funcName)
	return nil
}

// buildPythonRewriteScript constructs the Python rewriter script.
// The script uses the ast module for parsing and ast.unparse for
// serialization, falling back to a text-based approach on Python < 3.9.
func buildPythonRewriteScript(filePath, funcName, newBody string) string {
	// Escape the new body for embedding in the script
	escapedBody := strings.ReplaceAll(newBody, `\`, `\\`)
	escapedBody = strings.ReplaceAll(escapedBody, `"`, `\"`)
	escapedBody = strings.ReplaceAll(escapedBody, "\n", `\n`)

	return fmt.Sprintf(`
import ast, sys, os, textwrap

FILE_PATH  = %q
FUNC_NAME  = %q
NEW_BODY   = %q

def rewrite_ast(source, func_name, new_body_src):
    """Parse source, replace function body, unparse back to text."""
    tree = ast.parse(source)

    # Parse the new body as a standalone block
    new_body_src_dedented = textwrap.dedent(new_body_src)
    try:
        new_stmts = ast.parse(new_body_src_dedented).body
    except SyntaxError as e:
        return None, f"new body parse error: {e}"

    replaced = False
    for node in ast.walk(tree):
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            if node.name == func_name:
                node.body = new_stmts if new_stmts else [ast.Pass()]
                # Fix line numbers after mutation
                ast.fix_missing_locations(node)
                replaced = True
                break

    if not replaced:
        return None, f"function '{func_name}' not found"

    try:
        result = ast.unparse(tree)  # Python >= 3.9
        return result, None
    except AttributeError:
        return None, "ast.unparse not available (requires Python >= 3.9)"

def rewrite_text(source, func_name, new_body_src):
    """Fallback: text-based indentation-aware rewrite for Python < 3.9."""
    lines = source.splitlines(keepends=True)
    in_func = False
    func_indent = ""
    body_indent = ""
    start_line = -1
    end_line = -1

    for i, line in enumerate(lines):
        stripped = line.lstrip()
        if not in_func:
            if stripped.startswith(f"def {func_name}(") or stripped.startswith(f"async def {func_name}("):
                in_func = True
                func_indent = line[:len(line) - len(stripped)]
                body_indent = func_indent + "    "
                start_line = i + 1
        else:
            if line.strip() == "":
                continue
            curr_indent = line[:len(line) - len(line.lstrip())]
            if len(curr_indent) <= len(func_indent) and line.strip():
                end_line = i
                break
    else:
        if in_func:
            end_line = len(lines)

    if start_line == -1:
        return None, f"function '{func_name}' not found (text mode)"

    new_lines = []
    for l in new_body_src.splitlines():
        new_lines.append(body_indent + l.strip() + "\n")
    if not new_lines:
        new_lines = [body_indent + "pass\n"]

    result = "".join(lines[:start_line] + new_lines + lines[end_line:])
    return result, None

with open(FILE_PATH, "r") as f:
    source = f.read()

result, err = rewrite_ast(source, FUNC_NAME, NEW_BODY)
if result is None:
    result, err = rewrite_text(source, FUNC_NAME, NEW_BODY)

if result is None:
    print(f"ERROR: {err}")
    sys.exit(1)

with open(FILE_PATH, "w") as f:
    f.write(result)

print(f"OK: rewrote {FUNC_NAME} in {FILE_PATH}")
`, filePath, funcName, escapedBody)
}

// CheckPython3Available returns true if python3 ≥ 3.9 is on PATH.
// Used by the healer to decide whether Python rewriting is available.
func CheckPython3Available() bool {
	out, err := exec.Command("python3", "--version").Output()
	if err != nil {
		return false
	}
	// Parse "Python 3.X.Y"
	version := strings.TrimSpace(string(out))
	parts := strings.Split(version, " ")
	if len(parts) < 2 {
		return false
	}
	vparts := strings.Split(parts[1], ".")
	if len(vparts) < 2 {
		return false
	}
	major := vparts[0]
	minor := vparts[1]
	return major == "3" && minor >= "9"
}
