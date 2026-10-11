// cobol_frontend_v19_test.go: v19 tests for the feature-complete COBOL
// frontend: EVALUATE, COPY books, REDEFINES, OCCURS, FILE-CONTROL/SELECT,
// FD, COMP-3 and other USAGE clauses, expanded CICS verb surface.
//
// v19.6: assertions updated to use Node.ImportPath (was Node.Path) and
// Node.RawSource (was Node.Tag). Node has no Tag field. Field.Tag
// assertions are unchanged because Field actually has a Tag.
package ast

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── EVALUATE ─────────────────────────────────────────────────

const evaluateSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. EVALTEST.

       PROCEDURE DIVISION.
       MAIN-PARA.
           EVALUATE WS-CODE
               WHEN 1
                   PERFORM HANDLE-ONE
               WHEN 2
                   PERFORM HANDLE-TWO
               WHEN OTHER
                   PERFORM HANDLE-DEFAULT
           END-EVALUATE
           STOP RUN.
       HANDLE-ONE.
           DISPLAY 'one'.
       HANDLE-TWO.
           DISPLAY 'two'.
       HANDLE-DEFAULT.
           DISPLAY 'default'.
`

func TestCOBOL_EvaluateProducesBranch(t *testing.T) {
	m := parseCobolMust(t, evaluateSrc)

	var mainPara *Node
	for _, n := range m.Nodes {
		if n.Name == "MAIN-PARA" {
			mainPara = n
		}
	}
	if mainPara == nil {
		t.Fatal("MAIN-PARA not found")
	}

	var evalNode *Node
	for _, c := range mainPara.Children {
		if c.Kind == NodeBranch && c.Name == "EVALUATE" {
			evalNode = c
		}
	}
	if evalNode == nil {
		t.Fatalf("EVALUATE should produce NodeBranch with Name=EVALUATE; children: %+v", mainPara.Children)
	}
	// Expect three WHEN children: two value WHENs and one WHEN OTHER.
	whenCount := 0
	whenOtherCount := 0
	for _, c := range evalNode.Children {
		if c.Kind != NodeBranch {
			continue
		}
		if c.Name == "WHEN_OTHER" {
			whenOtherCount++
		} else if c.Name == "WHEN" {
			whenCount++
		}
	}
	if whenCount != 2 {
		t.Errorf("expected 2 WHEN clauses, got %d", whenCount)
	}
	if whenOtherCount != 1 {
		t.Errorf("expected 1 WHEN OTHER clause, got %d", whenOtherCount)
	}
}

func TestCOBOL_EvaluatePerformCalls(t *testing.T) {
	m := parseCobolMust(t, evaluateSrc)
	var mainPara *Node
	for _, n := range m.Nodes {
		if n.Name == "MAIN-PARA" {
			mainPara = n
		}
	}
	if mainPara == nil {
		t.Fatal("MAIN-PARA not found")
	}

	// Walk all descendants of mainPara looking for NodeCall to HANDLE-*.
	callees := map[string]bool{}
	var walk func(*Node)
	walk = func(n *Node) {
		for _, c := range n.Children {
			if c.Kind == NodeCall {
				callees[c.Callee] = true
			}
			walk(c)
		}
	}
	walk(mainPara)
	for _, want := range []string{"HANDLE-ONE", "HANDLE-TWO", "HANDLE-DEFAULT"} {
		if !callees[want] {
			t.Errorf("WHEN clauses should produce NodeCall(%s); callees=%v", want, callees)
		}
	}
}

// ─── COPY books ───────────────────────────────────────────────

func TestCOBOL_CopyUnresolvedWithoutPath(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. NOCOPY.

       DATA DIVISION.
       WORKING-STORAGE SECTION.
       COPY CUSTREC.

       PROCEDURE DIVISION.
       STOP RUN.
`
	fe := &CobolFrontend{} // no CopybookPath
	m, err := fe.Parse("nocopy.cob", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	found := false
	for _, n := range m.Nodes {
		if n.Kind == NodeUnknown && strings.HasPrefix(n.UnknownReason, "copy_unresolved:") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected NodeUnknown{copy_unresolved} for unresolved COPY; got nodes: %v", nodeKinds(m.Nodes))
	}
}

