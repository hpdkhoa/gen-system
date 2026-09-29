// cobol_scope_v2658_test.go — v26.58: procedure division scope defects.
//
// D061: an EVALUATE inside an open IF (or inline PERFORM) was attached to the
// paragraph, so it became a sibling after the IF instead of a child of it.
//
// D062: a period style IF got the wrong scope. On "IF A = 1 MOVE 1 TO X." the
// statement became part of the condition and the next sentence became the
// body. An IF line without a period stayed open until the paragraph ended,
// although COBOL closes every open IF at the first period.
//
// D063: "PERFORM 5 TIMES" became a loop around a call to a paragraph named
// "5" and pushed no loop scope, so the body landed outside the loop and the
// END-PERFORM closed the enclosing IF instead.
//
// D064: parseEvaluate ended at the first END-EVALUATE, even a nested one, and
// kept only CALL, PERFORM, EXEC CICS and STOP under a WHEN. MOVE left no node,
// and the later WHEN bodies landed on the paragraph without their WHEN.
package ast

import (
	"fmt"
	"strings"
	"testing"
)

// cobolTree renders a node tree as one line per node, indented by depth, so a
// test can compare the whole shape. Else arms print under an "ELSE" marker.
func cobolTree(n *Node) string {
	var b strings.Builder
	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		pad := strings.Repeat("  ", depth)
		switch n.Kind {
		case NodeCall:
			fmt.Fprintf(&b, "%scall %s\n", pad, n.Callee)
		case NodeAssign, NodeLoop:
			fmt.Fprintf(&b, "%s%s %s\n", pad, n.Kind, n.RawSource)
		case NodeReturn:
			fmt.Fprintf(&b, "%sreturn\n", pad)
		default:
			fmt.Fprintf(&b, "%s%s\n", pad, strings.TrimSpace(n.Name+" "+n.RawSource))
		}
		for _, c := range n.Children {
			walk(c, depth+1)
		}
		if len(n.Else) > 0 {
			fmt.Fprintf(&b, "%s ELSE\n", pad)
			for _, c := range n.Else {
				walk(c, depth+1)
			}
		}
	}
	walk(n, 0)
	return b.String()
}

func cobolParaTree(t *testing.T, src, para string) string {
	t.Helper()
	m := parseCobolMust(t, src)
	fns := cobolFunctions(m)
	p, ok := fns[para]
	if !ok {
		t.Fatalf("paragraph %s not found; functions: %v", para, keys(fns))
	}
	return cobolTree(p)
}

