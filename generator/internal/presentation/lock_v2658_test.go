// lock_v2658_test.go — v26.58 (D084, D085, D086): several handlers read s.idx,
// s.root and s.beliefs directly, with no lock, while ReplaceContext and SetRoot
// write those fields under s.mu. A Studio project load in the middle of a
// request mixed the old and the new project, and the reads were data races.
// The handlers now read each field once through the locked accessors.
//
// The race detector does not run on this 32 bit Go, so these tests do not
// measure the race. They check that each handler still answers from the
// project that the last ReplaceContext and SetRoot installed.
package presentation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func serveV23(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	s.RegisterV23Routes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d; body: %s", path, rec.Code, rec.Body.String())
	}
	return rec
}

// swappedServer starts on the test index and then swaps in the call chain
// project, as a Studio load does.
func swappedServer(t *testing.T) (*Server, string, string, string) {
	t.Helper()
	t.Chdir(t.TempDir()) // the store of an unset root writes ./.gen
	chain, caller, mid, leaf := callChainServer()
	s := buildTestServer()
	s.ReplaceContext(chain.index(), nil, map[string]*flow.FuncEffects{
		mid: {SymbolID: mid, Effects: map[flow.EffectKind]bool{flow.EffectDB: true}},
	})
	return s, caller, mid, leaf
}

func TestLock_CallgraphAndEffectsUseSwappedIndex(t *testing.T) {
	s, caller, mid, leaf := swappedServer(t)

	edges := callgraphEdges(t, s, mid, "both")
	if !hasEdge(edges, caller, mid) || !hasEdge(edges, mid, leaf) {
		t.Errorf("callgraph after swap: edges %+v", edges)
	}

	var eff EffectsListResponse
	if err := json.Unmarshal(serveV23(t, s, "/api/present/v23/effects").Body.Bytes(), &eff); err != nil {
		t.Fatal(err)
	}
	if eff.Total != 1 || len(eff.ByEffect["db"]) != 1 || eff.ByEffect["db"][0].SymbolID != mid {
		t.Errorf("effects after swap: %+v", eff)
	}
}

func TestLock_ArchitectureSearchAndFilesUseSwappedProject(t *testing.T) {
	t.Chdir(t.TempDir()) // the store of an unset root writes ./.gen
	oldRoot, newRoot := t.TempDir(), t.TempDir()
	s := buildTestServer()
	s.SetRoot(oldRoot, "", "")

	file := filepath.Join(newRoot, "billing", "invoice.go")
	m := &ast.Module{
		Language: ast.LangGo, FilePath: file, PackageName: "billing", Checksum: "n1",
		Nodes: []*ast.Node{{ID: "func:IssueInvoice", Kind: ast.NodeFunction, Name: "IssueInvoice", Language: ast.LangGo, Exported: true}},
	}
	idx := symbols.NewIndex()
	idx.AddModule(m)
	s.ReplaceContext(idx, nil, nil)
	s.SetRoot(newRoot, "", "")

	var arch ArchitectureResponse
	if err := json.Unmarshal(serveV23(t, s, "/api/present/v23/architecture").Body.Bytes(), &arch); err != nil {
		t.Fatal(err)
	}
	if arch.TotalFiles != 1 || len(arch.Packages) != 1 || arch.Packages[0].Path != "billing" {
		t.Errorf("architecture after swap: files %d, packages %+v; want one file in package billing", arch.TotalFiles, arch.Packages)
	}

	var sr SearchResponse
	if err := json.Unmarshal(serveV23(t, s, "/api/present/v23/search?q=IssueInvoice").Body.Bytes(), &sr); err != nil {
		t.Fatal(err)
	}
	if sr.Total != 1 || sr.Hits[0].File != file {
		t.Errorf("search after swap: %+v", sr)
	}
	if sr2 := serveV23(t, s, "/api/present/v23/search?q=CreateUser"); json.Unmarshal(sr2.Body.Bytes(), &sr) != nil || sr.Total != 0 {
		t.Errorf("search after swap still finds a symbol of the old project: %+v", sr)
	}

	rec := getJSON(t, s, "/api/present/files")
	var files struct {
		Root string `json:"root"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &files) != nil {
		t.Fatalf("files: status %d; body: %s", rec.Code, rec.Body.String())
	}
	if want, _ := filepath.Abs(newRoot); files.Root != want {
		t.Errorf("files root = %q, want %q", files.Root, want)
	}
	if rec := getJSON(t, s, "/api/present/files?dir="+url.QueryEscape(oldRoot)); rec.Code != http.StatusForbidden {
		t.Errorf("files in the old root: status %d, want 403", rec.Code)
	}
}

func TestLock_BeliefHandlersUseStoreOfLoadedRoot(t *testing.T) {
	t.Chdir(t.TempDir()) // the store of an unset root writes ./.gen
	newRoot := t.TempDir()
	s := buildTestServer()
	chain, _, mid, _ := callChainServer()
	s.SetRoot(newRoot, "", "")
	s.ReplaceContext(chain.index(), nil, nil)

	var br BeliefViewResponse
	rec := getJSON(t, s, "/api/present/beliefs")
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &br) != nil {
		t.Fatalf("beliefs: status %d; body: %s", rec.Code, rec.Body.String())
	}
	var target string
	for _, b := range br.Beliefs {
		if strings.Contains(b.Subject, "CreateUser") || strings.Contains(b.ID, "CreateUser") {
			t.Errorf("beliefs after the load still hold the old project: %+v", b)
		}
		if strings.Contains(b.ID, mid) || b.Subject == mid {
			target = b.ID
		}
	}
	if target == "" {
		t.Fatalf("no belief about Mid after the load; beliefs: %+v", br.Beliefs)
	}

	rec = postJSON(t, s, "/api/present/beliefs/correct", BeliefCorrection{BeliefID: target, CorrectedTo: "service"})
	if rec.Code != http.StatusOK {
		t.Fatalf("correct: status %d; body: %s", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(newRoot, ".gen", "beliefs.json"))
	if err != nil || !strings.Contains(string(data), target) {
		t.Errorf("the correction is not in the store of the loaded root: %v", err)
	}

	var dr DiffViewResponse
	rec = getJSON(t, s, "/api/present/diff")
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &dr) != nil || dr.Now == "" {
		t.Errorf("diff: status %d; body: %s", rec.Code, rec.Body.String())
	}
}