func TestCOBOL_CopyResolvedFromPath(t *testing.T) {
	dir := t.TempDir()
	copybook := `       01 CUSTOMER-RECORD.
          05 CUST-ID    PIC 9(6).
          05 CUST-NAME  PIC X(40).
`
	if err := os.WriteFile(filepath.Join(dir, "CUSTREC.cpy"), []byte(copybook), 0644); err != nil {
		t.Fatal(err)
	}
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. WITHCOPY.

       DATA DIVISION.
       WORKING-STORAGE SECTION.
       COPY CUSTREC.

       PROCEDURE DIVISION.
       STOP RUN.
`
	fe := &CobolFrontend{CopybookPath: []string{dir}}
	m, err := fe.Parse("withcopy.cob", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var customerRec *Node
	for _, n := range m.Nodes {
		if n.Kind == NodeStruct && n.Name == "CUSTOMER-RECORD" {
			customerRec = n
		}
	}
	if customerRec == nil {
		t.Fatalf("CUSTOMER-RECORD (from copybook) should be present; got: %v", nodeNames(m.Nodes))
	}
	if len(customerRec.Fields) != 2 {
		t.Errorf("CUSTOMER-RECORD should have 2 fields from copybook; got %d: %+v",
			len(customerRec.Fields), customerRec.Fields)
	}
}

func TestCOBOL_CopyReplacingApplied(t *testing.T) {
	dir := t.TempDir()
	copybook := `       01 PREFIX-RECORD.
          05 PREFIX-ID    PIC 9(6).
          05 PREFIX-NAME  PIC X(40).
`
	if err := os.WriteFile(filepath.Join(dir, "BOOK.cpy"), []byte(copybook), 0644); err != nil {
		t.Fatal(err)
	}
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. RP.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       COPY BOOK REPLACING ==PREFIX== BY ==WS-CUSTOMER==.
       PROCEDURE DIVISION.
       STOP RUN.
`
	fe := &CobolFrontend{CopybookPath: []string{dir}}
	m, err := fe.Parse("rp.cob", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// At least one struct should now contain the replacement string.
	var found bool
	for _, n := range m.Nodes {
		if n.Kind == NodeStruct && strings.Contains(n.Name, "WS-CUSTOMER") {
			found = true
		}
	}
	if !found {
		t.Errorf("REPLACING should rename PREFIX to WS-CUSTOMER in copybook; got: %v", nodeNames(m.Nodes))
	}
}

// ─── REDEFINES / OCCURS / VALUE / USAGE ───────────────────────

const dataMetadataSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. META.

       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-TABLES.
          05 WS-AMOUNT       PIC S9(9)V99 COMP-3.
          05 WS-RAW-RECORD   PIC X(80).
          05 WS-PARSED-VIEW  REDEFINES WS-RAW-RECORD.
             10 WS-FIELD-A   PIC X(20).
             10 WS-FIELD-B   PIC X(60).
          05 WS-ENTRIES      OCCURS 10 TIMES.
             10 WS-CODE      PIC X(4).
             10 WS-VALUE     PIC 9(8) COMP.
          05 WS-VAR-TABLE    OCCURS 1 TO 100 TIMES
                             DEPENDING ON WS-COUNT
                             PIC X(50).
          05 WS-COUNT        PIC 9(4) VALUE ZEROS.
          05 WS-FLAG         PIC X VALUE 'Y'.

       PROCEDURE DIVISION.
       STOP RUN.
`

// findField walks Fields slice of a struct.
func findField(n *Node, name string) *Field {
	for i := range n.Fields {
		if n.Fields[i].Name == name {
			return &n.Fields[i]
		}
	}
	return nil
}

func TestCOBOL_USAGE_COMP3(t *testing.T) {
	m := parseCobolMust(t, dataMetadataSrc)
	var rec *Node
	for _, n := range m.Nodes {
		if n.Name == "WS-TABLES" {
			rec = n
		}
	}
	if rec == nil {
		t.Fatal("WS-TABLES not found")
	}
	f := findField(rec, "WS-AMOUNT")
	if f == nil {
		t.Fatal("WS-AMOUNT field not found")
	}
	if !strings.Contains(f.Tag, "usage=COMP-3") {
		t.Errorf("WS-AMOUNT should have Tag containing usage=COMP-3; got Tag=%q", f.Tag)
	}
}

func TestCOBOL_USAGE_COMP(t *testing.T) {
	m := parseCobolMust(t, dataMetadataSrc)
	var rec *Node
	for _, n := range m.Nodes {
		if n.Name == "WS-TABLES" {
			rec = n
		}
	}
	if rec == nil {
		t.Fatal("WS-TABLES not found")
	}
	f := findField(rec, "WS-VALUE")
	if f == nil {
		t.Fatal("WS-VALUE field not found")
	}
	if !strings.Contains(f.Tag, "usage=COMP") {
		t.Errorf("WS-VALUE should have Tag containing usage=COMP; got Tag=%q", f.Tag)
	}
}

func TestCOBOL_REDEFINES_Captured(t *testing.T) {
	m := parseCobolMust(t, dataMetadataSrc)
	var rec *Node
	for _, n := range m.Nodes {
		if n.Name == "WS-TABLES" {
			rec = n
		}
	}
	if rec == nil {
		t.Fatal("WS-TABLES not found")
	}
	f := findField(rec, "WS-PARSED-VIEW")
	if f == nil {
		t.Fatal("WS-PARSED-VIEW field not found")
	}
	if !strings.Contains(f.Tag, "redefines=WS-RAW-RECORD") {
		t.Errorf("WS-PARSED-VIEW should have Tag redefines=WS-RAW-RECORD; got Tag=%q", f.Tag)
	}
}

func TestCOBOL_OCCURS_Captured(t *testing.T) {
	m := parseCobolMust(t, dataMetadataSrc)
	var rec *Node
	for _, n := range m.Nodes {
		if n.Name == "WS-TABLES" {
			rec = n
		}
	}
	if rec == nil {
		t.Fatal("WS-TABLES not found")
	}
	f := findField(rec, "WS-ENTRIES")
	if f == nil {
		t.Fatal("WS-ENTRIES field not found")
	}
	if !strings.Contains(f.Tag, "occurs=10") {
		t.Errorf("WS-ENTRIES should have Tag occurs=10; got Tag=%q", f.Tag)
	}
}

func TestCOBOL_OCCURS_DependingOn_Captured(t *testing.T) {
	m := parseCobolMust(t, dataMetadataSrc)
	var rec *Node
	for _, n := range m.Nodes {
		if n.Name == "WS-TABLES" {
			rec = n
		}
	}
	if rec == nil {
		t.Fatal("WS-TABLES not found")
	}
	f := findField(rec, "WS-VAR-TABLE")
	if f == nil {
		t.Fatal("WS-VAR-TABLE field not found")
	}
	if !strings.Contains(f.Tag, "occurs=100") {
		t.Errorf("WS-VAR-TABLE should record max occurs=100; got Tag=%q", f.Tag)
	}
	if !strings.Contains(f.Tag, "occurs_depending_on=WS-COUNT") {
		t.Errorf("WS-VAR-TABLE should record DEPENDING ON; got Tag=%q", f.Tag)
	}
}

func TestCOBOL_VALUE_Captured(t *testing.T) {
	m := parseCobolMust(t, dataMetadataSrc)
	var rec *Node
	for _, n := range m.Nodes {
		if n.Name == "WS-TABLES" {
			rec = n
		}
	}
	if rec == nil {
		t.Fatal("WS-TABLES not found")
	}
	f := findField(rec, "WS-COUNT")
	if f == nil {
		t.Fatal("WS-COUNT field not found")
	}
	if !strings.Contains(f.Tag, "value=ZEROS") {
		t.Errorf("WS-COUNT should have Tag value=ZEROS; got Tag=%q", f.Tag)
	}
	flag := findField(rec, "WS-FLAG")
	if flag == nil {
		t.Fatal("WS-FLAG field not found")
	}
	if !strings.Contains(flag.Tag, "value=") {
		t.Errorf("WS-FLAG should have Tag containing value=; got Tag=%q", flag.Tag)
	}
}

// ─── FILE-CONTROL / SELECT / ASSIGN / FD ──────────────────────

const fileCtrlSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. FILECTRL.

       ENVIRONMENT DIVISION.
       INPUT-OUTPUT SECTION.
       FILE-CONTROL.
           SELECT CUSTOMER-FILE ASSIGN TO DD-CUST
               ORGANIZATION IS INDEXED
               ACCESS MODE IS DYNAMIC
               RECORD KEY IS CUST-ID
               FILE STATUS IS WS-CUST-STATUS.
           SELECT REPORT-FILE ASSIGN TO DD-RPT
               ORGANIZATION IS SEQUENTIAL.

       DATA DIVISION.
       FILE SECTION.
       FD CUSTOMER-FILE.
       01 CUSTOMER-RECORD.
          05 CUST-ID   PIC 9(6).
          05 CUST-NAME PIC X(40).

       WORKING-STORAGE SECTION.
       01 WS-CUST-STATUS PIC XX VALUE '00'.

       PROCEDURE DIVISION.
       STOP RUN.
`

func TestCOBOL_FileControl_SelectEmitsImport(t *testing.T) {
	m := parseCobolMust(t, fileCtrlSrc)

	imports := map[string]*Node{}
	for _, n := range m.Nodes {
		if n.Kind == NodeImport {
			imports[n.Name] = n
		}
	}
	if _, ok := imports["CUSTOMER-FILE"]; !ok {
		t.Errorf("SELECT CUSTOMER-FILE should produce NodeImport; got imports=%v", keysOf(imports))
	}
	if _, ok := imports["REPORT-FILE"]; !ok {
		t.Errorf("SELECT REPORT-FILE should produce NodeImport; got imports=%v", keysOf(imports))
	}
}

func TestCOBOL_FileControl_AssignToCaptured(t *testing.T) {
	m := parseCobolMust(t, fileCtrlSrc)
	for _, n := range m.Nodes {
		if n.Kind == NodeImport && n.Name == "CUSTOMER-FILE" {
			if n.ImportPath != "DD-CUST" {
				t.Errorf("CUSTOMER-FILE ImportPath should be DD-CUST; got %q", n.ImportPath)
			}
			if !strings.Contains(n.RawSource, "organization=INDEXED") {
				t.Errorf("CUSTOMER-FILE RawSource should record organization=INDEXED; got %q", n.RawSource)
			}
			if !strings.Contains(n.RawSource, "access=DYNAMIC") {
				t.Errorf("CUSTOMER-FILE RawSource should record access=DYNAMIC; got %q", n.RawSource)
			}
			if !strings.Contains(n.RawSource, "record_key=CUST-ID") {
				t.Errorf("CUSTOMER-FILE RawSource should record record_key=CUST-ID; got %q", n.RawSource)
			}
			return
		}
	}
	t.Error("CUSTOMER-FILE NodeImport not found")
}

func TestCOBOL_FD_EmitsStruct(t *testing.T) {
	m := parseCobolMust(t, fileCtrlSrc)
	var fd *Node
	for _, n := range m.Nodes {
		if n.Kind == NodeStruct && n.Name == "CUSTOMER-FILE" && strings.Contains(n.RawSource, "fd=true") {
			fd = n
		}
	}
	if fd == nil {
		t.Errorf("FD CUSTOMER-FILE should produce NodeStruct with RawSource containing fd=true; got nodes: %v",
			nodeNames(m.Nodes))
	}
}

// ─── Expanded CICS verb surface ───────────────────────────────

const cicsExpandedSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. CICSWIDE.

       PROCEDURE DIVISION.
       MAIN.
           EXEC CICS WRITE FILE('CUST') FROM(WS-DATA) END-EXEC
           EXEC CICS REWRITE FILE('CUST') FROM(WS-DATA) END-EXEC
           EXEC CICS DELETE FILE('CUST') RIDFLD(WS-ID) END-EXEC
           EXEC CICS LINK PROGRAM('SUBPROG') END-EXEC
           EXEC CICS XCTL PROGRAM('NEXTPGM') END-EXEC
           EXEC CICS SEND MAP('SCREEN1') MAPSET('MS01') END-EXEC
           EXEC CICS RECEIVE MAP('SCREEN1') MAPSET('MS01') END-EXEC
           EXEC CICS SYNCPOINT END-EXEC
           EXEC CICS ABEND ABCODE('USR1') END-EXEC
           EXEC CICS ASSIGN APPLID(WS-APP) END-EXEC
           EXEC CICS RETURN END-EXEC.
`

func TestCOBOL_CICS_ExpandedVerbs(t *testing.T) {
	m := parseCobolMust(t, cicsExpandedSrc)
	var mainPara *Node
	for _, n := range m.Nodes {
		if n.Name == "MAIN" {
			mainPara = n
		}
	}
	if mainPara == nil {
		t.Fatal("MAIN not found")
	}
	want := []string{
		"CICS.WRITE", "CICS.REWRITE", "CICS.DELETE",
		"CICS.LINK", "CICS.XCTL",
		"CICS.SEND", "CICS.RECEIVE",
		"CICS.SYNCPOINT", "CICS.ABEND", "CICS.ASSIGN",
		"CICS.RETURN",
	}
	got := map[string]bool{}
	for _, c := range mainPara.Children {
		if c.Kind == NodeCall && strings.HasPrefix(c.Callee, "CICS.") {
			got[c.Callee] = true
		}
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing CICS verb %s; got CICS callees: %v", w, keysOfBool(got))
		}
	}
}

// ─── EXEC SQL block ───────────────────────────────────────────

const sqlSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. SQLTEST.

       PROCEDURE DIVISION.
       MAIN.
           EXEC SQL
               SELECT CUST_NAME INTO :WS-NAME
               FROM CUSTOMERS
               WHERE CUST_ID = :WS-ID
           END-EXEC.
`

func TestCOBOL_EXEC_SQL_ProducesSQLCall(t *testing.T) {
	m := parseCobolMust(t, sqlSrc)
	var mainPara *Node
	for _, n := range m.Nodes {
		if n.Name == "MAIN" {
			mainPara = n
		}
	}
	if mainPara == nil {
		t.Fatal("MAIN not found")
	}
	found := false
	for _, c := range mainPara.Children {
		if c.Kind == NodeCall && strings.HasPrefix(c.Callee, "SQL.") {
			found = true
		}
	}
	if !found {
		t.Errorf("EXEC SQL should produce NodeCall(SQL.*); children: %+v", mainPara.Children)
	}
}

// ─── 88-level condition names ─────────────────────────────────

const cond88Src = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. COND88.

       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-STATUS  PIC X.
          88 WS-OK     VALUE 'Y'.
          88 WS-FAIL   VALUE 'N'.

       PROCEDURE DIVISION.
       STOP RUN.
`

func TestCOBOL_Condition88_Captured(t *testing.T) {
	m := parseCobolMust(t, cond88Src)
	var status *Node
	for _, n := range m.Nodes {
		if n.Name == "WS-STATUS" {
			status = n
		}
	}
	if status == nil {
		t.Fatal("WS-STATUS not found")
	}
	var hasOK, hasFail bool
	for _, f := range status.Fields {
		if f.Name == "WS-OK" && strings.Contains(f.Tag, "level=88") {
			hasOK = true
		}
		if f.Name == "WS-FAIL" && strings.Contains(f.Tag, "level=88") {
			hasFail = true
		}
	}
	if !hasOK || !hasFail {
		t.Errorf("88-level conditions should appear as fields with level=88; got: %+v", status.Fields)
	}
}

// ─── Continuation lines ───────────────────────────────────────

func TestCOBOL_ContinuationLineMerging(t *testing.T) {
	// Fixed-format with '-' indicator continues prior literal.
	src := "      *>\n" +
		"       IDENTIFICATION DIVISION.\n" +
		"       PROGRAM-ID. CONTINUED.\n" +
		"       PROCEDURE DIVISION.\n" +
		"       MAIN.\n" +
		"           DISPLAY 'This is a long\n" +
		"      -    'message'.\n" +
		"           STOP RUN.\n"
	fe := &CobolFrontend{}
	m, err := fe.Parse("cont.cob", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.PackageName != "CONTINUED" {
		t.Errorf("PackageName = %q, want CONTINUED", m.PackageName)
	}
}

// ─── Helpers ──────────────────────────────────────────────────

func nodeKinds(ns []*Node) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, string(n.Kind)+":"+n.Name)
	}
	return out
}

func nodeNames(ns []*Node) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.Name)
	}
	return out
}

func keysOf(m map[string]*Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysOfBool(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// v23.41: data statements (SUBTRACT/MOVE/…) must emit a NodeAssign so they
// appear in the CFG (previously they produced no node and were invisible).
func TestCOBOL_DataStatementEmitsAssign_v2341(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. ARITH.
       PROCEDURE DIVISION.
       APPLY-DEBIT.
           SUBTRACT TXN-AMOUNT FROM CUST-BALANCE
           MOVE ZERO TO WS-FLAG.
`
	m := parseCobolMust(t, src)
	var para *Node
	for _, n := range m.Nodes {
		if n.Name == "APPLY-DEBIT" {
			para = n
		}
	}
	if para == nil {
		t.Fatal("APPLY-DEBIT not found")
	}
	var subtract, move *Node
	for _, c := range para.Children {
		if c.Kind == NodeAssign && c.Name == "SUBTRACT" {
			subtract = c
		}
		if c.Kind == NodeAssign && c.Name == "MOVE" {
			move = c
		}
	}
	if subtract == nil {
		t.Fatalf("SUBTRACT should emit a NodeAssign; children: %+v", para.Children)
	}
	if move == nil {
		t.Fatalf("MOVE should emit a NodeAssign; children: %+v", para.Children)
	}
	if subtract.RawSource == "" || subtract.Span.Start.Line == 0 {
		t.Errorf("SUBTRACT NodeAssign should carry RawSource + line, got %+v", subtract)
	}
}

