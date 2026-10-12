// cobol_dataflow_test.go (v26.66): a COBOL paragraph has no parameters, so
// its inputs and the data it writes come from its statements. File statements
// produced no node and no effect, so a paragraph that wrote a payslip file was
// "unknown" and its beliefs listed nothing it wrote.
package flow

import (
	"reflect"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

const cobolPayrollSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. PAYROLL.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-EOF              PIC X VALUE "N".
       01 WS-GROSS            PIC 9(7)V99.
       01 WS-TAX              PIC 9(7)V99.
       01 WS-TAX-RATE         PIC V999.
       01 WS-COUNT            PIC 9(6) VALUE 0.
       PROCEDURE DIVISION.
       MAIN-PARA.
           OPEN INPUT EMPLOYEE-FILE
           READ EMPLOYEE-FILE
               AT END
                   MOVE "Y" TO WS-EOF
           END-READ
           CLOSE EMPLOYEE-FILE
           STOP RUN.
       PROCESS-EMPLOYEE.
           MOVE EMP-ID TO PS-EMP-ID
           COMPUTE PS-NET = WS-GROSS - WS-TAX
           WRITE PAYSLIP-RECORD
           ADD 1 TO WS-COUNT.
       CALC-TAX.
           EVALUATE EMP-GRADE
               WHEN "A"
                   MOVE .350 TO WS-TAX-RATE
               WHEN OTHER
                   MOVE .100 TO WS-TAX-RATE
           END-EVALUATE
           IF EMP-REGION = "NY"
               ADD .040 TO WS-TAX-RATE
           END-IF
           COMPUTE WS-TAX ROUNDED = WS-GROSS * WS-TAX-RATE.
`

func payrollEffects(t *testing.T) (map[string]*FuncEffects, string) {
	t.Helper()
	m, err := ast.ForLanguage(ast.LangCOBOL).Parse("p.cbl", []byte(cobolPayrollSrc))
	if err != nil {
		t.Fatal(err)
	}
	return InferEffects(m, nil), m.FilePath
}

func TestCobolFileStatementsAreFileEffects(t *testing.T) {
	efx, file := payrollEffects(t)
	mainPara := efx[symbols.SymbolID(file, symbols.SymFunc, "MAIN-PARA")]
	if mainPara == nil || !mainPara.Effects[EffectFS] || mainPara.FSWrite {
		t.Fatalf("MAIN-PARA opens, reads and closes a file: want fs without a write, got %+v", mainPara)
	}
	proc := efx[symbols.SymbolID(file, symbols.SymFunc, "PROCESS-EMPLOYEE")]
	if proc == nil || !proc.Effects[EffectFS] || !proc.FSWrite {
		t.Fatalf("PROCESS-EMPLOYEE writes PAYSLIP-RECORD: want fs with a write, got %+v", proc)
	}
}

func TestCobolReadsAndWrites(t *testing.T) {
	efx, file := payrollEffects(t)
	cases := []struct {
		para          string
		reads, writes []string
	}{
		{"PROCESS-EMPLOYEE",
			[]string{"EMP-ID", "WS-COUNT", "WS-GROSS", "WS-TAX"},
			[]string{"PAYSLIP-RECORD", "PS-EMP-ID", "PS-NET", "WS-COUNT"}},
		{"CALC-TAX",
			[]string{"EMP-GRADE", "EMP-REGION", "WS-GROSS", "WS-TAX-RATE"},
			[]string{"WS-TAX", "WS-TAX-RATE"}},
	}
	for _, c := range cases {
		got := efx[symbols.SymbolID(file, symbols.SymFunc, c.para)]
		if got == nil {
			t.Fatalf("no effects for %s", c.para)
		}
		if !reflect.DeepEqual(got.DataReads, c.reads) {
			t.Errorf("%s reads %v, want %v", c.para, got.DataReads, c.reads)
		}
		if !reflect.DeepEqual(got.DataWrites, c.writes) {
			t.Errorf("%s writes %v, want %v", c.para, got.DataWrites, c.writes)
		}
	}
}
