// else_edges_v2669_test.go (v26.69): extractCallEdges walked Children only, so
// a call made only in an else arm never became a call edge. The CobolCraft
// baseline lost two PERFORMs this way (player-action.cob, StartedDigging).
package symbols

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

const cobolElseSrc = `       IDENTIFICATION DIVISION.
       PROGRAM-ID. DIG.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 HARD-X PIC 9.
       PROCEDURE DIVISION.
       MAIN-PARA.
           IF HARD-X > 0
               PERFORM START-BREAKING
           ELSE
               IF HARD-X = 0
                   PERFORM START-AGAIN
               ELSE
                   PERFORM BREAK-BLOCK
               END-IF
           END-IF
           STOP RUN.
       START-BREAKING.
           DISPLAY 'S'.
       START-AGAIN.
           DISPLAY 'A'.
       BREAK-BLOCK.
           DISPLAY 'B'.
`

const goElseSrc = `package p

func run(ok bool) {
	if ok {
		first()
	} else if !ok {
		second()
	} else {
		third()
	}
}

func first()  {}
func second() {}
func third()  {}
`

func callees(m *ast.Module, caller string) map[string]bool {
	idx := NewIndex()
	idx.AddModule(m)
	out := map[string]bool{}
	for _, e := range idx.AllCallEdges() {
		if s, ok := idx.ByID(e.CallerID); ok && s.Name == caller {
			out[e.CalleeName] = true
		}
	}
	return out
}

func TestCallsInElseArmsAreEdges(t *testing.T) {
	cob, err := ast.ForLanguage(ast.LangCOBOL).Parse("/cbl/dig.cbl", []byte(cobolElseSrc))
	if err != nil {
		t.Fatal(err)
	}
	got := callees(cob, "MAIN-PARA")
	for _, want := range []string{"START-BREAKING", "START-AGAIN", "BREAK-BLOCK"} {
		if !got[want] {
			t.Errorf("COBOL: no edge MAIN-PARA -> %s; edges: %v", want, got)
		}
	}

	goMod, err := (&ast.GoFrontend{}).Parse("/app/p.go", []byte(goElseSrc))
	if err != nil {
		t.Fatal(err)
	}
	got = callees(goMod, "run")
	for _, want := range []string{"first", "second", "third"} {
		if !got[want] {
			t.Errorf("Go: no edge run -> %s; edges: %v", want, got)
		}
	}
}
