// verify_effect_v2658_test.go (v26.58).
//
// D100: the effect check reported a DB-WRITE finding for every read-named
// method that touches the database at all. flow sets EffectDB for any callee
// that matches "db." or "Query", so OrderPGStore.Get and List, which only run
// SELECT, halted a stop-mode run on correct code.
package agent

import (
	"os"
	"path/filepath"
	"testing"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
)

func TestVerifyEffect_ReadsThatOnlySelectAreClean(t *testing.T) {
	prev := LoadOpts
	LoadOpts = agentctx.LoadContextOpts{}
	t.Cleanup(func() { LoadOpts = prev })

	root := t.TempDir()
	src := `package store

import "database/sql"

type Order struct{ ID int }

type OrderPGStore struct{ db *sql.DB }

func (s *OrderPGStore) List() []Order {
	out := []Order{}
	rows, err := s.db.Query(` + "`SELECT id FROM orders ORDER BY id`" + `)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var m Order
		if rows.Scan(&m.ID) == nil {
			out = append(out, m)
		}
	}
	return out
}

func (s *OrderPGStore) Get(id int) (Order, bool) {
	var m Order
	err := s.db.QueryRow(` + "`SELECT id FROM orders WHERE id=$1`" + `, id).Scan(&m.ID)
	if err != nil {
		return Order{}, false
	}
	return m, true
}

// GetAndMark is named as a read but writes.
func (s *OrderPGStore) GetAndMark(id int) bool {
	_, err := s.db.Exec(` + "`UPDATE orders SET seen=true WHERE id=$1`" + `, id)
	return err == nil
}
`
	p := filepath.Join(root, "internal", "store", "order_pg.go")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	rep := runUnderstandVerify(root)
	var effect []string
	for _, f := range rep.Findings {
		if f.Kind == "effect" {
			effect = append(effect, f.Symbol)
		}
	}
	if len(effect) != 1 || effect[0] != "OrderPGStore.GetAndMark" {
		t.Fatalf("effect findings = %q, want only OrderPGStore.GetAndMark", effect)
	}
}
