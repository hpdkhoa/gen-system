// ast/ts_cfg_test.go — TASK 2 (v19): tests for parseFuncBody structural
// node emission. Verifies that NodeBranch/NodeLoop/NodeReturn/NodeCall
// are emitted alongside the opaque body_not_parsed wrapper.
package ast

import (
	"strings"
	"testing"
)

// ─── helpers ──────────────────────────────────────────────────

func countKindInChildren(n *Node, k NodeKind) int {
	count := 0
	for _, c := range n.Children {
		if c.Kind == k {
			count++
		}
	}
	return count
}

func firstFuncNode(t *testing.T, src string) *Node {
	t.Helper()
	m := parseTSMust(t, src)
	funcs := m.TopLevelByKind(NodeFunction)
	if len(funcs) == 0 {
		t.Fatal("no top-level function found")
	}
	return funcs[0]
}

// ─── opaque body still present ────────────────────────────────

func TestTSFrontend_BodyWithIf_HasOpaqueWrapper(t *testing.T) {
	src := `function greet(name: string) {
  if (name === "") {
    return "nobody"
  }
  return "hello " + name
}`
	fn := firstFuncNode(t, src)
	var hasOpaque bool
	for _, c := range fn.Children {
		if c.Kind == NodeUnknown && c.UnknownReason == "body_not_parsed" {
			hasOpaque = true
		}
	}
	if !hasOpaque {
		t.Error("opaque body_not_parsed wrapper must still be present alongside structural nodes")
	}
}

// ─── branch detection ─────────────────────────────────────────

func TestTSFrontend_BodyWithIf_EmitsBranch(t *testing.T) {
	src := `function check(x: number): string {
  if (x > 0) {
    return "positive"
  }
  return "non-positive"
}`
	fn := firstFuncNode(t, src)
	branches := countKindInChildren(fn, NodeBranch)
	if branches == 0 {
		t.Error("expected at least one NodeBranch child for function with 'if'")
	}
}

func TestTSFrontend_BodyWithSwitch_EmitsBranch(t *testing.T) {
	src := `function label(code: number) {
  switch (code) {
    case 0: return "zero"
    case 1: return "one"
    default: return "other"
  }
}`
	fn := firstFuncNode(t, src)
	branches := countKindInChildren(fn, NodeBranch)
	if branches == 0 {
		t.Error("expected at least one NodeBranch child for function with 'switch'")
	}
}

func TestTSFrontend_BodyWithTry_EmitsBranch(t *testing.T) {
	src := `function safeParse(s: string) {
  try {
    return JSON.parse(s)
  } catch (e) {
    return null
  }
}`
	fn := firstFuncNode(t, src)
	branches := countKindInChildren(fn, NodeBranch)
	if branches == 0 {
		t.Error("expected at least one NodeBranch child for function with 'try'")
	}
}

// ─── loop detection ───────────────────────────────────────────

func TestTSFrontend_BodyWithFor_EmitsLoop(t *testing.T) {
	src := `function sumAll(nums: number[]): number {
  let total = 0
  for (const n of nums) { total += n }
  return total
}`
	fn := firstFuncNode(t, src)
	loops := countKindInChildren(fn, NodeLoop)
	if loops == 0 {
		t.Error("expected at least one NodeLoop child for function with 'for'")
	}
}

func TestTSFrontend_BodyWithWhile_EmitsLoop(t *testing.T) {
	src := `function countdown(n: number) {
  while (n > 0) { n-- }
}`
	fn := firstFuncNode(t, src)
	if countKindInChildren(fn, NodeLoop) == 0 {
		t.Error("expected NodeLoop for function with 'while'")
	}
}

// ─── return detection ─────────────────────────────────────────

func TestTSFrontend_BodyWithReturn_EmitsReturn(t *testing.T) {
	src := `function id(x: number): number { return x }`
	fn := firstFuncNode(t, src)
	if countKindInChildren(fn, NodeReturn) == 0 {
		t.Error("expected NodeReturn for function with 'return'")
	}
}