func checkTree(t *testing.T, got, want string) {
	t.Helper()
	want = strings.TrimLeft(want, "\n")
	if got != want {
		t.Errorf("tree mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestCOBOL_EvaluateInsideIfNests(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. EVIF.
       PROCEDURE DIVISION.
       MAIN-PARA.
           IF A = 1
               EVALUATE TRUE
                   WHEN B = 2
                       CALL 'SUB-B'
               END-EVALUATE
               MOVE 1 TO C
           END-IF
           STOP RUN.
`
	checkTree(t, cobolParaTree(t, src, "MAIN-PARA"), `
MAIN-PARA
  IF A = 1
    EVALUATE TRUE
      WHEN B = 2
        call SUB-B
    assign MOVE 1 TO C
  return
`)
}

func TestCOBOL_EvaluateInsideInlinePerformNests(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. EVLOOP.
       PROCEDURE DIVISION.
       MAIN-PARA.
           PERFORM UNTIL WS-EOF = 'Y'
               EVALUATE TRUE
                   WHEN B = 2
                       CALL 'SUB-B'
               END-EVALUATE
           END-PERFORM
           STOP RUN.
`
	checkTree(t, cobolParaTree(t, src, "MAIN-PARA"), `
MAIN-PARA
  loop until WS-EOF = 'Y'
    EVALUATE TRUE
      WHEN B = 2
        call SUB-B
  return
`)
}

// D062 case 1: a one-line IF carries its statement on the IF line.
func TestCOBOL_OneLineIfSplitsConditionAndStatement(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. ONELINE.
       PROCEDURE DIVISION.
       MAIN-PARA.
           IF A = 1 MOVE 1 TO X.
           MOVE 2 TO Y.
           IF B = 2 THEN MOVE 3 TO X ELSE MOVE 4 TO X END-IF
           IF C = 'MOVE IT' DISPLAY 'HI'.
           STOP RUN.
`
	checkTree(t, cobolParaTree(t, src, "MAIN-PARA"), `
MAIN-PARA
  IF A = 1
    assign MOVE 1 TO X.
  assign MOVE 2 TO Y.
  IF B = 2
    assign MOVE 3 TO X
   ELSE
    assign MOVE 4 TO X
  IF C = 'MOVE IT'
    assign DISPLAY 'HI'.
  return
`)
}

// D062 case 2: a period style IF over several lines ends at the first period.
func TestCOBOL_PeriodStyleIfEndsAtFirstPeriod(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. MULTILINE.
       PROCEDURE DIVISION.
       MAIN-PARA.
           IF A = 1,
               MOVE 1 TO X,
               MOVE 2 TO Y.
           MOVE 3 TO Z.
           PERFORM P-ONE.
           IF B = 2
               IF C = 3
                   MOVE 4 TO X
               END-IF.
           MOVE 5 TO Y.
           IF D = 4
               PERFORM P-TWO UNTIL E = 5.
           MOVE 6 TO Y.
           STOP RUN.
`
	checkTree(t, cobolParaTree(t, src, "MAIN-PARA"), `
MAIN-PARA
  IF A = 1
    assign MOVE 1 TO X,
    assign MOVE 2 TO Y.
  assign MOVE 3 TO Z.
  call P-ONE
  IF B = 2
    IF C = 3
      assign MOVE 4 TO X
  assign MOVE 5 TO Y.
  IF D = 4
    loop until E = 5
      call P-TWO
  assign MOVE 6 TO Y.
  return
`)
}

// D063: PERFORM n TIMES is an inline loop, not a call to the paragraph "n".
func TestCOBOL_PerformTimesInlineLoop(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. TIMESLOOP.
       PROCEDURE DIVISION.
       MAIN-PARA.
           IF A = 1
               PERFORM 5 TIMES
                   MOVE 1 TO X
               END-PERFORM
               MOVE 2 TO Y
           END-IF
           PERFORM WS-N TIMES
               CALL 'SUB-N'
           END-PERFORM
           PERFORM P-ONE 3 TIMES
           STOP RUN.
`
	checkTree(t, cobolParaTree(t, src, "MAIN-PARA"), `
MAIN-PARA
  IF A = 1
    loop 5 times
      assign MOVE 1 TO X
    assign MOVE 2 TO Y
  loop WS-N times
    call SUB-N
  loop 3 times
    call P-ONE
  return
`)
}

// D064: statements under WHEN are parsed like a paragraph body, and a nested
// EVALUATE ends at its own END-EVALUATE.
func TestCOBOL_EvaluateWhenBodiesAndNesting(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. EVNEST.
       PROCEDURE DIVISION.
       MAIN-PARA.
           EVALUATE TRUE
               WHEN A = 1
                   MOVE 1 TO X
                   EVALUATE B
                       WHEN 1
                           CALL 'SUB-B1'
                       WHEN OTHER
                           CALL 'SUB-B2'
                   END-EVALUATE
               WHEN A = 2
                   MOVE 2 TO X
                   IF C = 3
                       PERFORM P-C
                   END-IF
               WHEN OTHER
                   EXEC SQL COMMIT END-EXEC
           END-EVALUATE
           STOP RUN.
       NEXT-PARA.
           EVALUATE TRUE
               WHEN D = 4
                   MOVE 4 TO X
       LAST-PARA.
           MOVE 5 TO X.
`
	checkTree(t, cobolParaTree(t, src, "MAIN-PARA"), `
MAIN-PARA
  EVALUATE TRUE
    WHEN A = 1
      assign MOVE 1 TO X
      EVALUATE B
        WHEN 1
          call SUB-B1
        WHEN_OTHER OTHER
          call SUB-B2
    WHEN A = 2
      assign MOVE 2 TO X
      IF C = 3
        call P-C
    WHEN_OTHER OTHER
      call SQL.COMMIT
  return
`)
	// An EVALUATE left open ends with its paragraph.
	checkTree(t, cobolParaTree(t, src, "NEXT-PARA"), `
NEXT-PARA
  EVALUATE TRUE
    WHEN D = 4
      assign MOVE 4 TO X
`)
	checkTree(t, cobolParaTree(t, src, "LAST-PARA"), `
LAST-PARA
  assign MOVE 5 TO X.
`)
}
