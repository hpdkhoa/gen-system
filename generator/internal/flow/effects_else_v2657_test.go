// effects_else_v2657_test.go — v26.57 (D014): a call in an else (or catch)
// body must count toward the effects of its function, and a COBOL SECTION must
// get a CFG block for its ELSE body.
//
// inferNodeEffects walked n.Children only, and the Go, Java and COBOL
// frontends put the else and catch bodies in n.Else. A Go function with
// db.Exec in its else branch was tagged pure; the parser beliefs then said
// "no data change" and the enrich gate blocked a correct model claim.
// processCAGenNode, which builds the CFG of every COBOL SECTION, ignored
// n.Else, so the ELSE body had no block at all.
package flow

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

const goElseSrc = `package p

func Purge(ok bool, db DB) {
	if ok {
		x := 1
		_ = x
	} else {
		db.Exec("DELETE FROM t")
	}
}

type DB interface{ Exec(q string) }
`

func TestInferEffects_CallInElseBody(t *testing.T) {
	m, err := (&ast.GoFrontend{}).Parse("p.go", []byte(goElseSrc))
	if err != nil {
		t.Fatal(err)
	}
	efx := InferEffects(m, nil)
	id := symbols.SymbolID(m.FilePath, symbols.SymFunc, "Purge")
	got, ok := efx[id]
	if !ok {
		t.Fatalf("no effects for Purge; have %v", efx)
	}
	if !got.Effects[EffectDB] || got.Effects[EffectPure] {
		t.Fatalf("db.Exec in the else body must give db, got %v", got.Effects)
	}
}

const cobolSectionElseSrc = `
       IDENTIFICATION DIVISION.
       PROGRAM-ID. ELSETEST.
       PROCEDURE DIVISION.
       MAIN-SECTION SECTION.
           IF WS-A = 1
               CALL 'SUB-THEN'
           ELSE
               CALL 'SUB-ELSE'
           END-IF.
           STOP RUN.
`

func TestProcedureCFG_ElseBodyHasABlock(t *testing.T) {
	fe := ast.ForLanguage(ast.LangCOBOL)
	m, err := fe.Parse("s.cob", []byte(cobolSectionElseSrc))
	if err != nil {
		t.Fatal(err)
	}
	var sec *ast.Node
	for _, n := range m.Nodes {
		if n.Kind == ast.NodeProcedure && n.Name == "MAIN-SECTION" {
			sec = n
		}
	}
	if sec == nil {
		t.Fatal("SECTION MAIN-SECTION not parsed as a procedure")
	}
	cfg := buildProcedureCFG("sec", sec)
	var thenOK, elseOK bool
	for _, b := range cfg.Blocks {
		for _, s := range b.Stmts {
			if s.Kind == ast.NodeCall && s.Callee == "SUB-THEN" {
				thenOK = true
			}
			if s.Kind == ast.NodeCall && s.Callee == "SUB-ELSE" {
				elseOK = true
			}
		}
	}
	if !thenOK || !elseOK {
		t.Fatalf("then call in CFG: %v, else call in CFG: %v", thenOK, elseOK)
	}
}
