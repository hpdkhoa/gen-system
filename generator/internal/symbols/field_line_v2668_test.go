// field_line_v2668_test.go (v26.68): fields had no position, so every field
// symbol reported line 0 (COBOL record fields and Go struct fields alike).
package symbols

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

const cobolRecordSrc = `       IDENTIFICATION DIVISION.
       PROGRAM-ID. PAY.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 EMPLOYEE-RECORD.
          05 EMP-ID            PIC 9(6).
          05 EMP-NAME          PIC X(30).
       PROCEDURE DIVISION.
       MAIN-PARA.
           STOP RUN.
`

const goStructSrc = `package p

type Order struct {
	ID    int
	Total int
}
`

func fieldLines(t *testing.T, m *ast.Module) map[string]int {
	t.Helper()
	idx := NewIndex()
	idx.AddModule(m)
	out := map[string]int{}
	for _, s := range idx.AllSymbols() {
		if s.Kind == SymField {
			out[s.Name] = s.Span.Start.Line
		}
	}
	return out
}

func TestFieldsCarryTheirLine(t *testing.T) {
	cob, err := ast.ForLanguage(ast.LangCOBOL).Parse("/cbl/pay.cbl", []byte(cobolRecordSrc))
	if err != nil {
		t.Fatal(err)
	}
	goMod, err := (&ast.GoFrontend{}).Parse("/app/p.go", []byte(goStructSrc))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int{"EMP-ID": 6, "EMP-NAME": 7} {
		if got := fieldLines(t, cob)[name]; got != want {
			t.Errorf("COBOL field %s at line %d, want %d", name, got, want)
		}
	}
	for name, want := range map[string]int{"ID": 4, "Total": 5} {
		if got := fieldLines(t, goMod)[name]; got != want {
			t.Errorf("Go field %s at line %d, want %d", name, got, want)
		}
	}
}
