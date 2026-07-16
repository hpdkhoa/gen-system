// golden_ir_java_test.go — v25.1 GOLDEN-IR CONTRACT (Java frontend).
//
// PURPOSE. This file is the executable specification of the unified-IR shape a
// frontend must emit for the shared CFG builder (flow/flow.go) and the Studio
// renderer (web/studio-v23.html renderCFG) to work. It is written against the
// pure-Go JavaFrontend, which is the reference implementation, so the spec is
// "green" today and LOCKS the NodeTry / NodeLoop / NodeBranch conventions the
// recent CFG fixes established (v24c–v24g).
//
// WHY THIS EXISTS. A future JVM SIDECAR will produce this SAME unified IR as
// JSON over a process boundary. Unlike every in-process Go frontend, a sidecar
// that disagrees with the IR contract will NOT fail to compile — it will just
// emit subtly-wrong IR that renders as a broken CFG. This test is the guard: it
// pins the exact node kinds and field conventions (Children = body, Else =
// catch/else, RawSource = condition text) so that (a) a convention change is a
// deliberate, visible edit to THIS file, and (b) the sidecar has a concrete,
// runnable target to match. Point cfg-diag.sh at a sidecar-parsed JAR and the
// backend-truth section should mirror what these assertions require.
//
// SCOPE. Structural/IR-shape only — it asserts what the frontend EMITS. The
// companion golden_cfg_java_test.go (package flow) asserts what flow.Build then
// BUILDS from this IR. Together they cover both sides of the contract line.
package ast

import "testing"

// The reference fixture: one method exercising each control-flow construct the
// CFG cares about — a loop (with condition), an if/else (with condition), and a
// try/catch. Kept deliberately small and explicit so an assertion failure names
// exactly which convention drifted.
const goldenJavaSrc = `
package com.example;

public class Sample {
    public void run(int n) {
        Logger log = new Logger();
        for (int i = 0; i < n; i++) {
            log.tick(i);
        }
        if (n > 0) {
            log.positive();
        } else {
            log.nonpositive();
        }
        try {
            log.risky();
        } catch (Exception e) {
            log.recover();
        }
    }
}
`

// goldenMethod parses the fixture and returns the run() method node.
func goldenMethod(t *testing.T) *Node {
	t.Helper()
	m := parseJava(t, goldenJavaSrc)
	method := findNode(m.Nodes, NodeMethod, "run")
	if method == nil {
		t.Fatal("GOLDEN: run() method not found — frontend no longer emits NodeMethod for it")
	}
	return method
}

// ── CONTRACT 1: the method body carries structural children ───────────────
// The CFG builder reads flow from method.Children. If a frontend leaves the
// body opaque (all in RawSource, no structural nodes), every method collapses
// to Start→End. This asserts the reference produces real structural nodes.
func TestGoldenIR_Java_MethodHasStructuralBody(t *testing.T) {
	method := goldenMethod(t)
	if len(method.Children) == 0 {
		t.Fatal("GOLDEN: run() has no Children — body did not parse into IR nodes")
	}
	// Receiver must be set so flow.Build keys the CFG as <Recv>.<name>.
	if method.Receiver == nil || method.Receiver.Name != "Sample" {
		t.Errorf("GOLDEN: method.Receiver = %v, want {Name:Sample} (flow.Build keys the CFG on it)", method.Receiver)
	}
}

// ── CONTRACT 2: NodeLoop with condition in RawSource, body in Children ─────
// renderCFG draws a loop as a hexagon and shows RawSource as the condition.
// flow.go NodeLoop reads n.RawSource into the header block's Cond. The body
// must be in Children so it lands in the loop-body block.
func TestGoldenIR_Java_LoopConvention(t *testing.T) {
	method := goldenMethod(t)
	loop := findNode(method.Children, NodeLoop, "")
	if loop == nil {
		t.Fatal("GOLDEN: no NodeLoop for the for-statement — loops would not render as hexagons")
	}
	if loop.RawSource == "" {
		t.Error("GOLDEN: NodeLoop.RawSource is empty — the loop condition text is the hexagon label; frontend must capture it")
	}
	// The body call must be reachable under the loop (directly or via nested
	// harvested nodes) — otherwise the loop body is empty in the CFG.
	if findNode(loop.Children, NodeCall, "tick") == nil {
		t.Error("GOLDEN: loop body call 'tick' not under NodeLoop.Children — loop body would render empty")
	}
}

// ── CONTRACT 3: NodeBranch with condition + distinct Else ─────────────────
// flow.go NodeBranch: Children = THEN body, Else = ELSE body (v23.7). A missing
// Else means the false path silently falls to merge and the else work vanishes.
func TestGoldenIR_Java_BranchConvention(t *testing.T) {
	method := goldenMethod(t)
	branch := findNode(method.Children, NodeBranch, "if")
	if branch == nil {
		t.Fatal("GOLDEN: no NodeBranch named 'if' — if/else would not render as a decision")
	}
	if branch.RawSource == "" {
		t.Error("GOLDEN: NodeBranch.RawSource empty — decision diamond would show literal 'decision', not the real condition")
	}
	if findNode(branch.Children, NodeCall, "positive") == nil {
		t.Error("GOLDEN: THEN body call 'positive' not in NodeBranch.Children")
	}
	if len(branch.Else) == 0 {
		t.Fatal("GOLDEN: NodeBranch.Else is empty — the ELSE body must live in Else (v23.7), not merged into Children")
	}
	if findNode(branch.Else, NodeCall, "nonpositive") == nil {
		t.Error("GOLDEN: ELSE body call 'nonpositive' not in NodeBranch.Else")
	}
}

// ── CONTRACT 4: NodeTry with body in Children and catch in Else ───────────
// THE convention the try-CFG fix depends on. flow.go NodeTry: Children = try
// body (normal path), Else = catch/finally (exception path); it builds try-body
// + catch + merge. If a frontend puts the catch in Children (or drops it), the
// CFG cannot distinguish the exception path.
func TestGoldenIR_Java_TryConvention(t *testing.T) {
	method := goldenMethod(t)
	try := findNode(method.Children, NodeTry, "")
	if try == nil {
		t.Fatal("GOLDEN: no NodeTry — try/catch would not render as a branch-like split")
	}
	if findNode(try.Children, NodeCall, "risky") == nil {
		t.Error("GOLDEN: try-body call 'risky' not in NodeTry.Children — try body would render empty")
	}
	if len(try.Else) == 0 {
		t.Fatal("GOLDEN: NodeTry.Else is empty — catch/finally must live in Else, not Children")
	}
	if findNode(try.Else, NodeCall, "recover") == nil {
		t.Error("GOLDEN: catch-body call 'recover' not in NodeTry.Else")
	}
	// The catch body must NOT also be in the try body (would double-count and
	// put the exception path on the normal path).
	if findNode(try.Children, NodeCall, "recover") != nil {
		t.Error("GOLDEN: catch call 'recover' leaked into NodeTry.Children — it belongs only in Else")
	}
}

// ── CONTRACT 5: the vocabulary is stable ──────────────────────────────────
// A compact assertion that the exact set of control-flow kinds the CFG builder
// switches on is present. This is the single line a sidecar author reads to
// know "these are the kinds I must emit for this body".
func TestGoldenIR_Java_VocabularyPresent(t *testing.T) {
	method := goldenMethod(t)
	want := []NodeKind{NodeLoop, NodeBranch, NodeTry, NodeCall}
	for _, k := range want {
		if findNode(method.Children, k, "") == nil {
			t.Errorf("GOLDEN: expected at least one %s in run() body; the CFG builder relies on it", k)
		}
	}
}
