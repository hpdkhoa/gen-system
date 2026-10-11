// cobol_else_v2658_test.go (v26.58, F5): in a multi line IF, a statement on
// the same line as ELSE ("ELSE CALL 'SUB-ELSE'") left no node. The ELSE
// handler switched the IF to its else arm and consumed the whole line, so the
// text after ELSE was never parsed. An END-IF on that line did not close the
// IF either.
package ast

import "testing"

func TestCOBOL_StatementOnElseLine(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. ELSELINE.
       PROCEDURE DIVISION.
       MAIN-PARA.
           IF WS-A = 1
               CALL 'SUB-THEN'
           ELSE CALL 'SUB-ELSE'
           END-IF.
           IF WS-B = 2
               MOVE 1 TO X
           ELSE MOVE 2 TO X END-IF
           CALL 'SUB-AFTER'
           IF WS-C = 3
               MOVE 3 TO X
           ELSE IF WS-D = 4 MOVE 4 TO X
           END-IF.
           STOP RUN.
`
	checkTree(t, cobolParaTree(t, src, "MAIN-PARA"), `
MAIN-PARA
  IF WS-A = 1
    call SUB-THEN
   ELSE
    call SUB-ELSE
  IF WS-B = 2
    assign MOVE 1 TO X
   ELSE
    assign MOVE 2 TO X
  call SUB-AFTER
  IF WS-C = 3
    assign MOVE 3 TO X
   ELSE
    IF WS-D = 4
      assign MOVE 4 TO X
  return
`)
}