// v23.41: EVALUATE keeps its selector, and each WHEN keeps its value, so the
// CFG can label the multi-way decision (was: bare "decision" with no condition).
func TestCOBOL_EvaluateKeepsSelectorAndWhenValues_v2341(t *testing.T) {
	m := parseCobolMust(t, evaluateSrc)
	var para *Node
	for _, n := range m.Nodes {
		if n.Name == "MAIN-PARA" {
			para = n
		}
	}
	if para == nil {
		t.Fatal("MAIN-PARA not found")
	}
	var eval *Node
	for _, c := range para.Children {
		if c.Kind == NodeBranch && c.Name == "EVALUATE" {
			eval = c
		}
	}
	if eval == nil {
		t.Fatal("EVALUATE node not found")
	}
	if eval.RawSource != "WS-CODE" {
		t.Errorf("EVALUATE selector: got %q want %q", eval.RawSource, "WS-CODE")
	}
	var vals []string
	for _, w := range eval.Children {
		if w.Kind == NodeBranch {
			vals = append(vals, w.RawSource)
		}
	}
	// expect "1", "2", "OTHER"
	want := map[string]bool{"1": true, "2": true, "OTHER": true}
	for _, v := range vals {
		if !want[v] {
			t.Errorf("unexpected WHEN value %q (all: %v)", v, vals)
		}
	}
	if len(vals) != 3 {
		t.Errorf("expected 3 WHEN values, got %d: %v", len(vals), vals)
	}
}

