// cobol_nested_when_v2669_test.go: v26.69, statements in an IF nested under the
// ELSE of an IF under WHEN OTHER were lost. Found by the CobolCraft baseline
// (src/packets/serverbound/play/player-action.cob, StartedDigging).
package ast

import "testing"

const nestedWhenSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. NESTWHEN.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 MODE-X PIC 9.
       01 CHECK-X PIC 9.
       01 ID-X PIC 9.
       01 HARD-X PIC 9.
       PROCEDURE DIVISION.
       STARTED-DIGGING.
           EVALUATE TRUE
               WHEN MODE-X = 1
                   PERFORM BREAK-BLOCK
               WHEN OTHER
                   CALL "CHECK-BOUNDS" USING CHECK-X
                   IF CHECK-X = 0
                       CALL "GET-BLOCK" USING ID-X
                       IF ID-X <= 0
                           PERFORM START-BREAKING
                       ELSE
                           CALL "GET-HARDNESS" USING HARD-X
                           IF HARD-X > 0
                               PERFORM START-AGAIN
                           ELSE
                               PERFORM BREAK-AGAIN
                           END-IF
                       END-IF
                   END-IF
                   CONTINUE
           END-EVALUATE
           CALL "ACK" USING ID-X
           .
       START-BREAKING.
           DISPLAY 'S'.
       START-AGAIN.
           DISPLAY 'A'.
       BREAK-BLOCK.
           DISPLAY 'B'.
       BREAK-AGAIN.
           DISPLAY 'C'.
`

func TestCOBOL_NestedIfUnderWhenOtherKeepsEveryCall(t *testing.T) {
	m := parseCobolMust(t, nestedWhenSrc)
	fn := cobolFunctions(m)["STARTED-DIGGING"]
	if fn == nil {
		t.Fatalf("STARTED-DIGGING not parsed; functions: %v", keys(cobolFunctions(m)))
	}
	for _, callee := range []string{"BREAK-BLOCK", "CHECK-BOUNDS", "GET-BLOCK", "START-BREAKING",
		"GET-HARDNESS", "START-AGAIN", "BREAK-AGAIN", "ACK"} {
		if !hasCallTo(fn, callee) {
			t.Errorf("call to %s is missing from STARTED-DIGGING", callee)
		}
	}
}
