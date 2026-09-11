// objgraph/objgraph_test.go — tests for the ObjectGraph format
package objgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func TestBuild_NilIndex(t *testing.T) {
	g := Build("", nil, nil, DefaultBuildOptions)
	if g == nil {
		t.Fatal("Build(nil index) must return non-nil graph")
	}
	if len(g.Nodes) != 0 {
		t.Errorf("nil index should produce 0 nodes, got %d", len(g.Nodes))
	}
}

func TestBuild_EmptyIndex(t *testing.T) {
	idx := symbols.NewIndex()
	g := Build("/project", idx, nil, DefaultBuildOptions)
	if g == nil {
		t.Fatal("Build returned nil")
	}
	if g.ProjectRoot != "/project" {
		t.Errorf("ProjectRoot = %q, want /project", g.ProjectRoot)
	}
}

func TestBuild_WithRealModules(t *testing.T) {
	fe := ast.ForLanguage(ast.LangGo)
	modA, err := fe.Parse("pkg/handler.go", []byte(
		"package handler\n"+
			"func CreateUser(name string) error { return nil }\n"+
			"func DeleteUser(id int) error { return nil }\n",
	))
	if err != nil || modA == nil {
		t.Fatalf("parse failed: %v", err)
	}
	modB, err := fe.Parse("pkg/model.go", []byte(
		"package model\ntype User struct { ID int; Name string }\n",
	))
	if err != nil || modB == nil {
		t.Fatalf("parse model failed: %v", err)
	}
	idx := symbols.NewIndex()
	idx.AddModule(modA)
	idx.AddModule(modB)

	g := Build("/project", idx, nil, DefaultBuildOptions)
	if len(g.Nodes) == 0 {
		t.Error("graph should have nodes after indexing 2 modules")
	}
	found := false
	for _, n := range g.Nodes {
		if n.Name == "CreateUser" {
			found = true
			if n.Kind != "func" {
				t.Errorf("CreateUser.Kind = %q, want func", n.Kind)
			}
		}
	}
	if !found {
		t.Error("CreateUser not found in graph nodes")
	}
}

func TestBuild_NIT_Attached(t *testing.T) {
	fe := ast.ForLanguage(ast.LangGo)
	mod, err := fe.Parse("pkg/svc.go", []byte("package svc\nfunc Hello() string { return \"hi\" }\n"))
	if err != nil || mod == nil {
		t.Skip("parse returned nil or err")
	}
	idx := symbols.NewIndex()
	idx.AddModule(mod)

	opts := DefaultBuildOptions
	opts.IncludeNIT = true
	g := Build("/project", idx, nil, opts)

	for _, n := range g.Nodes {
		if n.Name == "Hello" && n.NIT == "" {
			t.Error("NIT should be populated when IncludeNIT=true")
		}
	}
}

func TestBuild_ExportedOnly(t *testing.T) {
	fe := ast.ForLanguage(ast.LangGo)
	mod, err := fe.Parse("pkg/svc.go", []byte("package svc\nfunc Public() {}\nfunc private() {}\n"))
	if err != nil || mod == nil {
		t.Skip("parse returned nil or err")
	}
	idx := symbols.NewIndex()
	idx.AddModule(mod)

	opts := DefaultBuildOptions
	opts.ExportedOnly = true
	g := Build("/project", idx, nil, opts)

	for _, n := range g.Nodes {
		if n.Name == "private" {
			t.Error("ExportedOnly should exclude unexported symbols")
		}
	}
}

func TestBuild_WithEffects(t *testing.T) {
	fe := ast.ForLanguage(ast.LangGo)
	mod, err := fe.Parse("pkg/repo.go", []byte("package repo\nfunc Save() { db.Exec(\"\") }\n"))
	if err != nil || mod == nil {
		t.Skip("parse returned nil or err")
	}
	idx := symbols.NewIndex()
	idx.AddModule(mod)
	cfgs := flow.Build(mod, idx)
	efx := flow.InferEffects(mod, cfgs)

	g := Build("/project", idx, efx, DefaultBuildOptions)
	for _, n := range g.Nodes {
		if n.Name == "Save" && len(n.Effects) == 0 {
			t.Error("Save should have inferred effects (db.Exec → EffectDB)")
		}
	}
}

