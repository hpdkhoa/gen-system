// cobol_format_v2670_test.go (v26.70): the tokenizer chose fixed or free format
// per line. A free format line whose first six columns were blank or digits was
// read as fixed format: cut at column 72, and dropped when column 7 held D, * or
// /. Found by diffing gen-system against cobc on CobolCraft (bench/compare/):
// "FUNCTION STORED-CHAR-LENGTH(...)" past column 72 became a call to
// STORED-CHAR-LE, and "PERFORM X(I, J) TIMES" lost TIMES and became a call.
package ast

import (
	"strings"
	"testing"
)

// A free format program: the division header starts in column 1.
var freeFormatSrc = "IDENTIFICATION DIVISION.\n" +
	"PROGRAM-ID. FREEFMT.\n" +
	"DATA DIVISION.\n" +
	"WORKING-STORAGE SECTION.\n" +
	"    01 WS-LEN PIC 9(4).\n" +
	"    01 WS-NAME PIC X(40).\n" +
	"PROCEDURE DIVISION.\n" +
	"MAIN-PARA.\n" +
	"             COMPUTE WS-LEN = WS-LEN + 10 + FUNCTION STORED-CHAR-LENGTH(WS-NAME) + FUNCTION LENGTH(WS-NAME)\n" +
	"      DISPLAY WS-LEN\n" +
	"             PERFORM WS-LEN TIMES\n" +
	"                 CALL \"After-The-Column-Seventy-Two-Mark-Of-Fixed-Format\" USING WS-NAME WS-LEN\n" +
	"             END-PERFORM\n" +
	"    STOP RUN.\n"

func TestCOBOL_FreeFormatLinesAreNotCutAtColumn72(t *testing.T) {
	m := parseCobolMust(t, freeFormatSrc)
	fn := cobolFunctions(m)["MAIN-PARA"]
	if fn == nil {
		t.Fatalf("MAIN-PARA not parsed; functions: %v", keys(cobolFunctions(m)))
	}
	for _, callee := range []string{"STORED-CHAR-LENGTH", "LENGTH", "AFTER-THE-COLUMN-SEVENTY-TWO-MARK-OF-FIXED-FORMAT"} {
		if !hasCallTo(fn, callee) {
			t.Errorf("call to %s is missing", callee)
		}
	}
	if hasCallTo(fn, "STORED-CHAR-LE") {
		t.Error("the intrinsic name was cut at column 72")
	}
	if hasCallTo(fn, "WS-LEN") {
		t.Error("PERFORM WS-LEN TIMES became a call")
	}
	// "      DISPLAY" has D in column 7; in free format that is a statement.
	lines := tokenizeCOBOL([]byte(freeFormatSrc))
	var sawDisplay, sawLevel bool
	for _, l := range lines {
		if strings.HasPrefix(l.upper, "DISPLAY WS-LEN") {
			sawDisplay = true
		}
		if strings.HasPrefix(l.upper, "01 WS-LEN") {
			sawLevel = true
		}
	}
	if !sawDisplay {
		t.Error("the DISPLAY line with D in column 7 was dropped as a debug line")
	}
	if !sawLevel {
		t.Error("the 01 level number was cut off a free format data line")
	}
}

// A classic fixed format program still loses its sequence numbers and the
// identification area after column 72.
var fixedFormatSrc = "000100 IDENTIFICATION DIVISION.                                         FIXEDFMT\n" +
	"000200 PROGRAM-ID. FIXEDFMT.                                              FIXEDFMT\n" +
	"000300 PROCEDURE DIVISION.                                                FIXEDFMT\n" +
	"000400 MAIN-PARA.                                                         FIXEDFMT\n" +
	"000500*    A COMMENT LINE                                                 FIXEDFMT\n" +
	"000600     CALL \"SUB-PROG\"                                                FIXEDFMT\n" +
	"000700     STOP RUN.                                                      FIXEDFMT\n"

func TestCOBOL_FixedFormatStillStripsSequenceAndIdentification(t *testing.T) {
	m := parseCobolMust(t, fixedFormatSrc)
	fn := cobolFunctions(m)["MAIN-PARA"]
	if fn == nil {
		t.Fatalf("MAIN-PARA not parsed; functions: %v", keys(cobolFunctions(m)))
	}
	if !hasCallTo(fn, "SUB-PROG") {
		t.Error("CALL SUB-PROG is missing")
	}
	for _, l := range tokenizeCOBOL([]byte(fixedFormatSrc)) {
		if strings.Contains(l.upper, "FIXEDFMT") && !strings.HasPrefix(l.upper, "PROGRAM-ID") {
			t.Errorf("the identification area survived: %q", l.text)
		}
		if strings.Contains(l.upper, "COMMENT") {
			t.Errorf("the comment line survived: %q", l.text)
		}
	}
}

const subscriptCountSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. SUBCOUNT.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       01 T.
          05 TAG-LEN PIC 9 OCCURS 9 INDEXED BY I.
       01 J PIC 9.
       PROCEDURE DIVISION.
       MAIN-PARA.
           PERFORM TAG-LEN(I, J) TIMES
               ADD 1 TO J
           END-PERFORM
           STOP RUN.
`

// v26.70: "PERFORM TAG-LEN(I, J) TIMES" read the subscripted count as a paragraph.
func TestCOBOL_SubscriptedLoopCountIsNotACall(t *testing.T) {
	m := parseCobolMust(t, subscriptCountSrc)
	fn := cobolFunctions(m)["MAIN-PARA"]
	if fn == nil {
		t.Fatalf("MAIN-PARA not parsed; functions: %v", keys(cobolFunctions(m)))
	}
	if hasCallTo(fn, "TAG-LEN(I,") {
		t.Error("the subscripted loop count became a call")
	}
}
