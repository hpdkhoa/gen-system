// copybooks_v2658_test.go — v26.58 (D082): SetRoot stored the whole
// --copybooks text as one folder path. With two folders in the list, the on
// demand COBOL parse in handleSymbols searched one folder named by the whole
// list, so every COPY stayed unresolved and the copybook symbols were missing.
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

const custrecBook = `       01 CUSTOMER-RECORD.
          05 CUST-ID    PIC 9(6).
          05 CUST-NAME  PIC X(40).
`

const withCopyProgram = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. WITHCOPY.

       DATA DIVISION.
       WORKING-STORAGE SECTION.
       COPY CUSTREC.

       PROCEDURE DIVISION.
       STOP RUN.
`

func TestHandleSymbols_CopybookListWithTwoFolders(t *testing.T) {
	root, booksA, booksB := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(booksB, "CUSTREC.cpy"), []byte(custrecBook), 0o644); err != nil {
		t.Fatal(err)
	}
	prog := filepath.Join(root, "withcopy.cob")
	if err := os.WriteFile(prog, []byte(withCopyProgram), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(symbols.NewIndex())
	s.SetRoot(root, booksA+string(os.PathListSeparator)+booksB, "")
	rec := getJSON(t, s, "/api/present/symbols?file="+url.QueryEscape(prog))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp SymbolMapResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sym := range resp.Symbols {
		if strings.Contains(sym.Name, "CUSTOMER-RECORD") {
			found = true
		}
	}
	if !found {
		t.Errorf("CUSTOMER-RECORD from the second copybook folder is missing; symbols: %+v; unknowns: %+v", resp.Symbols, resp.Unknowns)
	}
}
