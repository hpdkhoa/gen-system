// cobol_clause_v2658_test.go — v26.58: data clause extractor defects.
//
// D065: extractPIC, extractOccurs, extractRedefines and extractValue searched
// the clause text with strings.Index, which also matched a keyword inside the
// field name. WS-VALUE PIC X got value=PIC, WS-PIC PIC X(10) got type "?".
//
// D066: extractOccurs kept the largest number up to TIMES, and TIMES is
// optional, so "OCCURS 10 PIC 9(3) VALUE 100" reported occurs=100.
package ast

import "testing"

func cobolRecordFields(t *testing.T, src, rec string) map[string]Field {
	t.Helper()
	m := parseCobolMust(t, src)
	for _, n := range m.Nodes {
		if n.Kind == NodeStruct && n.Name == rec {
			out := map[string]Field{}
			for _, f := range n.Fields {
				out[f.Name] = f
			}
			return out
		}
	}
	t.Fatalf("record %s not found; nodes: %v", rec, nodeNames(m.Nodes))
	return nil
}

func TestCOBOL_ClauseKeywordInsideFieldName(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. NAMES.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-REC.
          05 WS-VALUE           PIC X.
          05 WS-REDEFINES-FLAG  PIC X.
          05 WS-OCCURS-CNT      PIC 9 VALUE 5.
          05 WS-PIC             PIC X(10).
          05 WS-PICTURE-CODE    PICTURE IS 9(4).
       PROCEDURE DIVISION.
           STOP RUN.
`
	fields := cobolRecordFields(t, src, "WS-REC")
	want := map[string][2]string{ // name -> {type, tag}
		"WS-VALUE":          {"X", "level=05"},
		"WS-REDEFINES-FLAG": {"X", "level=05"},
		"WS-OCCURS-CNT":     {"9", "level=05;value=5"},
		"WS-PIC":            {"X(10)", "level=05"},
		"WS-PICTURE-CODE":   {"9(4)", "level=05"},
	}
	for name, w := range want {
		f, ok := fields[name]
		if !ok {
			t.Errorf("field %s missing", name)
			continue
		}
		if f.Type.Name != w[0] || f.Tag != w[1] {
			t.Errorf("%s: type %q tag %q, want type %q tag %q", name, f.Type.Name, f.Tag, w[0], w[1])
		}
	}
}

// D066: the OCCURS count is the number after OCCURS (or after TO), not the
// largest number anywhere later in the clause.
func TestCOBOL_OccursWithoutTimesKeepsCount(t *testing.T) {
	src := `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. OCC.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 WS-REC.
          05 WS-TBL     OCCURS 10 PIC 9(3) VALUE 100.
          05 WS-RANGE   OCCURS 1 TO 50 DEPENDING ON WS-N PIC X VALUE 200.
          05 WS-TIMES   OCCURS 7 TIMES PIC 9 VALUE 300.
       PROCEDURE DIVISION.
           STOP RUN.
`
	fields := cobolRecordFields(t, src, "WS-REC")
	want := map[string]string{
		"WS-TBL":   "level=05;occurs=10;value=100",
		"WS-RANGE": "level=05;occurs=50;occurs_depending_on=WS-N;value=200",
		"WS-TIMES": "level=05;occurs=7;value=300",
	}
	for name, tag := range want {
		if got := fields[name].Tag; got != tag {
			t.Errorf("%s: tag %q, want %q", name, got, tag)
		}
	}
}
