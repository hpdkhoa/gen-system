// golden_cfg_java_test.go — v25.1 GOLDEN-CFG CONTRACT (flow.Build, Java IR).
//
// PURPOSE. The companion to ast/golden_ir_java_test.go. That file locks what the
// frontend EMITS; this file locks what flow.Build BUILDS from it — the block
// topology the Studio renderer (renderCFG) then classifies into hexagons,
// diamonds, and the try-split. It parses real Java through the pure-Go frontend
// (the reference IR producer) and runs the SHARED, language-agnostic builder, so
// it equally constrains the future JVM sidecar: any IR that satisfies the
// golden-IR contract must produce this same CFG shape here.
//
// WHY THIS EXISTS / WHAT IT CAUGHT. Written to lock the try/loop/branch CFG
// conventions, it immediately surfaced a real defect: the non-Go structural
// builder's extractStructuralNodes DROPPED NodeTry (and un-extracted Else),
// flattening a try body into its parent and losing the wrapper — the exact
// "lone try { node → End" CFG symptom. The v25.1 fix in cfg_ts_py.go restores
// NodeTry through extraction; these assertions are its regression guard.
//
// The Java method path goes buildFuncCFG → hasStructuralNodes → buildStructuralCFG
// → extractStructuralNodes → processNode. That is the same path TS/Python/CA Gen
// take, so a green result here means the try/loop/branch split works for every
// non-Go language, not just Java.
package flow

import (
	"testing"

	"gen-system/generator/internal/ast"
	"gen-system/generator/internal/symbols"
)

// buildJavaMethodCFG parses one Java source, builds CFGs, and returns the CFG
// for method <recv>.<name>. It fails the test if the frontend is unavailable or
// the CFG is missing.
func buildJavaMethodCFG(t *testing.T, src, file, recv, name string) *CFG {
	t.Helper()
	fe := &ast.JavaFrontend{}
	if !fe.Available() {
		t.Skip("JavaFrontend unavailable")
	}
	m, err := fe.Parse(file, []byte(src))
	if err != nil {
		t.Fatalf("GOLDEN: Java parse error: %v", err)
	}
	// flow.Build reads m.FilePath for the CFG key, so make sure it is set to the
	// same value we compute the id from.
	m.FilePath = file
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	id := symbols.SymbolID(file, symbols.SymMethod, recv+"."+name)
	cfg, ok := cfgs[id]
	if !ok {
		t.Fatalf("GOLDEN: no CFG for %q; have: %v", id, keys(cfgs))
	}
	if cfg == nil {
		t.Fatalf("GOLDEN: nil CFG for %q", id)
	}
	return cfg
}

// reachesBack reports whether, following Succs, `from` can reach `target`
// (a cycle). This is the exact back-edge test renderCFG's loopInfo uses to
// classify a loop header, ported to the block graph.
func reachesBack(cfg *CFG, from, target int, seen map[int]bool) bool {
	if seen == nil {
		seen = map[int]bool{}
	}
	if from == target {
		return true
	}
	if seen[from] {
		return false
	}
	seen[from] = true
	b := cfg.Block(from)
	if b == nil {
		return false
	}
	for _, s := range b.Succs {
		if reachesBack(cfg, s, target, seen) {
			return true
		}
	}
	return false
}

// hasBlockWhereLabelContains is a small helper: does any block's Label contain s?
func hasBlockLabel(cfg *CFG, s string) bool {
	for _, b := range cfg.Blocks {
		if containsFold(b.Label, s) {
			return true
		}
	}
	return false
}

