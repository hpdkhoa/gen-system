// effects_write_v2658_test.go — v26.58 (D100): EffectDB is set for any
// database access, so the verify pass could not tell a SELECT from a write and
// reported every Get and List of a PG store as a read that writes. DBWrite now
// marks the writes alone.
package flow

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

const goWriteSrc = `package p

type DB interface {
	Query(q string) error
	QueryRow(q string) error
	Exec(q string) error
}

func Read(db DB) { db.Query("SELECT 1"); db.QueryRow("SELECT 1") }

func Write(db DB) { db.Exec("UPDATE t SET a=1") }
`

func TestInferEffects_DBWriteOnlyForWrites(t *testing.T) {
	m, err := (&ast.GoFrontend{}).Parse("p.go", []byte(goWriteSrc))
	if err != nil {
		t.Fatal(err)
	}
	efx := InferEffects(m, nil)
	for name, wantWrite := range map[string]bool{"Read": false, "Write": true} {
		got, ok := efx[symbols.SymbolID(m.FilePath, symbols.SymFunc, name)]
		if !ok {
			t.Fatalf("no effects for %s", name)
		}
		if !got.Effects[EffectDB] {
			t.Errorf("%s: want the db effect, got %v", name, got.Effects)
		}
		if got.DBWrite != wantWrite {
			t.Errorf("%s: DBWrite = %v, want %v", name, got.DBWrite, wantWrite)
		}
	}
}
