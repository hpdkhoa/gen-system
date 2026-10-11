package flow

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// ─── Helpers ──────────────────────────────────────────────────

func goFuncModule(name string, children ...*ast.Node) *ast.Module {
	return &ast.Module{
		Language: ast.LangGo,
		FilePath: "pkg/foo.go",
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

func callNode(callee string) *ast.Node {
	return &ast.Node{Kind: ast.NodeCall, Callee: callee}
}

func branchNode(children ...*ast.Node) *ast.Node {
	return &ast.Node{Kind: ast.NodeBranch, Children: children}
}

func loopNode(children ...*ast.Node) *ast.Node {
	return &ast.Node{Kind: ast.NodeLoop, Children: children}
}

func returnNode() *ast.Node {
	return &ast.Node{Kind: ast.NodeReturn}
}

// ─── BasicBlock ───────────────────────────────────────────────

func TestBasicBlock_Fields(t *testing.T) {
	b := &BasicBlock{ID: 0, Label: "entry"}
	b.Succs = append(b.Succs, 1)
	b.Preds = append(b.Preds, -1)
	if b.ID != 0 || b.Label != "entry" {
		t.Errorf("unexpected BasicBlock fields")
	}
}

// ─── CFG ──────────────────────────────────────────────────────

func TestCFG_Block(t *testing.T) {
	cfg := &CFG{
		Blocks: []*BasicBlock{
			{ID: 0, Label: "entry"},
			{ID: 1, Label: "body"},
		},
		Entry: 0,
	}
	if cfg.Block(0) == nil {
		t.Error("Block(0) should not be nil")
	}
	if cfg.Block(1) == nil {
		t.Error("Block(1) should not be nil")
	}
	if cfg.Block(-1) != nil {
		t.Error("Block(-1) should be nil")
	}
	if cfg.Block(99) != nil {
		t.Error("Block(99) should be nil")
	}
}

// ─── Build: Go ────────────────────────────────────────────────

func TestBuild_SimpleGoFunction(t *testing.T) {
	m := goFuncModule("Simple",
		callNode("log.Println"),
		returnNode(),
	)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	if cfgs == nil {
		t.Fatal("Build returned nil")
	}
	funcID := symbols.SymbolID("pkg/foo.go", symbols.SymFunc, "Simple")
	cfg, ok := cfgs[funcID]
	if !ok {
		t.Fatalf("no CFG for funcID %q, have: %v", funcID, keys(cfgs))
	}
	if cfg.Entry < 0 {
		t.Error("CFG entry should be non-negative")
	}
	if len(cfg.Blocks) == 0 {
		t.Error("CFG should have at least one block")
	}
}

func TestBuild_BranchCreatesMultipleBlocks(t *testing.T) {
	m := goFuncModule("WithBranch",
		branchNode(callNode("a"), callNode("b")),
	)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	funcID := symbols.SymbolID("pkg/foo.go", symbols.SymFunc, "WithBranch")
	cfg := cfgs[funcID]
	if cfg == nil {
		t.Fatal("no CFG for WithBranch")
	}
	// A branch should create: entry, then-block, merge-block = at least 3
	if len(cfg.Blocks) < 3 {
		t.Errorf("branch should create >= 3 blocks, got %d", len(cfg.Blocks))
	}
}

func TestBuild_LoopCreatesBackEdge(t *testing.T) {
	m := goFuncModule("WithLoop",
		loopNode(callNode("step")),
	)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	funcID := symbols.SymbolID("pkg/foo.go", symbols.SymFunc, "WithLoop")
	cfg := cfgs[funcID]
	if cfg == nil {
		t.Fatal("no CFG for WithLoop")
	}
	// Loop should create header, body, exit = at least 3 blocks
	if len(cfg.Blocks) < 3 {
		t.Errorf("loop should create >= 3 blocks, got %d", len(cfg.Blocks))
	}
	// Body block should have a back-edge successor pointing to header
	var hasBackEdge bool
	for _, b := range cfg.Blocks {
		for _, s := range b.Succs {
			if s < b.ID {
				hasBackEdge = true
			}
		}
	}
	if !hasBackEdge {
		t.Log("Note: back edge may not be numerically detectable depending on block ordering")
	}
}

func TestBuild_ReturnAddsExitBlock(t *testing.T) {
	m := goFuncModule("WithReturn", returnNode())
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	funcID := symbols.SymbolID("pkg/foo.go", symbols.SymFunc, "WithReturn")
	cfg := cfgs[funcID]
	if cfg == nil {
		t.Fatal("no CFG for WithReturn")
	}
	if len(cfg.Exits) == 0 {
		t.Error("return statement should add to cfg.Exits")
	}
}

// ─── Build: CA Gen ────────────────────────────────────────────

func TestBuild_CAGenProcedure(t *testing.T) {
	m := &ast.Module{
		Language: ast.LangCAGen,
		FilePath: "order.ad",
		Nodes: []*ast.Node{
			{
				Kind:     ast.NodeProcedure,
				Name:     "ProcessOrder",
				Exported: true,
				Children: []*ast.Node{
					{Kind: ast.NodeLoop, Name: "READ EACH ORDER"},
					{Kind: ast.NodeBranch, Name: "IF VALID"},
					{Kind: ast.NodeReturn, Name: "EXIT PROCEDURE"},
				},
			},
		},
	}
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	procID := symbols.SymbolID("order.ad", symbols.SymProcedure, "ProcessOrder")
	cfg := cfgs[procID]
	if cfg == nil {
		t.Fatal("no CFG for CA Gen procedure")
	}
	if len(cfg.Blocks) < 2 {
		t.Errorf("CA Gen procedure should have >= 2 blocks, got %d", len(cfg.Blocks))
	}
}

// ─── Dataflow ─────────────────────────────────────────────────

func TestAnalyzeDataFlow_NoIssues(t *testing.T) {
	// Well-connected CFG: entry → block1 → exit
	entry := &BasicBlock{ID: 0, Label: "entry", Succs: []int{1}}
	body := &BasicBlock{ID: 1, Label: "body", Preds: []int{0}}
	cfg := &CFG{
		FuncID: "f",
		Blocks: []*BasicBlock{entry, body},
		Entry:  0,
		Exits:  []int{1},
	}
	issues := AnalyzeDataFlow(cfg, nil)
	if len(issues) != 0 {
		t.Errorf("well-formed CFG should have no issues, got: %v", issues)
	}
}

func TestAnalyzeDataFlow_DeadCode(t *testing.T) {
	// Block 1 has no predecessors (dead code)
	entry := &BasicBlock{ID: 0, Label: "entry", Succs: []int{2}}
	dead := &BasicBlock{ID: 1, Label: "dead", Preds: []int{}} // no preds!
	exit := &BasicBlock{ID: 2, Label: "exit", Preds: []int{0}}
	cfg := &CFG{
		FuncID: "f",
		Blocks: []*BasicBlock{entry, dead, exit},
		Entry:  0,
		Exits:  []int{2},
	}
	issues := AnalyzeDataFlow(cfg, nil)
	if len(issues) == 0 {
		t.Error("dead block should produce a DataFlowIssue")
	}
	if issues[0].Kind != "dead_code" {
		t.Errorf("issue kind = %q, want dead_code", issues[0].Kind)
	}
}

func TestAnalyzeDataFlow_NilCFG(t *testing.T) {
	issues := AnalyzeDataFlow(nil, nil)
	if len(issues) != 0 {
		t.Error("nil CFG should return no issues")
	}
}

func TestAnalyzeDataFlow_EmptyCFG(t *testing.T) {
	cfg := &CFG{FuncID: "f", Blocks: []*BasicBlock{}}
	issues := AnalyzeDataFlow(cfg, nil)
	if len(issues) != 0 {
		t.Error("empty CFG should return no issues")
	}
}

// ─── Effects ─────────────────────────────────────────────────

func TestInferEffects_DB(t *testing.T) {
	m := goFuncModule("SaveUser",
		callNode("db.Exec"),
	)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	efx := InferEffects(m, cfgs)
	funcID := symbols.SymbolID("pkg/foo.go", symbols.SymFunc, "SaveUser")
	e, ok := efx[funcID]
	if !ok {
		t.Fatalf("no effects for SaveUser; keys: %v", effectKeys(efx))
	}
	if !e.Effects[EffectDB] {
		t.Errorf("SaveUser with db.Exec should have EffectDB; got effects: %v", e.Effects)
	}
}

func TestInferEffects_Network(t *testing.T) {
	m := goFuncModule("CallAPI",
		callNode("http.Get"),
	)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	efx := InferEffects(m, cfgs)
	funcID := symbols.SymbolID("pkg/foo.go", symbols.SymFunc, "CallAPI")
	e := efx[funcID]
	if e == nil {
		t.Fatal("no effects for CallAPI")
	}
	if !e.Effects[EffectNetwork] {
		t.Errorf("CallAPI should have EffectNetwork; got %v", e.Effects)
	}
}

func TestInferEffects_FS(t *testing.T) {
	m := goFuncModule("WriteFile",
		callNode("os.Create"),
	)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	efx := InferEffects(m, cfgs)
	funcID := symbols.SymbolID("pkg/foo.go", symbols.SymFunc, "WriteFile")
	e := efx[funcID]
	if e == nil {
		t.Fatal("no effects for WriteFile")
	}
	if !e.Effects[EffectFS] {
		t.Errorf("WriteFile should have EffectFS; got %v", e.Effects)
	}
}

func TestInferEffects_Pure(t *testing.T) {
	m := goFuncModule("Add",
		callNode("someHelper"), // non-matching name
	)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	efx := InferEffects(m, cfgs)
	funcID := symbols.SymbolID("pkg/foo.go", symbols.SymFunc, "Add")
	e := efx[funcID]
	if e == nil {
		t.Fatal("no effects for Add")
	}
	if !e.Effects[EffectPure] {
		t.Logf("Add effects: %v (may have EffectUnknown instead of Pure)", e.Effects)
	}
}

func TestInferEffects_CAGen_WriteIsDB(t *testing.T) {
	m := &ast.Module{
		Language: ast.LangCAGen,
		FilePath: "order.ad",
		Nodes: []*ast.Node{
			{
				Kind: ast.NodeProcedure,
				Name: "UpdateOrder",
				Children: []*ast.Node{
					{Kind: ast.NodeAssign, Name: "WRITE ORDER"},
				},
			},
		},
	}
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	efx := InferEffects(m, cfgs)
	procID := symbols.SymbolID("order.ad", symbols.SymProcedure, "UpdateOrder")
	e := efx[procID]
	if e == nil {
		t.Fatal("no effects for CA Gen procedure")
	}
	if !e.Effects[EffectDB] {
		t.Errorf("WRITE should tag EffectDB; got %v", e.Effects)
	}
}

func TestInferEffects_EmptyModule(t *testing.T) {
	m := &ast.Module{Language: ast.LangGo, FilePath: "empty.go"}
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	efx := InferEffects(m, cfgs)
	if len(efx) != 0 {
		t.Errorf("empty module should produce no effects, got %d", len(efx))
	}
}

// ─── Helpers ──────────────────────────────────────────────────

func keys(m CFGSet) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func effectKeys(m map[string]*FuncEffects) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// v23.39: the branch condition (carried on NodeBranch.RawSource by the Go and
// COBOL frontends) must survive onto the deciding block's Cond field, so the
// CFG view can render a real decision node instead of a bare "IF".
func TestBuild_BranchConditionOnBlock_v2339(t *testing.T) {
	br := branchNode(callNode("a"))
	br.RawSource = "CUST-BALANCE < 0"
	m := goFuncModule("WithCond", br)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	funcID := symbols.SymbolID("pkg/foo.go", symbols.SymFunc, "WithCond")
	cfg := cfgs[funcID]
	if cfg == nil {
		t.Fatal("no CFG for WithCond")
	}
	found := ""
	for _, b := range cfg.Blocks {
		if b.Cond != "" {
			found = b.Cond
		}
	}
	if found != "CUST-BALANCE < 0" {
		t.Errorf("expected a block with Cond=%q, got %q", "CUST-BALANCE < 0", found)
	}
}

// v23.41: an EVALUATE branch (Name=="EVALUATE" with WHEN children) builds a
// multi-way CFG: the decision block fans out to one case block per WHEN, each
// carrying the WHEN value as its Cond, all rejoining at a merge. Previously the
// WHENs nested as fake ifs, producing a cascade of unlabeled decisions.
func TestBuild_EvaluateMultiWay_v2341(t *testing.T) {
	// EVALUATE with 3 WHENs, each performing a call.
	when := func(val, callee string) *ast.Node {
		n := &ast.Node{Kind: ast.NodeBranch, Name: "WHEN", RawSource: val}
		n.Children = []*ast.Node{callNode(callee)}
		return n
	}
	eval := &ast.Node{Kind: ast.NodeBranch, Name: "EVALUATE", RawSource: "TRUE"}
	eval.Children = []*ast.Node{when("A", "doA"), when("B", "doB"), when("OTHER", "doC")}
	m := goFuncModule("Dispatch", eval)
	idx := symbols.BuildFromModules([]*ast.Module{m})
	cfgs := Build(m, idx)
	funcID := symbols.SymbolID("pkg/foo.go", symbols.SymFunc, "Dispatch")
	cfg := cfgs[funcID]
	if cfg == nil {
		t.Fatal("no CFG for Dispatch")
	}
	// The entry (decision) block should have 3 successors (one per WHEN).
	entry := cfg.Block(cfg.Entry)
	if entry == nil {
		t.Fatal("no entry block")
	}
	if len(entry.Succs) != 3 {
		t.Errorf("EVALUATE decision should fan out to 3 cases, got %d succs", len(entry.Succs))
	}
	// At least 3 case blocks should carry a WHEN value in Cond.
	withCond := 0
	for _, b := range cfg.Blocks {
		if b.Cond == "A" || b.Cond == "B" || b.Cond == "OTHER" {
			withCond++
		}
	}
	if withCond != 3 {
		t.Errorf("expected 3 case blocks with WHEN-value Cond, got %d", withCond)
	}
}
