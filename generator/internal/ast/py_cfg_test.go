// ast/py_cfg_test.go (TASK 2, v19): tests for Python structural body nodes.
// Skipped automatically when python3/python is unavailable in the sandbox.
package ast

import (
	"testing"
)

func requirePyFE(t *testing.T) *PyFrontend {
	t.Helper()
	fe := &PyFrontend{}
	if !fe.Available() {
		t.Skip("python3/python not found in PATH; skipping Python CFG tests")
	}
	return fe
}

func pyParse(t *testing.T, src string) *Module {
	t.Helper()
	fe := requirePyFE(t)
	m, err := fe.Parse("test.py", []byte(src))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(m.ParseErrors) > 0 {
		t.Fatalf("parse errors: %v", m.ParseErrors)
	}
	return m
}

func pyFuncNode(t *testing.T, m *Module, name string) *Node {
	t.Helper()
	for _, n := range m.Nodes {
		if n.Kind == NodeFunction && n.Name == name {
			return n
		}
	}
	t.Fatalf("function %q not found in module", name)
	return nil
}

// ─── branch detection ─────────────────────────────────────────

func TestPyFrontend_BodyWithIf_EmitsBranch(t *testing.T) {
	src := `
def check(x):
    if x > 0:
        return "positive"
    return "non-positive"
`
	m := pyParse(t, src)
	fn := pyFuncNode(t, m, "check")
	count := countKindInChildren(fn, NodeBranch)
	if count == 0 {
		t.Error("expected at least one NodeBranch child for Python function with 'if'")
	}
}

// v26.25: renamed from _EmitsBranch. This test predated NodeTry: v25.1 made
// try/except its own kind (Children = try body, Else = handler), which the
// structural CFG builder recognises explicitly (flow/cfg_ts_py.go). Asserting
// NodeBranch here was asserting the old contract against a frontend that had
// correctly moved to the new one.
func TestPyFrontend_BodyWithTry_EmitsTry(t *testing.T) {
	src := `
def safe_parse(s):
    try:
        return int(s)
    except ValueError:
        return 0
`
	m := pyParse(t, src)
	fn := pyFuncNode(t, m, "safe_parse")
	count := countKindInChildren(fn, NodeTry)
	if count == 0 {
		t.Error("expected NodeTry for Python function with 'try'")
	}
}

// ─── loop detection ───────────────────────────────────────────

func TestPyFrontend_BodyWithFor_EmitsLoop(t *testing.T) {
	src := `
def sum_all(nums):
    total = 0
    for n in nums:
        total += n
    return total
`
	m := pyParse(t, src)
	fn := pyFuncNode(t, m, "sum_all")
	if countKindInChildren(fn, NodeLoop) == 0 {
		t.Error("expected NodeLoop for Python function with 'for'")
	}
}

func TestPyFrontend_BodyWithWhile_EmitsLoop(t *testing.T) {
	src := `
def countdown(n):
    while n > 0:
        n -= 1
`
	m := pyParse(t, src)
	fn := pyFuncNode(t, m, "countdown")
	if countKindInChildren(fn, NodeLoop) == 0 {
		t.Error("expected NodeLoop for Python function with 'while'")
	}
}

// ─── return / raise detection ─────────────────────────────────

func TestPyFrontend_BodyWithReturn_EmitsReturn(t *testing.T) {
	src := `
def identity(x):
    return x
`
	m := pyParse(t, src)
	fn := pyFuncNode(t, m, "identity")
	if countKindInChildren(fn, NodeReturn) == 0 {
		t.Error("expected NodeReturn for Python function with 'return'")
	}
}

func TestPyFrontend_BodyWithRaise_EmitsReturn(t *testing.T) {
	src := `
def fail(msg):
    raise ValueError(msg)
`
	m := pyParse(t, src)
	fn := pyFuncNode(t, m, "fail")
	// 'raise' maps to NodeReturn (escape).
	if countKindInChildren(fn, NodeReturn) == 0 {
		t.Error("expected NodeReturn for Python function with 'raise'")
	}
}

// ─── call detection ───────────────────────────────────────────

func TestPyFrontend_BodyWithCall_EmitsCall(t *testing.T) {
	src := `
def run():
    do_something()
    process_result()
`
	m := pyParse(t, src)
	fn := pyFuncNode(t, m, "run")
	if countKindInChildren(fn, NodeCall) == 0 {
		t.Error("expected NodeCall for Python function with call statements")
	}
}

// ─── existing behaviour preserved ────────────────────────────

func TestPyFrontend_BodyAlwaysHasOpaqueMarker(t *testing.T) {
	src := `
def greet(name):
    if name:
        return "hello " + name
    return "hi"
`
	m := pyParse(t, src)
	fn := pyFuncNode(t, m, "greet")
	var hasOpaque bool
	for _, c := range fn.Children {
		if c.Kind == NodeUnknown && c.UnknownReason == "body_not_captured" {
			hasOpaque = true
		}
	}
	if !hasOpaque {
		t.Error("body_not_captured opaque marker must still be present")
	}
}

// ─── CFG integration ─────────────────────────────────────────

func TestPyFrontend_BodyBranch_ProducesMultiBlockCFG(t *testing.T) {
	src := `
def process(x):
    if x > 0:
        return x
    return 0
`
	m := pyParse(t, src)
	// Verify the children include structural nodes that cfg builder will use.
	fn := pyFuncNode(t, m, "process")
	branches := countKindInChildren(fn, NodeBranch)
	returns := countKindInChildren(fn, NodeReturn)
	if branches == 0 {
		t.Error("process should have branch nodes")
	}
	if returns == 0 {
		t.Error("process should have return nodes")
	}
}
