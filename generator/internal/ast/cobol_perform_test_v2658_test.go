// cobol_perform_test_v2658_test.go (v26.58, F6): "PERFORM WITH TEST AFTER
// UNTIL C" took WITH as the name of the performed paragraph. The parser made
// a loop around a call to WITH, pushed no loop scope, and left the inline body
// outside the loop, so END-PERFORM closed the wrong scope. WITH TEST BEFORE or
// WITH TEST AFTER (WITH is optional) is a phrase of the loop, not a paragraph.
package ast

import "testing"

func TestCOBOL_PerformWithTestInlineLoop(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. WITHTEST.
       PROCEDURE DIVISION.
       MAIN-PARA.
           PERFORM WITH TEST AFTER UNTIL WS-EOF = 'Y'
               CALL 'SUB-A'
           END-PERFORM.
           IF A = 1
               PERFORM TEST BEFORE UNTIL X = 1
                   MOVE 1 TO X
               END-PERFORM
               MOVE 2 TO Y
           END-IF
           PERFORM P-ONE WITH TEST AFTER UNTIL Y = 2
           PERFORM WITH TEST BEFORE VARYING I FROM 1 BY 1 UNTIL I > 3
               CALL 'SUB-V'
           END-PERFORM
           STOP RUN.
`
	checkTree(t, cobolParaTree(t, src, "MAIN-PARA"), `
MAIN-PARA
  loop until WS-EOF = 'Y'
    call SUB-A
  IF A = 1
    loop until X = 1
      assign MOVE 1 TO X
    assign MOVE 2 TO Y
  loop until Y = 2
    call P-ONE
  loop varying I FROM 1 BY 1 UNTIL I > 3
    call SUB-V
  return
`)
}
