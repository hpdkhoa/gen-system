// cobol_frontend_test.go: tests for the COBOL/CICS language frontend
//
// v19: build-tag removed. COBOL frontend now registers in the default
// build, so these tests run with the standard `go test ./...`.

package ast

import (
	"strings"
	"testing"
)

// ─── Helpers ──────────────────────────────────────────────────

func cobolFE() Frontend {
	fe := ForLanguage(LangCOBOL)
	if fe == nil {
		panic("CobolFrontend not registered — check init()")
	}
	return fe
}

func parseCobolMust(t *testing.T, src string) *Module {
	t.Helper()
	fe := cobolFE()
	m, err := fe.Parse("test.cob", []byte(src))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if m == nil {
		t.Fatal("Parse returned nil module")
	}
	return m
}

// ─── Registration ─────────────────────────────────────────────

func TestCOBOL_Language(t *testing.T) {
	fe := cobolFE()
	if fe.Language() != LangCOBOL {
		t.Errorf("Language() = %q, want %q", fe.Language(), LangCOBOL)
	}
}

func TestCOBOL_Available(t *testing.T) {
	if !cobolFE().Available() {
		t.Error("CobolFrontend.Available() should be true (pure Go)")
	}
}

func TestCOBOL_Extensions(t *testing.T) {
	exts := cobolFE().Extensions()
	required := []string{".cob", ".cbl", ".cpy", ".cobol"}
	for _, req := range required {
		found := false
		for _, e := range exts {
			if e == req {
				found = true
			}
		}
		if !found {
			t.Errorf("Extensions() missing %q; got %v", req, exts)
		}
	}
}

func TestCOBOL_ForFile(t *testing.T) {
	cases := []string{"program.cob", "copybook.cpy", "main.cbl", "app.cobol"}
	for _, path := range cases {
		fe := ForFile(path)
		if fe == nil {
			t.Errorf("ForFile(%q) = nil, want CobolFrontend", path)
			continue
		}
		if fe.Language() != LangCOBOL {
			t.Errorf("ForFile(%q).Language() = %q, want cobol", path, fe.Language())
		}
	}
}

// ─── PROGRAM-ID extraction ────────────────────────────────────

const programIDSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. PAYROLL-CALC.
       AUTHOR. ACME CORP.

       ENVIRONMENT DIVISION.

       DATA DIVISION.

       PROCEDURE DIVISION.
       MAIN-LOGIC.
           STOP RUN.
`

func TestCOBOL_ProgramID(t *testing.T) {
	m := parseCobolMust(t, programIDSrc)
	if m.PackageName != "PAYROLL-CALC" {
		t.Errorf("PackageName = %q, want PAYROLL-CALC", m.PackageName)
	}
	// A NodeModule should exist with the program name.
	found := false
	for _, n := range m.Nodes {
		if n.Kind == NodeModule && n.Name == "PAYROLL-CALC" {
			found = true
		}
	}
	if !found {
		t.Error("NodeModule with Name=PAYROLL-CALC not found")
	}
}

// ─── Data Division: 01-level records ─────────────────────────

const dataDivSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. DATATEST.

       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-CUSTOMER-RECORD.
          05 WS-CUST-ID      PIC 9(6).
          05 WS-CUST-NAME    PIC X(40).
          05 WS-CUST-BALANCE PIC S9(9)V99.
       01 WS-COUNTER         PIC 9(4) VALUE ZEROS.

       PROCEDURE DIVISION.
       STOP RUN.
`

func TestCOBOL_01LevelRecord(t *testing.T) {
	m := parseCobolMust(t, dataDivSrc)

	var record *Node
	for _, n := range m.Nodes {
		if n.Kind == NodeStruct && n.Name == "WS-CUSTOMER-RECORD" {
			record = n
		}
	}
	if record == nil {
		t.Fatal("NodeStruct WS-CUSTOMER-RECORD not found")
	}
	if len(record.Fields) != 3 {
		t.Errorf("expected 3 fields, got %d: %+v", len(record.Fields), record.Fields)
	}
	// Check PIC clause extraction
	for _, f := range record.Fields {
		if f.Type.Name == "" || f.Type.Name == "?" {
			t.Errorf("field %q has empty/unknown PIC: %q", f.Name, f.Type.Name)
		}
	}
}

func TestCOBOL_SimpleCounter(t *testing.T) {
	m := parseCobolMust(t, dataDivSrc)
	var counter *Node
	for _, n := range m.Nodes {
		if n.Kind == NodeStruct && n.Name == "WS-COUNTER" {
			counter = n
		}
	}
	if counter == nil {
		t.Fatal("NodeStruct WS-COUNTER not found")
	}
}

// ─── Procedure Division ────────────────────────────────────────

const procedureSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. PROCTEST.

       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-FLAG PIC X VALUE 'N'.

       PROCEDURE DIVISION.
       MAIN-SECTION SECTION.
       MAIN-PARA.
           PERFORM VALIDATE-PARA
           IF WS-FLAG = 'Y'
               PERFORM PROCESS-PARA
           END-IF
           STOP RUN.
       VALIDATE-PARA.
           MOVE 'Y' TO WS-FLAG.
       PROCESS-PARA.
           DISPLAY 'Processing'.
