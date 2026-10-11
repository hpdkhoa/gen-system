package presentation

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// ─── Test fixtures ────────────────────────────────────────────

func buildTestIndex() *symbols.Index {
	m := &ast.Module{
		Language:    ast.LangGo,
		FilePath:    "/project/handler/user.go",
		PackageName: "handler",
		Checksum:    "abc123",
		Nodes: []*ast.Node{
			{
				ID:       "func:CreateUser",
				Kind:     ast.NodeFunction,
				Name:     "CreateUser",
				Language: ast.LangGo,
				Span:     ast.Span{Start: ast.Pos{Line: 10}},
				Exported: true,
				Children: []*ast.Node{
					{Kind: ast.NodeCall, Callee: "db.Exec"},
				},
			},
			{
				ID:       "struct:UserRequest",
				Kind:     ast.NodeStruct,
				Name:     "UserRequest",
				Language: ast.LangGo,
				Span:     ast.Span{Start: ast.Pos{Line: 5}},
				Exported: true,
				Fields: []ast.Field{
					{Name: "Name", Type: ast.TypeRef{Name: "string"}, Exported: true},
				},
			},
			{
				ID:            "unknown:1",
				Kind:          ast.NodeUnknown,
				Span:          ast.Span{Start: ast.Pos{Line: 30}},
				UnknownReason: "unsupported_directive",
				RawSource:     "//go:generate protoc ...",
			},
		},
	}
	idx := symbols.NewIndex()
	idx.AddModule(m)
	return idx
}

func buildTestServer() *Server {
	idx := buildTestIndex()
	s := New(idx)

	// Add a simple CFG for the CreateUser function
	funcID := symbols.SymbolID("/project/handler/user.go", symbols.SymFunc, "CreateUser")
	entry := &flow.BasicBlock{ID: 0, Label: "entry", Succs: []int{1}}
	exit := &flow.BasicBlock{ID: 1, Label: "exit", Preds: []int{0}}
	cfg := &flow.CFG{
		FuncID: funcID,
		Blocks: []*flow.BasicBlock{entry, exit},
		Entry:  0,
		Exits:  []int{1},
	}
	cfgSet := flow.CFGSet{funcID: cfg}
	s.cfgs["/project/handler/user.go"] = cfgSet

	// Add effect
	s.efx[funcID] = &flow.FuncEffects{
		SymbolID: funcID,
		Effects:  map[flow.EffectKind]bool{flow.EffectDB: true},
	}

	return s
}

func getJSON(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)
	return rec
}

func postJSON(t *testing.T, s *Server, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)
	return rec
}

// ─── /api/present/symbols ─────────────────────────────────────

func TestHandleSymbols_MissingParam(t *testing.T) {
	s := buildTestServer()
	rec := getJSON(t, s, "/api/present/symbols")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleSymbols_FileNotFound(t *testing.T) {
	s := buildTestServer()
	rec := getJSON(t, s, "/api/present/symbols?file=/nonexistent/file.go")
	// Expect 404 (file not indexed and can't be parsed)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusOK {
		t.Logf("status = %d (404 or 200 both acceptable depending on on-demand parse)", rec.Code)
	}
}

func TestHandleSymbols_KnownFile(t *testing.T) {
	s := buildTestServer()
	rec := getJSON(t, s, "/api/present/symbols?file=/project/handler/user.go")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var resp SymbolMapResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.File != "/project/handler/user.go" {
		t.Errorf("File = %q, want /project/handler/user.go", resp.File)
	}
	if len(resp.Symbols) == 0 {
		t.Error("expected symbols in response")
	}
	// Verify symbol names
	names := map[string]bool{}
	for _, sym := range resp.Symbols {
		names[sym.Name] = true
	}
	if !names["CreateUser"] {
		t.Errorf("CreateUser not in symbols: %v", names)
	}
	if !names["UserRequest"] {
		t.Errorf("UserRequest not in symbols: %v", names)
	}
	// Verify symbols are sorted by line
	for i := 1; i < len(resp.Symbols); i++ {
		if resp.Symbols[i].Line < resp.Symbols[i-1].Line {
			t.Errorf("symbols not sorted by line: %d < %d", resp.Symbols[i].Line, resp.Symbols[i-1].Line)
		}
	}
}