// v23.43: PERFORM ... UNTIL keeps its condition on the NodeLoop so the CFG
// loop-header decision shows the real test instead of a bare "decision".
func TestCOBOL_PerformUntilKeepsCondition_v2343(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. LOOPTEST.
       PROCEDURE DIVISION.
       PROCESS-ALL.
           PERFORM APPLY-FEES UNTIL WS-FEE-INDEX >= TXN-FEE-COUNT.
`
	m := parseCobolMust(t, src)
	var para *Node
	for _, n := range m.Nodes {
		if n.Name == "PROCESS-ALL" {
			para = n
		}
	}
	if para == nil {
		t.Fatal("PROCESS-ALL not found")
	}
	var loop *Node
	for _, c := range para.Children {
		if c.Kind == NodeLoop {
			loop = c
		}
	}
	if loop == nil {
		t.Fatalf("PERFORM UNTIL should emit a NodeLoop; children: %+v", para.Children)
	}
	if loop.RawSource == "" || !strings.Contains(loop.RawSource, "WS-FEE-INDEX") {
		t.Errorf("loop condition not captured: RawSource=%q", loop.RawSource)
	}
}

// v23.46: IF bodies must nest as children of the branch, not flat siblings.
// Before this fix the CFG built empty then-blocks and pushed real statements
// into the merge/exit (the "no End on branch" bug). Verify the nested shape.
func TestCOBOL_IfBodyNests_v2346(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. NESTTEST.
       PROCEDURE DIVISION.
       APPLY-DEBIT.
           SUBTRACT TXN-AMOUNT FROM CUST-BALANCE
           IF CUST-BALANCE < 0
               IF CUST-BALANCE < CUST-CREDIT-LIMIT
                   PERFORM ROUTE-TO-OVERDRAFT
               END-IF
           END-IF.
`
	m := parseCobolMust(t, src)
	var para *Node
	for _, n := range m.Nodes {
		if n.Name == "APPLY-DEBIT" {
			para = n
		}
	}
	if para == nil {
		t.Fatal("APPLY-DEBIT not found")
	}
	// paragraph should have: SUBTRACT (assign) + outer IF (branch). Not the
	// PERFORM as a flat sibling.
	var outerIf *Node
	var flatPerform bool
	for _, c := range para.Children {
		if c.Kind == NodeBranch {
			outerIf = c
		}
		if c.Kind == NodeCall && c.Callee == "ROUTE-TO-OVERDRAFT" {
			flatPerform = true
		}
	}
	if outerIf == nil {
		t.Fatal("outer IF branch not found in paragraph")
	}
	if flatPerform {
		t.Error("PERFORM ROUTE-TO-OVERDRAFT leaked as a flat sibling; should be nested in the inner IF")
	}
	// outer IF should contain the inner IF as a child
	var innerIf *Node
	for _, c := range outerIf.Children {
		if c.Kind == NodeBranch {
			innerIf = c
		}
	}
	if innerIf == nil {
		t.Fatalf("inner IF not nested in outer IF; outer children: %d", len(outerIf.Children))
	}
	// inner IF should contain the PERFORM
	found := false
	for _, c := range innerIf.Children {
		if c.Kind == NodeCall && c.Callee == "ROUTE-TO-OVERDRAFT" {
			found = true
		}
	}
	if !found {
		t.Error("PERFORM ROUTE-TO-OVERDRAFT not nested inside the inner IF")
	}
}