`

func TestCOBOL_ParagraphsExtracted(t *testing.T) {
	m := parseCobolMust(t, procedureSrc)

	paragraphs := map[string]bool{}
	for _, n := range m.Nodes {
		if n.Kind == NodeFunction || n.Kind == NodeProcedure {
			paragraphs[n.Name] = true
		}
	}
	required := []string{"MAIN-PARA", "VALIDATE-PARA", "PROCESS-PARA"}
	for _, req := range required {
		if !paragraphs[req] {
			t.Errorf("paragraph %q not found; extracted: %v", req, paragraphs)
		}
	}
}

func TestCOBOL_PerformBecomesNodeCall(t *testing.T) {
	m := parseCobolMust(t, procedureSrc)

	// PERFORM VALIDATE-PARA in MAIN-PARA should become a NodeCall child.
	var mainPara *Node
	for _, n := range m.Nodes {
		if n.Name == "MAIN-PARA" {
			mainPara = n
		}
	}
	if mainPara == nil {
		t.Fatal("MAIN-PARA not found")
	}
	found := false
	for _, child := range mainPara.Children {
		if child.Kind == NodeCall && child.Callee == "VALIDATE-PARA" {
			found = true
		}
	}
	if !found {
		t.Errorf("PERFORM VALIDATE-PARA should produce NodeCall; children: %v", mainPara.Children)
	}
}

func TestCOBOL_StopRunBecomesNodeReturn(t *testing.T) {
	m := parseCobolMust(t, procedureSrc)
	var mainPara *Node
	for _, n := range m.Nodes {
		if n.Name == "MAIN-PARA" {
			mainPara = n
		}
	}
	if mainPara == nil {
		t.Fatal("MAIN-PARA not found")
	}
	hasReturn := false
	for _, child := range mainPara.Children {
		if child.Kind == NodeReturn {
			hasReturn = true
		}
	}
	if !hasReturn {
		t.Error("STOP RUN should produce NodeReturn child")
	}
}

func TestCOBOL_IfBecomesNodeBranch(t *testing.T) {
	m := parseCobolMust(t, procedureSrc)
	var mainPara *Node
	for _, n := range m.Nodes {
		if n.Name == "MAIN-PARA" {
			mainPara = n
		}
	}
	if mainPara == nil {
		t.Skip("MAIN-PARA not found — skipping branch test")
	}
	hasBranch := false
	for _, child := range mainPara.Children {
		if child.Kind == NodeBranch {
			hasBranch = true
		}
	}
	if !hasBranch {
		t.Error("IF statement should produce NodeBranch child")
	}
}

// ─── CICS EXEC blocks ─────────────────────────────────────────

const cicsSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. CICSTEST.

       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-DATA PIC X(100).

       PROCEDURE DIVISION.
       MAIN-LOGIC.
           EXEC CICS READ
               FILE('CUSTOMER')
               INTO(WS-DATA)
               RIDFLD(WS-CUST-ID)
               RESP(WS-RESP)
           END-EXEC
           EXEC CICS WRITEQ TS
               QUEUE('MYQUEUE')
               FROM(WS-DATA)
           END-EXEC
           EXEC CICS RETURN
           END-EXEC.
`

func TestCOBOL_CICSReadBecomesNodeCall(t *testing.T) {
	m := parseCobolMust(t, cicsSrc)

	var mainLogic *Node
	for _, n := range m.Nodes {
		if n.Name == "MAIN-LOGIC" {
			mainLogic = n
		}
	}
	if mainLogic == nil {
		t.Fatal("MAIN-LOGIC paragraph not found")
	}

	cicsRead := false
	cicsWrite := false
	for _, child := range mainLogic.Children {
		if child.Kind == NodeCall {
			if strings.HasPrefix(child.Callee, "CICS.READ") {
				cicsRead = true
			}
			if strings.HasPrefix(child.Callee, "CICS.WRITEQ") {
				cicsWrite = true
			}
		}
	}
	if !cicsRead {
		t.Errorf("EXEC CICS READ should produce NodeCall with Callee=CICS.READ; children: %v",
			mainLogic.Children)
	}
	if !cicsWrite {
		t.Errorf("EXEC CICS WRITEQ should produce NodeCall with Callee=CICS.WRITEQ; children: %v",
			mainLogic.Children)
	}
}

func TestCOBOL_CICSCalleePrefix(t *testing.T) {
	m := parseCobolMust(t, cicsSrc)
	for _, n := range m.Nodes {
		for _, child := range n.Children {
			if child.Kind == NodeCall && strings.HasPrefix(child.Callee, "CICS.") {
				// All CICS callee names must have the "CICS." prefix.
				if !strings.Contains(child.Callee, ".") {
					t.Errorf("CICS call missing prefix: %q", child.Callee)
				}
			}
		}
	}
}

