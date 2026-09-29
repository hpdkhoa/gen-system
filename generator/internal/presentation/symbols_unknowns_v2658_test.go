// symbols_unknowns_v2658_test.go — v26.58 (D087): handleSymbols parsed a COBOL
// file that the index did not hold with the copybook aware frontend, but read
// the unknown nodes only from ast.DefaultCache, where that parse never went.
// A COPY with no copybook gave an empty unknowns list, so the user did not see
// the copy_unresolved node.
package presentation

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func TestHandleSymbols_OnDemandCobolReportsUnknowns(t *testing.T) {
	root := t.TempDir()
	prog := filepath.Join(root, "nocopy.cob")
	src := strings.Replace(withCopyProgram, "WITHCOPY", "NOCOPY", 1)
	if err := os.WriteFile(prog, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(symbols.NewIndex())
	s.SetRoot(root, "", "")

	rec := getJSON(t, s, "/api/present/symbols?file="+url.QueryEscape(prog))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp SymbolMapResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range resp.Unknowns {
		if strings.HasPrefix(u.Reason, "copy_unresolved") {
			found = true
		}
	}
	if !found {
		t.Errorf("want a copy_unresolved unknown for COPY CUSTREC; unknowns: %+v", resp.Unknowns)
	}
}