func TestRender_Nil(t *testing.T) {
	if Render(nil) != "" {
		t.Error("Render(nil) should return empty string")
	}
}

func TestRender_Empty(t *testing.T) {
	g := &ObjectGraph{ProjectRoot: "/p", LayerIndex: make(map[string][]string)}
	out := Render(g)
	if !strings.Contains(out, "GRAPH") {
		t.Errorf("Render should contain GRAPH header; got: %q", out)
	}
}

func TestRender_HasLayerAndNode(t *testing.T) {
	g := &ObjectGraph{
		ProjectRoot: "/project",
		Nodes: []ObjectNode{
			{ID: "a.go#func:Foo", Name: "Foo", Kind: "func", NIT: "FN Foo()", Layer: "handler"},
		},
		LayerIndex: map[string][]string{"handler": {"a.go#func:Foo"}},
	}
	out := Render(g)
	if !strings.Contains(out, "handler") {
		t.Errorf("render should include layer name; got: %q", out)
	}
	if !strings.Contains(out, "Foo") {
		t.Errorf("render should include node name; got: %q", out)
	}
}

func TestRender_CallEdgesSection(t *testing.T) {
	g := &ObjectGraph{
		ProjectRoot: "/project",
		Nodes: []ObjectNode{
			{ID: "a.go#func:A", Name: "A", Kind: "func"},
			{ID: "b.go#func:B", Name: "B", Kind: "func"},
		},
		Edges:      []ObjectEdge{{From: "a.go#func:A", To: "b.go#func:B", Kind: EdgeCalls}},
		LayerIndex: make(map[string][]string),
	}
	out := Render(g)
	if !strings.Contains(out, "CALLS") {
		t.Errorf("render should include CALLS section: %q", out)
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "objgraph.json")

	g := &ObjectGraph{
		ProjectRoot: "/project",
		Nodes:       []ObjectNode{{ID: "pkg/foo.go#func:Bar", Name: "Bar", Kind: "func", Layer: "service"}},
		Edges:       []ObjectEdge{{From: "pkg/foo.go#func:Bar", To: "pkg/db.go#func:Exec", Kind: EdgeCalls}},
		LayerIndex:  map[string][]string{"service": {"pkg/foo.go#func:Bar"}},
	}

	if err := g.Save(path); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not written: %v", err)
	}

	g2, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if g2.ProjectRoot != "/project" {
		t.Errorf("ProjectRoot = %q, want /project", g2.ProjectRoot)
	}
	if len(g2.Nodes) != 1 || g2.Nodes[0].Name != "Bar" {
		t.Errorf("nodes not preserved: %+v", g2.Nodes)
	}
	if len(g2.Edges) != 1 {
		t.Errorf("edges = %d, want 1", len(g2.Edges))
	}
}

func TestLoad_NonExistent(t *testing.T) {
	g, err := Load("/nonexistent/path/objgraph.json")
	if err != nil {
		t.Fatalf("Load(nonexistent) should return empty graph, not error: %v", err)
	}
	if g == nil {
		t.Fatal("Load(nonexistent) should return non-nil graph")
	}
}

func TestNodesInLayer(t *testing.T) {
	g := &ObjectGraph{
		Nodes: []ObjectNode{
			{ID: "a", Name: "A", Layer: "handler"},
			{ID: "b", Name: "B", Layer: "service"},
			{ID: "c", Name: "C", Layer: "handler"},
		},
		LayerIndex: map[string][]string{"handler": {"a", "c"}, "service": {"b"}},
	}
	if len(g.NodesInLayer("handler")) != 2 {
		t.Errorf("NodesInLayer(handler) = %d, want 2", len(g.NodesInLayer("handler")))
	}
}