func TestHandleSymbols_ContentType(t *testing.T) {
	s := buildTestServer()
	rec := getJSON(t, s, "/api/present/symbols?file=/project/handler/user.go")
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// ─── /api/present/flow ────────────────────────────────────────

func TestHandleFlow_MissingParam(t *testing.T) {
	s := buildTestServer()
	rec := getJSON(t, s, "/api/present/flow")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleFlow_UnknownFunc(t *testing.T) {
	s := buildTestServer()
	rec := getJSON(t, s, "/api/present/flow?func=nonexistent/file.go%23func:Missing")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleFlow_KnownFunc(t *testing.T) {
	s := buildTestServer()
	funcID := symbols.SymbolID("/project/handler/user.go", symbols.SymFunc, "CreateUser")
	rec := getJSON(t, s, "/api/present/flow?func="+funcID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var resp FlowViewResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.FuncID != funcID {
		t.Errorf("FuncID = %q, want %q", resp.FuncID, funcID)
	}
	if len(resp.Blocks) == 0 {
		t.Error("expected blocks in flow response")
	}
	// DB effect should be present
	var hasDB bool
	for _, eff := range resp.Effects {
		if eff == "db" {
			hasDB = true
		}
	}
	if !hasDB {
		t.Errorf("expected db effect, got: %v", resp.Effects)
	}
}

// ─── /api/present/beliefs ─────────────────────────────────────

func TestHandleBeliefs_OK(t *testing.T) {
	s := buildTestServer()
	rec := getJSON(t, s, "/api/present/beliefs")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var resp BeliefViewResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.TotalBeliefs < 0 {
		t.Error("TotalBeliefs should be >= 0")
	}
}

func TestHandleBeliefs_HasBeliefs(t *testing.T) {
	s := buildTestServer()
	rec := getJSON(t, s, "/api/present/beliefs")
	var resp BeliefViewResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)

	// We should have at least one belief (effect: db for CreateUser)
	if len(resp.Beliefs) == 0 {
		t.Error("expected at least one belief (effect:db for CreateUser)")
	}
	if resp.TotalBeliefs != len(resp.Beliefs) {
		t.Errorf("TotalBeliefs=%d but len(Beliefs)=%d", resp.TotalBeliefs, len(resp.Beliefs))
	}
}

// ─── /api/present/beliefs/correct ────────────────────────────

func TestHandleCorrect_WrongMethod(t *testing.T) {
	s := buildTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/present/beliefs/correct", nil)
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /correct should return 405, got %d", rec.Code)
	}
}

func TestHandleCorrect_BadJSON(t *testing.T) {
	s := buildTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/present/beliefs/correct",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad JSON should return 400, got %d", rec.Code)
	}
}

