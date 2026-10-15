// cobol_programs_v2669_test.go (v26.69): a COBOL file may hold several programs.
// Only the first became a module symbol, statements before the first paragraph
// had no caller, and a PERFORM could resolve to a paragraph of another file.
// Found on CobolCraft, where 97 files hold more than one program.
package symbols

import (
	"strconv"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

const twoProgramsSrc = `IDENTIFICATION DIVISION.
PROGRAM-ID. World-CheckBounds.
DATA DIVISION.
WORKING-STORAGE SECTION.
01 WS-OK PIC 9.
PROCEDURE DIVISION.
    CALL "World-GetBlock" USING WS-OK
    PERFORM Init
    GOBACK.
Init.
    MOVE 1 TO WS-OK.
END PROGRAM World-CheckBounds.

IDENTIFICATION DIVISION.
PROGRAM-ID. World-GetBlock.
DATA DIVISION.
WORKING-STORAGE SECTION.
01 WS-ID PIC 9.
PROCEDURE DIVISION.
    PERFORM Init
    GOBACK.
Init.
    MOVE 2 TO WS-ID.
END PROGRAM World-GetBlock.
`

const otherFileSrc = `IDENTIFICATION DIVISION.
PROGRAM-ID. Other.
PROCEDURE DIVISION.
AAA-MAIN.
    PERFORM Init
    STOP RUN.
`

const thirdFileSrc = `IDENTIFICATION DIVISION.
PROGRAM-ID. Third.
PROCEDURE DIVISION.
MAIN-PARA.
    DISPLAY 'X'.
INIT.
    DISPLAY 'Y'.
`

func buildCobol(t *testing.T, files map[string]string) *Index {
	t.Helper()
	var mods []*ast.Module
	for path, src := range files {
		m, err := ast.ForLanguage(ast.LangCOBOL).Parse(path, []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		mods = append(mods, m)
	}
	idx := BuildFromModules(mods)
	idx.ResolveAll()
	return idx
}

// edgeTargets maps "caller ID L<line>" to the callee ID ("" when unresolved).
func edgeTargets(idx *Index) map[string]string {
	out := map[string]string{}
	for _, e := range idx.AllCallEdges() {
		out[e.CallerID+" L"+strconv.Itoa(e.Span.Start.Line)] = e.CalleeID
	}
	return out
}

func TestCobolEveryProgramInAFileIsAModule(t *testing.T) {
	idx := buildCobol(t, map[string]string{"/cc/blocks.cob": twoProgramsSrc})
	for _, name := range []string{"WORLD-CHECKBOUNDS", "WORLD-GETBLOCK"} {
		s, ok := idx.ByID(SymbolID("/cc/blocks.cob", SymModule, name))
		if !ok {
			t.Fatalf("no module symbol for %s", name)
		}
		if s.Span.Start.Line == 0 || s.Span.End.Line <= s.Span.Start.Line {
			t.Errorf("%s span %+v does not cover the program", name, s.Span)
		}
	}
	// Both programs have a paragraph Init; each keeps its own symbol.
	for _, id := range []string{
		SymbolID("/cc/blocks.cob", SymFunc, "WORLD-CHECKBOUNDS.INIT"),
		SymbolID("/cc/blocks.cob", SymFunc, "WORLD-GETBLOCK.INIT"),
	} {
		if _, ok := idx.ByID(id); !ok {
			t.Errorf("missing paragraph symbol %s", id)
		}
	}
}

func TestCobolProgramBodyCallsHaveACallerAndResolveLocally(t *testing.T) {
	idx := buildCobol(t, map[string]string{
		"/cc/blocks.cob": twoProgramsSrc,
		"/cc/other.cob":  otherFileSrc,
	})
	got := edgeTargets(idx)
	first := SymbolID("/cc/blocks.cob", SymModule, "WORLD-CHECKBOUNDS")
	second := SymbolID("/cc/blocks.cob", SymModule, "WORLD-GETBLOCK")
	want := map[string]string{
		// CALL to the second program of the same file: a module, now internal.
		first + " L7": second,
		// PERFORM Init from each program body: its own program's Init.
		first + " L8":   SymbolID("/cc/blocks.cob", SymFunc, "WORLD-CHECKBOUNDS.INIT"),
		second + " L20": SymbolID("/cc/blocks.cob", SymFunc, "WORLD-GETBLOCK.INIT"),
		// PERFORM Init in a file with no Init paragraph: never another file's.
		SymbolID("/cc/other.cob", SymFunc, "AAA-MAIN") + " L5": "",
	}
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("no edge %s; edges: %v", k, got)
			continue
		}
		if g != w {
			t.Errorf("edge %s resolved to %q, want %q", k, g, w)
		}
	}
}

func TestCobolSingleProgramIDsDoNotChange(t *testing.T) {
	idx := buildCobol(t, map[string]string{"/cc/third.cob": thirdFileSrc})
	for _, id := range []string{
		SymbolID("/cc/third.cob", SymFunc, "MAIN-PARA"),
		SymbolID("/cc/third.cob", SymFunc, "INIT"),
		SymbolID("/cc/third.cob", SymModule, "THIRD"),
	} {
		s, ok := idx.ByID(id)
		if !ok {
			t.Errorf("missing %s", id)
			continue
		}
		if s.Program != "" || strings.Contains(s.QualName, ".") {
			t.Errorf("%s got program %q, qualified name %q", id, s.Program, s.QualName)
		}
	}
}