func TestCalleesOf(t *testing.T) {
	g := &ObjectGraph{Edges: []ObjectEdge{
		{From: "a", To: "b", Kind: EdgeCalls},
		{From: "a", To: "c", Kind: EdgeCalls},
		{From: "x", To: "b", Kind: EdgeCalls},
	}}
	if len(g.CalleesOf("a")) != 2 {
		t.Errorf("CalleesOf(a) = %d, want 2", len(g.CalleesOf("a")))
	}
}

func TestCallersOf(t *testing.T) {
	g := &ObjectGraph{Edges: []ObjectEdge{
		{From: "a", To: "b", Kind: EdgeCalls},
		{From: "c", To: "b", Kind: EdgeCalls},
	}}
	if len(g.CallersOf("b")) != 2 {
		t.Errorf("CallersOf(b) = %d, want 2", len(g.CallersOf("b")))
	}
}

func TestNodeByID(t *testing.T) {
	g := &ObjectGraph{Nodes: []ObjectNode{{ID: "a", Name: "Alpha"}}}
	n := g.NodeByID("a")
	if n == nil {
		t.Fatal("NodeByID(a) should not be nil")
	}
	if n.Name != "Alpha" {
		t.Errorf("Name = %q, want Alpha", n.Name)
	}
	if g.NodeByID("missing") != nil {
		t.Error("NodeByID(missing) should return nil")
	}
}

func TestSkillContext_Nil(t *testing.T) {
	if SkillContext(nil, "handler") != "" {
		t.Error("SkillContext(nil) should return empty string")
	}
}

func TestSkillContext_EmptyLayer(t *testing.T) {
	g := &ObjectGraph{LayerIndex: make(map[string][]string)}
	if SkillContext(g, "handler") != "" {
		t.Error("SkillContext for empty layer should return empty string")
	}
}

func TestSkillContext_HasContent(t *testing.T) {
	g := &ObjectGraph{
		Nodes: []ObjectNode{
			{ID: "a", Name: "CreateUser", NIT: "FN CreateUser(name:string) error",
				Layer: "handler", Effects: []string{"db"}},
		},
		LayerIndex: map[string][]string{"handler": {"a"}},
	}
	out := SkillContext(g, "handler")
	if !strings.Contains(out, "handler") {
		t.Errorf("SkillContext should mention layer: %q", out)
	}
	if !strings.Contains(out, "CreateUser") {
		t.Errorf("SkillContext should contain symbol: %q", out)
	}
	if !strings.Contains(out, "db") {
		t.Errorf("SkillContext should contain effect: %q", out)
	}
}

func TestTokenEstimate(t *testing.T) {
	if TokenEstimate("hello world") <= 0 {
		t.Error("TokenEstimate should return positive for non-empty string")
	}
	if TokenEstimate("") != 0 {
		t.Error("TokenEstimate of empty string should be 0")
	}
}

func TestEffectsForSymbol_Nil(t *testing.T) {
	if effectsForSymbol("id", nil) != nil {
		t.Error("nil effects map should return nil")
	}
}

func TestEffectsForSymbol_Found(t *testing.T) {
	efx := map[string]*flow.FuncEffects{
		"id": {SymbolID: "id", Effects: map[flow.EffectKind]bool{flow.EffectDB: true}},
	}
	result := effectsForSymbol("id", efx)
	if len(result) == 0 {
		t.Error("should return effects when present")
	}
	found := false
	for _, r := range result {
		if r == string(flow.EffectDB) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected db in effects, got %v", result)
	}
}

func TestDefaultPath(t *testing.T) {
	if !strings.HasSuffix(DefaultPath, ".json") {
		t.Errorf("DefaultPath should end in .json, got %q", DefaultPath)
	}
}

// Keep ast import alive.
func TestASTImportLive(t *testing.T) {
	if ast.LangGo == "" {
		t.Error("ast.LangGo should not be empty")
	}
}