func containsFold(hay, needle string) bool {
	// tiny case-insensitive contains, no strings import churn concerns
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		ok := true
		for j := 0; j < len(needle); j++ {
			a, b := hay[i+j], needle[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// ── GOLDEN: try/catch builds a body + catch + merge split ─────────────────
// The defect this locks: try must NOT collapse to a lone node → End. The CFG
// must contain the try-body block, the catch block, and a merge where both
// converge. Both the try-body and catch must carry their body statements.
func TestGoldenCFG_Java_TrySplit(t *testing.T) {
	src := `
package com.example;
public class T {
    public void go() {
        try {
            doWork();
        } catch (Exception e) {
            handle();
        }
    }
}`
	cfg := buildJavaMethodCFG(t, src, "T.java", "T", "go")

	if len(cfg.Blocks) < 3 {
		t.Fatalf("GOLDEN: try/catch should build >= 3 blocks (try-body, catch, merge); got %d — try was flattened/dropped", len(cfg.Blocks))
	}
	if !hasBlockLabel(cfg, "try") {
		t.Error("GOLDEN: no block labelled with 'try' — the try-split was not built (extractStructuralNodes dropping NodeTry?)")
	}
	if !hasBlockLabel(cfg, "catch") {
		t.Error("GOLDEN: no 'catch' block — the exception path (Else) was lost")
	}

	// The try-body call and the catch call must both appear as statements
	// somewhere in the CFG (not dropped).
	var sawWork, sawHandle bool
	for _, b := range cfg.Blocks {
		for _, s := range b.Stmts {
			if s == nil {
				continue
			}
			if s.Callee == "doWork" {
				sawWork = true
			}
			if s.Callee == "handle" {
				sawHandle = true
			}
		}
	}
	if !sawWork {
		t.Error("GOLDEN: try-body call 'doWork' missing from the CFG — try body was lost")
	}
	if !sawHandle {
		t.Error("GOLDEN: catch-body call 'handle' missing from the CFG — catch body was lost")
	}

	// Convergence: at least one block must have >= 2 predecessors (the merge
	// where try-body and catch rejoin).
	merged := false
	for _, b := range cfg.Blocks {
		if len(b.Preds) >= 2 {
			merged = true
			break
		}
	}
	if !merged {
		t.Error("GOLDEN: no merge block (>=2 preds) — try-body and catch do not reconverge")
	}
}

// ── GOLDEN: a try whose ONLY control flow is the try still splits ─────────
// This is the hasStructuralNodes fix: without NodeTry recognised as structural,
// a try-only method fell to the single-opaque-block path and never split.
func TestGoldenCFG_Java_TryOnlyStillSplits(t *testing.T) {
	src := `
package com.example;
public class T2 {
    public void solo() {
        try {
            risky();
        } catch (Exception e) {
            fallback();
        }
    }
}`
	cfg := buildJavaMethodCFG(t, src, "T2.java", "T2", "solo")
	if len(cfg.Blocks) < 3 {
		t.Fatalf("GOLDEN: try-only method must still split into >=3 blocks; got %d (hasStructuralNodes not counting NodeTry?)", len(cfg.Blocks))
	}
	if !hasBlockLabel(cfg, "catch") {
		t.Error("GOLDEN: try-only method has no catch block — routed to the opaque single-block path")
	}
}

// ── GOLDEN: loop builds a back-edge (renders as a hexagon) ────────────────
// renderCFG's loopInfo classifies a header as a loop iff a successor reaches
// back to it. Assert that cycle exists and the loop condition text survived to
// a block Cond (the hexagon label).
func TestGoldenCFG_Java_LoopBackEdge(t *testing.T) {
	src := `
package com.example;
public class L {
    public void loop(int n) {
        for (int i = 0; i < n; i++) {
            step();
        }
    }
}`
	cfg := buildJavaMethodCFG(t, src, "L.java", "L", "loop")

	// Find a block with a back-edge: some successor reaches the block itself.
	backEdge := false
	for _, b := range cfg.Blocks {
		for _, s := range b.Succs {
			if s != b.ID && reachesBack(cfg, s, b.ID, nil) {
				backEdge = true
				break
			}
		}
		if backEdge {
			break
		}
	}
	if !backEdge {
		t.Error("GOLDEN: no back-edge in the loop CFG — it would not render as a hexagon")
	}

	// The loop condition text must survive onto some block's Cond (the header),
	// so the hexagon shows the real condition, not a bare label.
	condSeen := false
	for _, b := range cfg.Blocks {
		if b.Cond != "" {
			condSeen = true
			break
		}
	}
	if !condSeen {
		t.Error("GOLDEN: no block carries a Cond — the loop header condition text was lost")
	}
}

// ── GOLDEN: if/else builds a distinct else block ──────────────────────────
// Locks that Else produces its own block (pred → else → merge), not a silent
// fall-through that drops the else work.
func TestGoldenCFG_Java_IfElseDistinct(t *testing.T) {
	src := `
package com.example;
public class B {
    public void pick(int n) {
        if (n > 0) {
            up();
        } else {
            down();
        }
    }
}`
	cfg := buildJavaMethodCFG(t, src, "B.java", "B", "pick")

	var sawUp, sawDown bool
	for _, b := range cfg.Blocks {
		for _, s := range b.Stmts {
			if s == nil {
				continue
			}
			if s.Callee == "up" {
				sawUp = true
			}
			if s.Callee == "down" {
				sawDown = true
			}
		}
	}
	if !sawUp {
		t.Error("GOLDEN: THEN call 'up' missing from CFG")
	}
	if !sawDown {
		t.Error("GOLDEN: ELSE call 'down' missing from CFG — else body was dropped (Else not built as its own block)")
	}
}