// ─── CALL statement ───────────────────────────────────────────

const callSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. CALLTEST.

       PROCEDURE DIVISION.
       CALL-PARA.
           CALL 'SUBPGM01' USING WS-DATA.
           CALL 'ERRHANDLR'.
           STOP RUN.
`

func TestCOBOL_CallStatement(t *testing.T) {
	m := parseCobolMust(t, callSrc)

	var callPara *Node
	for _, n := range m.Nodes {
		if n.Name == "CALL-PARA" {
			callPara = n
		}
	}
	if callPara == nil {
		t.Fatal("CALL-PARA not found")
	}
	callees := map[string]bool{}
	for _, child := range callPara.Children {
		if child.Kind == NodeCall {
			callees[child.Callee] = true
		}
	}
	if !callees["SUBPGM01"] {
		t.Errorf("CALL 'SUBPGM01' should produce NodeCall(SUBPGM01); callees=%v", callees)
	}
	if !callees["ERRHANDLR"] {
		t.Errorf("CALL 'ERRHANDLR' should produce NodeCall(ERRHANDLR); callees=%v", callees)
	}
}

// ─── Checksum ─────────────────────────────────────────────────

func TestCOBOL_Checksum(t *testing.T) {
	fe := cobolFE()
	m1, _ := fe.Parse("a.cob", []byte("IDENTIFICATION DIVISION.\nPROGRAM-ID. A."))
	m2, _ := fe.Parse("a.cob", []byte("IDENTIFICATION DIVISION.\nPROGRAM-ID. B."))
	if m1.Checksum == "" {
		t.Error("checksum should be set")
	}
	if len(m1.Checksum) != 64 {
		t.Errorf("checksum should be 64 hex chars, got %d", len(m1.Checksum))
	}
	if m1.Checksum == m2.Checksum {
		t.Error("different source should produce different checksums")
	}
}

// ─── Empty source / edge cases ────────────────────────────────

func TestCOBOL_EmptySource(t *testing.T) {
	fe := cobolFE()
	m, err := fe.Parse("empty.cob", []byte{})
	if err != nil {
		t.Fatalf("empty source should not error: %v", err)
	}
	if m == nil {
		t.Fatal("empty source should return non-nil module")
	}
}

func TestCOBOL_CommentLinesSkipped(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
      * This is a comment line.
       PROGRAM-ID. COMTEST.
      * Another comment.
       PROCEDURE DIVISION.
       STOP RUN.
`
	m := parseCobolMust(t, src)
	if m.PackageName != "COMTEST" {
		t.Errorf("PackageName = %q, want COMTEST", m.PackageName)
	}
}

func TestCOBOL_Language_Constant(t *testing.T) {
	if LangCOBOL != "cobol" {
		t.Errorf("LangCOBOL = %q, want \"cobol\"", LangCOBOL)
	}
}

func TestCOBOL_Emit_RoundTrip(t *testing.T) {
	fe := cobolFE()
	src := []byte("       IDENTIFICATION DIVISION.\n       PROGRAM-ID. RT.\n")
	m, _ := fe.Parse("rt.cob", src)
	out, err := fe.Emit(m)
	if err != nil {
		t.Fatalf("Emit error: %v", err)
	}
	if string(out) != string(src) {
		t.Error("Emit should return original source unchanged")
	}
}

// v24: inline PERFORM UNTIL … END-PERFORM must nest the statements between the
// header and END-PERFORM as the loop body (previously the body was lost and the
// statements attached at the wrong scope level).
func TestCOBOL_InlinePerformUntilBodyNests_v24(t *testing.T) {
	src := `       IDENTIFICATION DIVISION.
       PROGRAM-ID. LOOPTEST.
       PROCEDURE DIVISION.
       MAIN-PARA.
           PERFORM UNTIL WS-DONE = 'Y'
               ADD 1 TO WS-COUNTER
               MOVE 'Y' TO WS-DONE
           END-PERFORM.
           STOP RUN.
`
	m := parseCobolMust(t, src)
	var mainPara *Node
	for _, n := range m.Nodes {
		if n.Name == "MAIN-PARA" {
			mainPara = n
		}
	}
	if mainPara == nil {
		t.Fatal("MAIN-PARA not found")
	}
	var loop *Node
	for _, c := range mainPara.Children {
		if c.Kind == NodeLoop {
			loop = c
		}
	}
	if loop == nil {
		t.Fatalf("inline PERFORM UNTIL should produce a NodeLoop; children: %v", mainPara.Children)
	}
	// the loop body (Children) should hold the ADD and MOVE statements.
	if len(loop.Children) < 2 {
		t.Errorf("inline PERFORM UNTIL body should nest its 2 statements as loop children, got %d: %v", len(loop.Children), loop.Children)
	}
	// STOP RUN must not be inside the loop (it's after END-PERFORM).
	for _, c := range loop.Children {
		if c.Kind == NodeReturn {
			t.Error("STOP RUN after END-PERFORM must not be nested inside the loop body")
		}
	}
}
