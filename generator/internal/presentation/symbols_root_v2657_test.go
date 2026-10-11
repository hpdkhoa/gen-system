// symbols_root_v2657_test.go (v26.57, D013): /api/present/symbols must not
// parse a file outside the project. handleFiles refused such a folder with 403,
// but handleSymbols parsed and indexed any path it was given, so a request
// could read the functions of any .go file on the machine, and
// /api/present/source then returned their bodies. Both servers listen on all
// interfaces, so the endpoint was open to the network.
package presentation

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func writeGo(t *testing.T, dir, name, src string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHandleSymbols_RefusesFileOutsideRoot(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	inside := writeGo(t, root, "a.go", "package a\n\nfunc Inside() {}\n")
	secret := writeGo(t, outside, "secret.go", "package s\n\nfunc Secret() string { return \"key\" }\n")

	s := New(symbols.NewIndex())
	s.SetRoot(root, "", "")
	before := len(s.index().AllSymbols())

	for _, p := range []string{
		secret,
		filepath.Join(root, "..", filepath.Base(outside), "secret.go"),
	} {
		rec := getJSON(t, s, "/api/present/symbols?file="+url.QueryEscape(p))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403; body: %s", p, rec.Code, rec.Body.String())
		}
	}
	if after := len(s.index().AllSymbols()); after != before {
		t.Errorf("a refused request still changed the index: %d symbols, was %d", after, before)
	}

	rec := getJSON(t, s, "/api/present/symbols?file="+url.QueryEscape(inside))
	if rec.Code != http.StatusOK {
		t.Errorf("file inside the root: status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSymbols_RefusesOnDemandParseWithoutRoot(t *testing.T) {
	secret := writeGo(t, t.TempDir(), "secret.go", "package s\n\nfunc Secret() {}\n")
	s := New(symbols.NewIndex())
	rec := getJSON(t, s, "/api/present/symbols?file="+url.QueryEscape(secret))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403 when no project root is set", rec.Code)
	}
}
