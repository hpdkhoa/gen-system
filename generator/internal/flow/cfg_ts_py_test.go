// flow/cfg_ts_py_test.go — tests for TS/Python structural CFG builder
package flow

import (
	"testing"

	"gen-system/generator/internal/ast"
	"gen-system/generator/internal/symbols"
)

// ─── hasStructuralNodes ───────────────────────────────────────

func TestHasStructuralNodes_Empty(t *testing.T) {
	if hasStructuralNodes(nil) {
		t.Error("nil children should return false")
	}
	if hasStructuralNodes([]*ast.Node{}) {
		t.Error("empty children should return false")
	}
}

func TestHasStructuralNodes_OnlyCall(t *testing.T) {
	children := []*ast.Node{{Kind: ast.NodeCall, Callee: "fn"}}
	// Calls alone are not structural (no branching).
	if hasStructuralNodes(children) {
		t.Error("NodeCall alone should not count as structural")
	}
}

func TestHasStructuralNodes_WithBranch(t *testing.T) {
	children := []*ast.Node{
		{Kind: ast.NodeCall, Callee: "fn"},
		{Kind: ast.NodeBranch},
	}
	if !hasStructuralNodes(children) {
		t.Error("NodeBranch should be detected as structural")
	}
}

func TestHasStructuralNodes_WithReturn(t *testing.T) {
	children := []*ast.Node{{Kind: ast.NodeReturn}}
	if !hasStructuralNodes(children) {
		t.Error("NodeReturn should be detected as structural")
	}
}

func TestHasStructuralNodes_WithLoop(t *testing.T) {
	children := []*ast.Node{{Kind: ast.NodeLoop}}
	if !hasStructuralNodes(children) {
		t.Error("NodeLoop should be detected as structural")
	}
}

func TestHasStructuralNodes_NestedInUnknown(t *testing.T) {
	// TS frontend wraps body in NodeUnknown{reason:"body_not_parsed"}
	opaqueBody := &ast.Node{
		Kind:          ast.NodeUnknown,
		UnknownReason: "body_not_parsed",
		Children: []*ast.Node{
			{Kind: ast.NodeCall, Callee: "fetch"},
			{Kind: ast.NodeBranch},
		},
	}
	if !hasStructuralNodes([]*ast.Node{opaqueBody}) {
		t.Error("NodeBranch nested in body_not_parsed wrapper should be detected")
	}
}

func TestHasStructuralNodes_OpaqueUnknownNoChildren(t *testing.T) {
	opaqueBody := &ast.Node{
		Kind:          ast.NodeUnknown,
		UnknownReason: "body_not_parsed",
	}
	if hasStructuralNodes([]*ast.Node{opaqueBody}) {
		t.Error("opaque body with no children should not count as structural")
	}
}

// ─── extractStructuralNodes ───────────────────────────────────

func TestExtractStructuralNodes_UnwrapsOpaqueBody(t *testing.T) {
	branch := &ast.Node{Kind: ast.NodeBranch}
	call := &ast.Node{Kind: ast.NodeCall, Callee: "fn"}
	opaque := &ast.Node{
		Kind:          ast.NodeUnknown,
		UnknownReason: "body_not_parsed",
		Children:      []*ast.Node{call, branch},
	}
	out := extractStructuralNodes([]*ast.Node{opaque})
	// Should contain call and branch (structural nodes from inside wrapper).
	if len(out) == 0 {
		t.Fatal("extracted nodes should be non-empty")
	}
	kinds := map[ast.NodeKind]bool{}
	for _, n := range out {
		kinds[n.Kind] = true
	}
	if !kinds[ast.NodeBranch] {
		t.Error("NodeBranch should be extracted from opaque wrapper")
	}
	if !kinds[ast.NodeCall] {
		t.Error("NodeCall should be extracted from opaque wrapper")
	}
}

func TestExtractStructuralNodes_NilSafe(t *testing.T) {
	// Should not panic.
	out := extractStructuralNodes([]*ast.Node{nil, nil})
	if len(out) != 0 {
		t.Errorf("nil nodes should produce empty output, got %d", len(out))
	}
}

// ─── TS multi-block CFG ───────────────────────────────────────

func tsFuncModule(name string, children ...*ast.Node) *ast.Module {
	return &ast.Module{
		Language: ast.LangTS,
		FilePath: "src/service.ts",
		Nodes: []*ast.Node{
			{
				Kind:     ast.NodeFunction,
				Name:     name,
				Exported: true,
				Children: children,
			},
		},
	}
}

