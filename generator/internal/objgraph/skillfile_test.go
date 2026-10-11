// objgraph/skillfile_test.go (TASK 4, v19): SkillFile round-trip tests.
package objgraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// ─── helpers ──────────────────────────────────────────────────

// buildSmallGraph creates a minimal ObjectGraph for testing.
func buildSmallGraph(root string) *ObjectGraph {
	idx := symbols.NewIndex()

	mA := &ast.Module{
		Language: ast.LangGo,
		FilePath: filepath.Join(root, "handler.go"),
		Nodes: []*ast.Node{
			{ID: filepath.Join(root, "handler.go") + "#func:HandleRequest",
				Kind: ast.NodeFunction, Name: "HandleRequest", Exported: true,
				Span: ast.Span{Start: ast.Pos{Line: 10}}},
			{ID: filepath.Join(root, "handler.go") + "#func:validate",
				Kind: ast.NodeFunction, Name: "validate",
				Span: ast.Span{Start: ast.Pos{Line: 30}}},
		},
	}
	mB := &ast.Module{
		Language: ast.LangGo,
		FilePath: filepath.Join(root, "service.go"),
		Nodes: []*ast.Node{
			{ID: filepath.Join(root, "service.go") + "#func:CreateUser",
				Kind: ast.NodeFunction, Name: "CreateUser", Exported: true,
				Span: ast.Span{Start: ast.Pos{Line: 5}}},
		},
	}
	idx.AddModule(mA)
	idx.AddModule(mB)

	return Build(root, idx, nil, BuildOptions{IncludeNIT: true})
}

// ─── WriteSkillFile / ReadSkillFile ───────────────────────────

func TestSkillFile_WriteRead_HeaderFields(t *testing.T) {
	dir := t.TempDir()
	g := buildSmallGraph(dir)
	path := filepath.Join(dir, "test.skill")

	constraints := []string{"100% offline", "no new external deps"}
	if err := WriteSkillFile(path, g, constraints, nil); err != nil {
		t.Fatalf("WriteSkillFile: %v", err)
	}

	sf, err := ReadSkillFile(path)
	if err != nil {
		t.Fatalf("ReadSkillFile: %v", err)
	}

	if sf.Header.SkillVersion != skillVersion {
		t.Errorf("version: want %d, got %d", skillVersion, sf.Header.SkillVersion)
	}
	if sf.Header.ProjectRoot != dir {
		t.Errorf("project_root: want %s, got %s", dir, sf.Header.ProjectRoot)
	}
	if sf.Header.WrittenAt == "" {
		t.Error("written_at should be set")
	}
	if len(sf.Header.Constraints) != 2 {
		t.Errorf("want 2 constraints in header, got %d", len(sf.Header.Constraints))
	}
}

func TestSkillFile_WriteRead_NodeCount(t *testing.T) {
	dir := t.TempDir()
	g := buildSmallGraph(dir)
	path := filepath.Join(dir, "test.skill")

	if err := WriteSkillFile(path, g, nil, nil); err != nil {
		t.Fatalf("WriteSkillFile: %v", err)
	}
	sf, err := ReadSkillFile(path)
	if err != nil {
		t.Fatalf("ReadSkillFile: %v", err)
	}

	if len(sf.Nodes) != len(g.Nodes) {
		t.Errorf("node count: want %d, got %d", len(g.Nodes), len(sf.Nodes))
	}
}

func TestSkillFile_WriteRead_NodeFields(t *testing.T) {
	dir := t.TempDir()
	g := buildSmallGraph(dir)
	path := filepath.Join(dir, "test.skill")

	notes := map[string]string{}
	for _, n := range g.Nodes {
		if n.Name == "HandleRequest" {
			notes[n.ID] = "entry point for REST API"
		}
	}
	if err := WriteSkillFile(path, g, nil, notes); err != nil {
		t.Fatalf("WriteSkillFile: %v", err)
	}
	sf, err := ReadSkillFile(path)
	if err != nil {
		t.Fatalf("ReadSkillFile: %v", err)
	}

	var found bool
	for _, r := range sf.Nodes {
		if r.Name == "HandleRequest" {
			found = true
			if r.Note != "entry point for REST API" {
				t.Errorf("note: want 'entry point for REST API', got %q", r.Note)
			}
			if r.SymKind == "" {
				t.Error("sym_kind should be set")
			}
			if r.Language == "" {
				t.Error("lang should be set")
			}
		}
	}
	if !found {
		t.Error("HandleRequest not found in skill file nodes")
	}
}