func TestTSFrontend_BodyWithThrow_EmitsReturn(t *testing.T) {
	src := `function fail(msg: string) { throw new Error(msg) }`
	fn := firstFuncNode(t, src)
	// 'throw' is treated as NodeReturn (escape).
	if countKindInChildren(fn, NodeReturn) == 0 {
		t.Error("expected NodeReturn for function with 'throw'")
	}
}

// ─── call detection ───────────────────────────────────────────

func TestTSFrontend_BodyWithCall_EmitsCall(t *testing.T) {
	src := `function run() {
  doSomething()
  processResult()
}`
	fn := firstFuncNode(t, src)
	calls := countKindInChildren(fn, NodeCall)
	if calls == 0 {
		t.Error("expected at least one NodeCall child for function with call expressions")
	}
}

// ─── empty body ───────────────────────────────────────────────

func TestTSFrontend_EmptyBody_NoStructural(t *testing.T) {
	src := `function noop() {}`
	fn := firstFuncNode(t, src)
	// Only the opaque wrapper should be present.
	for _, c := range fn.Children {
		if c.Kind != NodeUnknown {
			t.Errorf("empty body should only have NodeUnknown child, got %s", c.Kind)
		}
	}
}

// ─── existing tests stay green ────────────────────────────────

// TestTSFrontend_ParseFunction_StructuralDoesNotBreakSignature verifies
// that adding structural body nodes doesn't corrupt function metadata.
// Note: parseParamList intentionally returns nil (params are recorded as
// unknown via body_not_parsed); only name/exported/async are asserted.
func TestTSFrontend_ParseFunction_StructuralDoesNotBreakSignature(t *testing.T) {
	src := `export async function fetchUser(id: string): Promise<User> {
  if (!id) { throw new Error("no id") }
  const user = await getUser(id)
  return user
}`
	m := parseTSMust(t, src)
	funcs := m.TopLevelByKind(NodeFunction)
	if len(funcs) != 1 {
		t.Fatalf("expected 1 function, got %d", len(funcs))
	}
	fn := funcs[0]
	if fn.Name != "fetchUser" {
		t.Errorf("name: want fetchUser, got %s", fn.Name)
	}
	if !fn.Exported {
		t.Error("should be exported")
	}
	if !fn.Async {
		t.Error("should be async")
	}
	// Verify structural nodes are present alongside the opaque wrapper.
	if countKindInChildren(fn, NodeBranch) == 0 {
		t.Error("expected at least one NodeBranch (from 'if')")
	}
	if countKindInChildren(fn, NodeReturn) == 0 {
		t.Error("expected at least one NodeReturn (from 'throw' or 'return')")
	}
}

// v23.49 — TS for/while loops capture the parenthesised condition into RawSource.
func TestTSFrontend_LoopConditionCaptured_v2349(t *testing.T) {
	src := `function scan(arr: number[]): number {
  let sum = 0
  for (let i = 0; i < arr.length; i++) {
    sum += arr[i]
  }
  while (sum > 100) {
    sum -= 10
  }
  return sum
}`
	fn := firstFuncNode(t, src)
	var forCond, whileCond string
	var walk func(ns []*Node)
	walk = func(ns []*Node) {
		for _, n := range ns {
			if n.Kind == NodeLoop {
				if n.Name == "for" && forCond == "" {
					forCond = n.RawSource
				}
				if n.Name == "while" && whileCond == "" {
					whileCond = n.RawSource
				}
			}
			walk(n.Children)
		}
	}
	walk(fn.Children)
	if forCond == "" || !strings.Contains(forCond, "arr.length") {
		t.Errorf("for condition not captured: got %q", forCond)
	}
	if whileCond == "" || !strings.Contains(whileCond, "sum") {
		t.Errorf("while condition not captured: got %q", whileCond)
	}
}
