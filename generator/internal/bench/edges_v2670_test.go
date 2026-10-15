// edges_v2670_test.go (v26.70): the edge export names the caller's program and
// paragraph, so bench/compare/ can check caller attribution against cobc.
package bench

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const edgesSrc = `IDENTIFICATION DIVISION.
PROGRAM-ID. First-Prog.
PROCEDURE DIVISION.
    CALL "Second-Prog"
    PERFORM Init
    GOBACK.
Init.
    DISPLAY 'X'.
END PROGRAM First-Prog.

IDENTIFICATION DIVISION.
PROGRAM-ID. Second-Prog.
PROCEDURE DIVISION.
    GOBACK.
END PROGRAM Second-Prog.
`

func TestCallEdgesExport(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "two.cob"), []byte(edgesSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	edges := CallEdges(root, nil)
	byLine := map[int]EdgeRecord{}
	for _, e := range edges {
		byLine[e.Line] = e
	}
	call, ok := byLine[4]
	if !ok {
		t.Fatalf("no edge on line 4; edges: %+v", edges)
	}
	if call.File != "two.cob" || call.Callee != "SECOND-PROG" || call.CallerProgram != "FIRST-PROG" ||
		call.CallerParagraph != "" || call.Class != "internal" || call.ResolvedID != "two.cob#module:SECOND-PROG" {
		t.Errorf("CALL edge = %+v", call)
	}
	perform, ok := byLine[5]
	if !ok {
		t.Fatalf("no edge on line 5; edges: %+v", edges)
	}
	if perform.Callee != "INIT" || perform.CallerProgram != "FIRST-PROG" || perform.ResolvedID != "two.cob#func:FIRST-PROG.INIT" {
		t.Errorf("PERFORM edge = %+v", perform)
	}

	var buf bytes.Buffer
	if err := WriteEdgesJSONL(&buf, edges); err != nil {
		t.Fatal(err)
	}
	var first EdgeRecord
	if err := json.Unmarshal(bytes.SplitN(buf.Bytes(), []byte("\n"), 2)[0], &first); err != nil {
		t.Fatalf("first line is not JSON: %v", err)
	}
}
