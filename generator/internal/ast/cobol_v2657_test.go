// cobol_v2657_test.go — v26.57: two COBOL procedure-division defects.
//
// D008: a line holding only a scope terminator such as "END-PERFORM." or the
// verb "CONTINUE." looked like a paragraph header (one token ending in "."),
// so the parser closed the paragraph and opened a function named END-PERFORM
// or CONTINUE; every later statement landed in that false function.
//
// D009: a one-line "EXEC CICS ... END-EXEC" under WHEN made parseEvaluate scan
// for END-EXEC on the FOLLOWING lines, so it swallowed everything up to the
// next END-EXEC: the rest of the EVALUATE and whole paragraphs after it.
package ast

import "testing"

func cobolFunctions(m *Module) map[string]*Node {
	out := map[string]*Node{}
	for _, n := range m.Nodes {
		if n.Kind == NodeFunction || n.Kind == NodeProcedure {
			out[n.Name] = n
		}
	}
	return out
}

func hasCallTo(n *Node, callee string) bool {
	if n == nil {
		return false
	}
	if n.Kind == NodeCall && n.Callee == callee {
		return true
	}
	for _, c := range n.Children {
		if hasCallTo(c, callee) {
			return true
		}
	}
	for _, c := range n.Else {
		if hasCallTo(c, callee) {
			return true
		}
	}
	return false
}

const scopeTerminatorSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. TERMTEST.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-EOF PIC X VALUE 'N'.
       PROCEDURE DIVISION.
       MAIN-PARA.
           PERFORM UNTIL WS-EOF = 'Y'
               MOVE 'Y' TO WS-EOF
           END-PERFORM.
           CALL 'SUB-A'.
           CONTINUE.
           CALL 'SUB-B'.
           STOP RUN.
       END-OF-JOB-PARA.
           DISPLAY 'DONE'.
`

func TestCOBOL_ScopeTerminatorIsNotParagraph(t *testing.T) {
	m := parseCobolMust(t, scopeTerminatorSrc)
	fns := cobolFunctions(m)
	for _, bad := range []string{"END-PERFORM", "CONTINUE"} {
		if _, ok := fns[bad]; ok {
			t.Errorf("%s. became a paragraph; functions: %v", bad, keys(fns))
		}
	}
	for _, callee := range []string{"SUB-A", "SUB-B"} {
		if !hasCallTo(fns["MAIN-PARA"], callee) {
			t.Errorf("CALL %s is not in MAIN-PARA", callee)
		}
	}
	// A real paragraph whose name starts with END- must stay a paragraph.
	if _, ok := fns["END-OF-JOB-PARA"]; !ok {
		t.Errorf("paragraph END-OF-JOB-PARA lost; functions: %v", keys(fns))
	}
}

const evaluateCICSSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. EVALCICS.
       PROCEDURE DIVISION.
       MAIN-PARA.
           EVALUATE TRUE
               WHEN WS-A = 1
                   EXEC CICS RETURN END-EXEC
               WHEN OTHER
                   CALL 'SUB-OTHER'
           END-EVALUATE.
       NEXT-PARA.
           CALL 'SUB-NEXT'.
           EXEC CICS
               SEND MAP('M1')
           END-EXEC.
`

func TestCOBOL_OneLineCICSUnderWhen(t *testing.T) {
	m := parseCobolMust(t, evaluateCICSSrc)
	fns := cobolFunctions(m)
	main := fns["MAIN-PARA"]
	if !hasCallTo(main, "CICS.RETURN") {
		t.Errorf("CICS.RETURN missing under the first WHEN")
	}
	if !hasCallTo(main, "SUB-OTHER") {
		t.Errorf("WHEN OTHER body was swallowed: CALL SUB-OTHER missing")
	}
	next, ok := fns["NEXT-PARA"]
	if !ok {
		t.Fatalf("paragraph NEXT-PARA was swallowed; functions: %v", keys(fns))
	}
	if !hasCallTo(next, "SUB-NEXT") {
		t.Errorf("CALL SUB-NEXT missing from NEXT-PARA")
	}
}

func keys(m map[string]*Node) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
