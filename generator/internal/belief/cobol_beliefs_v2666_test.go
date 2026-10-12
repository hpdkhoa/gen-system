// cobol_beliefs_v2666_test.go (v26.66): COBOL paragraphs have no parameters,
// so their beliefs listed only the action guessed from the name and the calls.
// They now list the data items the paragraph reads (inputs) and writes
// (data-change), limited to names the index knows as data items.
package belief

import (
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

const cobolBeliefSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. PAYROLL.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-EOF              PIC X VALUE "N".
       01 WS-GROSS            PIC 9(7)V99.
       01 WS-TAX              PIC 9(7)V99.
       01 WS-TAX-RATE         PIC V999.
       01 EMP-GRADE           PIC X.
       PROCEDURE DIVISION.
       MAIN-PARA.
           OPEN INPUT EMPLOYEE-FILE
           READ EMPLOYEE-FILE
               AT END
                   MOVE "Y" TO WS-EOF
           END-READ
           CLOSE EMPLOYEE-FILE
           STOP RUN.
       CALC-TAX.
           EVALUATE EMP-GRADE
               WHEN "A"
                   MOVE .350 TO WS-TAX-RATE
               WHEN OTHER
                   MOVE .100 TO WS-TAX-RATE
           END-EVALUATE
           COMPUTE WS-TAX ROUNDED = WS-GROSS * WS-TAX-RATE.
`

func cobolBeliefs(t *testing.T) map[string]string {
	t.Helper()
	m, err := ast.ForLanguage(ast.LangCOBOL).Parse("/project/p.cbl", []byte(cobolBeliefSrc))
	if err != nil {
		t.Fatal(err)
	}
	idx := symbols.NewIndex()
	idx.AddModule(m)
	idx.ResolveAll()
	bs := NewStore(t.TempDir())
	bs.RebuildFromIndex(idx, flow.InferEffects(m, nil))
	out := map[string]string{}
	for _, b := range bs.All() {
		out[b.Subject+" "+string(b.Kind)] = b.Object
	}
	return out
}

func TestCobolParagraphBeliefsListInputsAndWrites(t *testing.T) {
	got := cobolBeliefs(t)
	if want := "EMP-GRADE, WS-GROSS, WS-TAX-RATE"; got["CALC-TAX "+string(KindInputs)] != want {
		t.Errorf("CALC-TAX inputs = %q, want %q (all: %v)", got["CALC-TAX "+string(KindInputs)], want, got)
	}
	if want := "WS-TAX, WS-TAX-RATE"; got["CALC-TAX "+string(KindDataChange)] != want {
		t.Errorf("CALC-TAX data-change = %q, want %q", got["CALC-TAX "+string(KindDataChange)], want)
	}
	// EMPLOYEE-FILE has no data item in this source, so it never shows up.
	for k, v := range got {
		if strings.Contains(v, "EMPLOYEE-FILE") || strings.Contains(v, "ROUNDED") {
			t.Errorf("%s = %q holds a name that is not a data item", k, v)
		}
	}
}

func TestCobolFileReadIsNotAFilesystemChange(t *testing.T) {
	got := cobolBeliefs(t)
	if v := got["MAIN-PARA "+string(KindDataChange)]; v != "WS-EOF" {
		t.Fatalf("MAIN-PARA only reads its file: data-change = %q, want %q", v, "WS-EOF")
	}
	if v := got["MAIN-PARA "+string(KindEffect)]; v != "fs" {
		t.Fatalf("MAIN-PARA effect = %q, want fs", v)
	}
}