func TestSkillFile_WriteRead_ConstraintRecords(t *testing.T) {
	dir := t.TempDir()
	g := buildSmallGraph(dir)
	path := filepath.Join(dir, "test.skill")

	constraints := []string{
		"100% offline, no CDN",
		"all code must compile clean",
	}
	if err := WriteSkillFile(path, g, constraints, nil); err != nil {
		t.Fatalf("WriteSkillFile: %v", err)
	}
	sf, err := ReadSkillFile(path)
	if err != nil {
		t.Fatalf("ReadSkillFile: %v", err)
	}

	if len(sf.Constraints) != 2 {
		t.Errorf("want 2 constraint records, got %d", len(sf.Constraints))
	}
	for _, c := range sf.Constraints {
		if c.Category == "" {
			t.Errorf("constraint category should be set; got empty for %q", c.Text)
		}
	}
}

// ─── ToObjectGraph round-trip ─────────────────────────────────

func TestSkillFile_ToObjectGraph_PreservesNodeCount(t *testing.T) {
	dir := t.TempDir()
	g := buildSmallGraph(dir)
	path := filepath.Join(dir, "rt.skill")

	if err := WriteSkillFile(path, g, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf, err := ReadSkillFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	g2 := sf.ToObjectGraph()

	if len(g2.Nodes) != len(g.Nodes) {
		t.Errorf("round-trip node count: want %d, got %d", len(g.Nodes), len(g2.Nodes))
	}
	if g2.ProjectRoot != g.ProjectRoot {
		t.Errorf("round-trip project_root: want %s, got %s", g.ProjectRoot, g2.ProjectRoot)
	}
}

// ─── IndexFromSkillFile round-trip ────────────────────────────

func TestSkillFile_IndexFromSkillFile_SymbolsResolvable(t *testing.T) {
	dir := t.TempDir()
	g := buildSmallGraph(dir)
	path := filepath.Join(dir, "idx.skill")

	if err := WriteSkillFile(path, g, nil, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf, err := ReadSkillFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	idx := IndexFromSkillFile(sf)
	syms := idx.AllSymbols()
	if len(syms) == 0 {
		t.Fatal("IndexFromSkillFile returned empty symbol set")
	}

	// Every node in the graph should be findable by name.
	for _, n := range g.Nodes {
		found := idx.ByName(n.Name)
		if len(found) == 0 {
			t.Errorf("symbol %q not found in index after round-trip", n.Name)
		}
	}
}

// ─── Error handling ───────────────────────────────────────────

func TestSkillFile_ReadMissingFile_Errors(t *testing.T) {
	_, err := ReadSkillFile("/nonexistent/path.skill")
	if err == nil {
		t.Error("reading a missing file should return an error")
	}
}

func TestSkillFile_ReadWrongVersion_Errors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.skill")
	// Write a header with wrong version.
	content := `{"skill_version":99,"project_root":"/tmp","written_at":"2025-01-01T00:00:00Z","node_count":0,"edge_count":0}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadSkillFile(path)
	if err == nil {
		t.Error("wrong skill_version should return an error")
	}
}

func TestSkillFile_ReadEmptyFile_Errors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.skill")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	sf, err := ReadSkillFile(path)
	// Empty file: no header was read, so SkillVersion will be 0.
	// The function should either error or return a zero header.
	if err == nil && sf.Header.SkillVersion != 0 {
		t.Error("empty file should produce zero header or error")
	}
}

// ─── EffectsFromSkillFile ─────────────────────────────────────

func TestSkillFile_EffectsFromSkillFile(t *testing.T) {
	sf := &SkillFile{
		Nodes: []SkillRecord{
			{Kind: "node", ID: "file.go#func:FetchUser", Name: "FetchUser",
				Effects: []string{"db", "network"}},
			{Kind: "node", ID: "file.go#func:ComputeHash", Name: "ComputeHash",
				Effects: nil},
		},
	}
	efx := EffectsFromSkillFile(sf)
	if len(efx) != 1 {
		t.Errorf("want 1 effects entry, got %d", len(efx))
	}
	fe, ok := efx["file.go#func:FetchUser"]
	if !ok {
		t.Fatal("FetchUser not in effects map")
	}
	if !fe.Effects["db"] {
		t.Error("FetchUser should have db effect")
	}
	if !fe.Effects["network"] {
		t.Error("FetchUser should have network effect")
	}
}
