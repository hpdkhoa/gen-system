// cobol_copy_v2658_test.go (v26.58): COPY expansion defects.
//
// D056: expandCopybooks marked a copybook as visited and never cleared the
// mark, so a second COPY of the same book (a common pattern with REPLACING)
// was dropped as copy_cycle and its record lost all fields. No cycle existed.
package ast

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCOBOL_SecondCopyOfSameCopybook(t *testing.T) {
	dir := t.TempDir()
	book := `          05 :P:-STREET  PIC X(30).
          05 :P:-CITY    PIC X(20).
`
	if err := os.WriteFile(filepath.Join(dir, "ADDR.cpy"), []byte(book), 0644); err != nil {
		t.Fatal(err)
	}
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. TWOCOPY.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 HOME-ADDR.
       COPY ADDR REPLACING ==:P:== BY ==HOME==.
       01 WORK-ADDR.
       COPY ADDR REPLACING ==:P:== BY ==WORK==.
       PROCEDURE DIVISION.
           STOP RUN.
`
	fe := &CobolFrontend{CopybookPath: []string{dir}}
	m, err := fe.Parse("twocopy.cob", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	counts := map[string]int{}
	for _, n := range m.Nodes {
		if n.Kind == NodeStruct {
			counts[n.Name] = len(n.Fields)
		}
		if n.Kind == NodeUnknown && strings.HasPrefix(n.UnknownReason, "copy_cycle") {
			t.Errorf("false cycle reported: %s", n.UnknownReason)
		}
	}
	for _, rec := range []string{"HOME-ADDR", "WORK-ADDR"} {
		if counts[rec] != 2 {
			t.Errorf("%s has %d fields, want 2", rec, counts[rec])
		}
	}
}

// A real cycle (A copies B, B copies A) must still stop and be reported.
func TestCOBOL_RealCopyCycleStillReported(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "BOOKA.cpy"), []byte("          05 A-FLD PIC X.\n       COPY BOOKB.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "BOOKB.cpy"), []byte("          05 B-FLD PIC X.\n       COPY BOOKA.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. CYC.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 REC.
       COPY BOOKA.
       PROCEDURE DIVISION.
           STOP RUN.
`
	fe := &CobolFrontend{CopybookPath: []string{dir}}
	m, err := fe.Parse("cyc.cob", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	found := false
	for _, n := range m.Nodes {
		if n.Kind == NodeUnknown && n.UnknownReason == "copy_cycle:BOOKA" {
			found = true
		}
	}
	if !found {
		t.Errorf("copy_cycle:BOOKA not reported; nodes: %v", nodeNames(m.Nodes))
	}
}