// v23.47: PERFORM <para> UNTIL <cond> must nest the paragraph call as the loop
// body (was: loop and call emitted as siblings → empty "until" diamond).
func TestCOBOL_PerformParaUntilNestsCall_v2347(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. LOOPBODY.
       PROCEDURE DIVISION.
       MAIN-PARA.
           PERFORM PROCESS-ALL-TRANSACTIONS UNTIL WS-DONE OR WS-ERROR.
`
	m := parseCobolMust(t, src)
	var para *Node
	for _, n := range m.Nodes {
		if n.Name == "MAIN-PARA" {
			para = n
		}
	}
	if para == nil {
		t.Fatal("MAIN-PARA not found")
	}
	var loop *Node
	for _, c := range para.Children {
		if c.Kind == NodeLoop {
			loop = c
		}
		if c.Kind == NodeCall && c.Callee == "PROCESS-ALL-TRANSACTIONS" {
			t.Error("performed paragraph leaked as a sibling; should be the loop body")
		}
	}
	if loop == nil {
		t.Fatal("PERFORM UNTIL did not produce a NodeLoop")
	}
	found := false
	for _, c := range loop.Children {
		if c.Kind == NodeCall && c.Callee == "PROCESS-ALL-TRANSACTIONS" {
			found = true
		}
	}
	if !found {
		t.Error("PROCESS-ALL-TRANSACTIONS not nested as the loop body")
	}
}

// v23.47: EXEC CICS statements inside IF/ELSE must nest into the branch, not
// collapse onto the paragraph (was: both arms' EXEC landed outside the IF).
func TestCOBOL_ExecInIfElseNests_v2347(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. CLEANUP.
       PROCEDURE DIVISION.
       CLEANUP-PARA.
           IF WS-ERROR
               EXEC CICS ABEND ABCODE('POST') END-EXEC
           ELSE
               EXEC CICS SYNCPOINT END-EXEC
           END-IF.
`
	m := parseCobolMust(t, src)
	var para *Node
	for _, n := range m.Nodes {
		if n.Name == "CLEANUP-PARA" {
			para = n
		}
	}
	if para == nil {
		t.Fatal("CLEANUP-PARA not found")
	}
	var br *Node
	for _, c := range para.Children {
		if c.Kind == NodeBranch {
			br = c
		}
		if c.Kind == NodeCall && (c.Callee == "CICS.ABEND" || c.Callee == "CICS.SYNCPOINT") {
			t.Errorf("EXEC %s leaked to the paragraph; should be inside the IF/ELSE", c.Callee)
		}
	}
	if br == nil {
		t.Fatal("IF branch not found")
	}
	thenHas, elseHas := false, false
	for _, c := range br.Children {
		if c.Kind == NodeCall && c.Callee == "CICS.ABEND" {
			thenHas = true
		}
	}
	for _, c := range br.Else {
		if c.Kind == NodeCall && c.Callee == "CICS.SYNCPOINT" {
			elseHas = true
		}
	}
	if !thenHas {
		t.Error("EXEC CICS ABEND not in the THEN body")
	}
	if !elseHas {
		t.Error("EXEC CICS SYNCPOINT not in the ELSE body")
	}
}