func TestBuild_TS_SingleBlock_OpaqueBody(t *testing.T) {
	// Function with fully opaque body — no structural nodes.
	opaque := &ast.Node{
		Kind:          ast.NodeUnknown,
		UnknownReason: "body_not_parsed",
		RawSource:     "const x = doSomething()",
	}
	m := tsFuncModule("HandleRequest", opaque)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)

	funcID := symbols.SymbolID("src/service.ts", symbols.SymFunc, "HandleRequest")
	cfg := cfgs[funcID]
	if cfg == nil {
		t.Fatal("CFG should exist for TS function")
	}
	// No structural nodes → single block.
	if len(cfg.Blocks) != 1 {
		t.Errorf("opaque TS body should produce 1 block, got %d", len(cfg.Blocks))
	}
}

func TestBuild_TS_MultiBlock_WithBranch(t *testing.T) {
	// Function with a branch node inside opaque body wrapper.
	opaque := &ast.Node{
		Kind:          ast.NodeUnknown,
		UnknownReason: "body_not_parsed",
		Children: []*ast.Node{
			{Kind: ast.NodeCall, Callee: "validate"},
			{Kind: ast.NodeBranch, Children: []*ast.Node{
				{Kind: ast.NodeCall, Callee: "handleValid"},
			}},
			{Kind: ast.NodeReturn},
		},
	}
	m := tsFuncModule("ProcessRequest", opaque)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)

	funcID := symbols.SymbolID("src/service.ts", symbols.SymFunc, "ProcessRequest")
	cfg := cfgs[funcID]
	if cfg == nil {
		t.Fatal("CFG should exist")
	}
	// Branch should produce at least: entry, then, merge = 3 blocks.
	if len(cfg.Blocks) < 3 {
		t.Errorf("TS function with branch should produce ≥3 blocks, got %d", len(cfg.Blocks))
	}
}

func TestBuild_TS_MultiBlock_WithLoop(t *testing.T) {
	opaque := &ast.Node{
		Kind:          ast.NodeUnknown,
		UnknownReason: "body_not_parsed",
		Children: []*ast.Node{
			{Kind: ast.NodeLoop, Children: []*ast.Node{
				{Kind: ast.NodeCall, Callee: "processItem"},
			}},
		},
	}
	m := tsFuncModule("ProcessAll", opaque)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)

	funcID := symbols.SymbolID("src/service.ts", symbols.SymFunc, "ProcessAll")
	cfg := cfgs[funcID]
	if cfg == nil {
		t.Fatal("CFG should exist")
	}
	// Loop: entry, header, body, exit = 4 blocks.
	if len(cfg.Blocks) < 3 {
		t.Errorf("TS function with loop should produce ≥3 blocks, got %d", len(cfg.Blocks))
	}
}

func TestBuild_TS_ReturnAddsExit(t *testing.T) {
	opaque := &ast.Node{
		Kind:          ast.NodeUnknown,
		UnknownReason: "body_not_parsed",
		Children:      []*ast.Node{{Kind: ast.NodeReturn}},
	}
	m := tsFuncModule("EarlyReturn", opaque)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	funcID := symbols.SymbolID("src/service.ts", symbols.SymFunc, "EarlyReturn")
	cfg := cfgs[funcID]
	if cfg == nil {
		t.Fatal("CFG should exist")
	}
	if len(cfg.Exits) == 0 {
		t.Error("NodeReturn should add to cfg.Exits")
	}
}

// ─── Python multi-block CFG ───────────────────────────────────

func pyFuncModule(name string, children ...*ast.Node) *ast.Module {
	return &ast.Module{
		Language: ast.LangPython,
		FilePath: "service.py",
		Nodes: []*ast.Node{
			{
				Kind:     ast.NodeFunction,
				Name:     name,
				Exported: true,
				Children: children,
			},
		},
	}
}

func TestBuild_Python_MultiBlock_WithBranch(t *testing.T) {
	// Python frontend emits children directly (no opaque wrapper).
	m := pyFuncModule("process",
		&ast.Node{Kind: ast.NodeCall, Callee: "validate"},
		&ast.Node{Kind: ast.NodeBranch},
		&ast.Node{Kind: ast.NodeReturn},
	)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	funcID := symbols.SymbolID("service.py", symbols.SymFunc, "process")
	cfg := cfgs[funcID]
	if cfg == nil {
		t.Fatal("CFG should exist for Python function")
	}
	if len(cfg.Blocks) < 3 {
		t.Errorf("Python function with branch should have ≥3 blocks, got %d", len(cfg.Blocks))
	}
}

// ─── buildStructuralCFG directly ─────────────────────────────

func TestBuildStructuralCFG_Empty(t *testing.T) {
	n := &ast.Node{Kind: ast.NodeFunction, Name: "Empty"}
	cfg := buildStructuralCFG("empty.ts#func:Empty", n)
	if cfg == nil {
		t.Fatal("buildStructuralCFG returned nil")
	}
	if len(cfg.Blocks) == 0 {
		t.Error("should have at least entry block")
	}
}