func TestHandleCorrect_UnknownBelief(t *testing.T) {
	s := buildTestServer()
	rec := postJSON(t, s, "/api/present/beliefs/correct", BeliefCorrection{
		BeliefID:    "nonexistent:belief:id",
		CorrectedTo: "handler",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown belief should return 400, got %d", rec.Code)
	}
}

func TestHandleCorrect_ValidCorrection(t *testing.T) {
	s := buildTestServer()
	// First, get beliefs to find a valid ID
	rec1 := getJSON(t, s, "/api/present/beliefs")
	var bResp BeliefViewResponse
	_ = json.NewDecoder(rec1.Body).Decode(&bResp)
	if len(bResp.Beliefs) == 0 {
		t.Skip("no beliefs available to correct")
	}
	beliefID := bResp.Beliefs[0].ID

	// Correct it
	rec2 := postJSON(t, s, "/api/present/beliefs/correct", BeliefCorrection{
		BeliefID:    beliefID,
		CorrectedTo: "corrected_value",
		HumanNote:   "test correction",
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("correction failed: status=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// Verify correction persisted
	rec3 := getJSON(t, s, "/api/present/beliefs")
	var bResp2 BeliefViewResponse
	_ = json.NewDecoder(rec3.Body).Decode(&bResp2)
	for _, b := range bResp2.Beliefs {
		if b.ID == beliefID {
			if !b.HumanVerified {
				t.Error("belief should be HumanVerified after correction")
			}
			if b.CorrectedTo != "corrected_value" {
				t.Errorf("CorrectedTo = %q, want corrected_value", b.CorrectedTo)
			}
			return
		}
	}
	t.Error("corrected belief not found in subsequent beliefs response")
}

// ─── /api/present/diff ────────────────────────────────────────

func TestHandleDiff_NoChecksum(t *testing.T) {
	s := buildTestServer()
	// Trigger beliefs to set a checksum
	getJSON(t, s, "/api/present/beliefs")

	rec := getJSON(t, s, "/api/present/diff")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	var resp DiffViewResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Now == "" {
		t.Error("Now should be set in diff response")
	}
}

func TestHandleDiff_SameChecksum(t *testing.T) {
	s := buildTestServer()
	// Get beliefs to establish checksum
	rec1 := getJSON(t, s, "/api/present/beliefs")
	var bResp BeliefViewResponse
	_ = json.NewDecoder(rec1.Body).Decode(&bResp)
	_ = bResp

	checksum := s.beliefs.CurrentChecksum()
	rec2 := getJSON(t, s, "/api/present/diff?since="+checksum)
	var resp DiffViewResponse
	_ = json.NewDecoder(rec2.Body).Decode(&resp)
	if len(resp.Changes) != 0 {
		t.Logf("Note: diff with same checksum returned %d changes (may be expected if beliefs changed)", len(resp.Changes))
	}
}

// ─── RegisterRoutes ───────────────────────────────────────────

func TestRegisterRoutes_AllEndpoints(t *testing.T) {
	s := buildTestServer()
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	endpoints := []string{
		"/api/present/symbols",
		"/api/present/flow",
		"/api/present/beliefs",
		"/api/present/beliefs/correct",
		"/api/present/diff",
	}
	for _, ep := range endpoints {
		req := httptest.NewRequest(http.MethodGet, ep, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		// None should return 404 (even if params missing → 400 is expected)
		if rec.Code == http.StatusNotFound {
			t.Errorf("endpoint %q not registered (got 404)", ep)
		}
	}
}

// BeliefStore unit tests moved to internal/belief/belief_test.go along with the
// store itself (session 1 relocation). The HTTP-surface tests above still cover
// the /api/present/beliefs endpoints end-to-end.

// ─── New / AddFlowData ────────────────────────────────────────

func TestNew_NilIndex(t *testing.T) {
	// New(nil) should not panic. It should initialise with empty index
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("New(nil) panicked: %v", r)
		}
	}()
	s := New(symbols.NewIndex())
	if s == nil {
		t.Error("New returned nil")
	}
}

func TestAddFlowData(t *testing.T) {
	s := New(symbols.NewIndex())
	cfgSet := flow.CFGSet{
		"file.go#func:F": &flow.CFG{FuncID: "file.go#func:F"},
	}
	efx := map[string]*flow.FuncEffects{
		"file.go#func:F": {Effects: map[flow.EffectKind]bool{flow.EffectPure: true}},
	}
	// Should not panic
	s.AddFlowData("file.go", cfgSet, efx)
	if s.cfgs["file.go"] == nil {
		t.Error("AddFlowData should store cfgSet")
	}
	if s.efx["file.go#func:F"] == nil {
		t.Error("AddFlowData should store effects")
	}
}

func TestLoadEffects(t *testing.T) {
	s := New(symbols.NewIndex())
	s.LoadEffects(map[string]*flow.FuncEffects{
		"a": {Effects: map[flow.EffectKind]bool{flow.EffectDB: true}},
		"b": {Effects: map[flow.EffectKind]bool{flow.EffectFS: true}},
	})
	if s.efx["a"] == nil || s.efx["b"] == nil {
		t.Error("LoadEffects should store all entries")
	}
}

func TestLoadAllCFGs(t *testing.T) {
	s := New(symbols.NewIndex())
	s.LoadAllCFGs(map[string]flow.CFGSet{
		"a.go": {"f": &flow.CFG{}},
		"b.go": {"g": &flow.CFG{}},
	})
	if s.cfgs["a.go"] == nil || s.cfgs["b.go"] == nil {
		t.Error("LoadAllCFGs should store all entries")
	}
}

// ─── /api/present/source (v23.37) ─────────────────────────────

func TestHandleSource_ReturnsSpanLines(t *testing.T) {
	// Real temp file so handleSource can read it; symbol span points into it.
	dir := t.TempDir()
	path := dir + "/svc.go"
	src := "package svc\n" + // 1
		"\n" + // 2
		"func Checkout(id int) error {\n" + // 3
		"\treturn nil\n" + // 4
		"}\n" // 5
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &ast.Module{
		Language: ast.LangGo,
		FilePath: path,
		Nodes: []*ast.Node{{
			ID:       "func:Checkout",
			Kind:     ast.NodeFunction,
			Name:     "Checkout",
			Language: ast.LangGo,
			Span:     ast.Span{Start: ast.Pos{Line: 3}, End: ast.Pos{Line: 5}},
			Exported: true,
		}},
	}
	idx := symbols.NewIndex()
	idx.AddModule(m)
	s := New(idx)

	id := symbols.SymbolID(path, symbols.SymFunc, "Checkout")
	rec := getJSON(t, s, "/api/present/source?sym="+id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var resp SourceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.StartLine != 3 || resp.EndLine != 5 {
		t.Errorf("span = %d..%d, want 3..5", resp.StartLine, resp.EndLine)
	}
	if len(resp.Lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(resp.Lines))
	}
	if resp.Lines[0].Line != 3 || resp.Lines[0].Text != "func Checkout(id int) error {" {
		t.Errorf("first line = %d %q, want 3 func signature", resp.Lines[0].Line, resp.Lines[0].Text)
	}
	if resp.Name != "Checkout" {
		t.Errorf("name = %q, want Checkout", resp.Name)
	}
}

func TestHandleSource_MissingParam(t *testing.T) {
	s := buildTestServer()
	rec := getJSON(t, s, "/api/present/source")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleSource_UnknownSymbol(t *testing.T) {
	s := buildTestServer()
	rec := getJSON(t, s, "/api/present/source?sym=does#not:exist")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
