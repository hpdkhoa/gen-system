// generator/internal/bench/diff_correctness.go — v19.7: regression bench for
// the schema diff. Locks down the v19 ghost-entry fix and the basic add /
// remove / type-change behaviours.
//
// This is a unit-style scenario — it does not need a project root.
package bench

import (
	"fmt"
	"time"

	"gen-system/generator/internal/compiler/diff"
)

func DiffCorrectness() Suite {
	t0 := time.Now()
	s := Suite{Name: "diff_correctness"}

	type tc struct {
		name      string
		oldS      diff.Schema
		newS      diff.Schema
		wantAdd   int
		wantRem   int
		wantMod   int
	}
	cases := []tc{
		{
			name:    "identical_schema_emits_nothing",
			oldS:    diff.Schema{Entities: []diff.Entity{{Name: "User", Fields: []diff.Field{{Name: "ID", Type: "int"}}}}},
			newS:    diff.Schema{Entities: []diff.Entity{{Name: "User", Fields: []diff.Field{{Name: "ID", Type: "int"}}}}},
			wantAdd: 0, wantRem: 0, wantMod: 0,
		},
		{
			name:    "added_entity",
			oldS:    diff.Schema{},
			newS:    diff.Schema{Entities: []diff.Entity{{Name: "Post"}}},
			wantAdd: 1, wantRem: 0, wantMod: 0,
		},
		{
			name:    "removed_entity",
			oldS:    diff.Schema{Entities: []diff.Entity{{Name: "Post"}}},
			newS:    diff.Schema{},
			wantAdd: 0, wantRem: 1, wantMod: 0,
		},
		{
			name: "field_added_to_existing_entity",
			oldS: diff.Schema{Entities: []diff.Entity{{Name: "User", Fields: []diff.Field{{Name: "ID", Type: "int"}}}}},
			newS: diff.Schema{Entities: []diff.Entity{{Name: "User", Fields: []diff.Field{
				{Name: "ID", Type: "int"}, {Name: "Email", Type: "string"},
			}}}},
			wantAdd: 0, wantRem: 0, wantMod: 1,
		},
		{
			name: "type_change_is_breaking_but_still_modified",
			oldS: diff.Schema{Entities: []diff.Entity{{Name: "User", Fields: []diff.Field{{Name: "ID", Type: "int"}}}}},
			newS: diff.Schema{Entities: []diff.Entity{{Name: "User", Fields: []diff.Field{{Name: "ID", Type: "uuid"}}}}},
			wantAdd: 0, wantRem: 0, wantMod: 1,
		},
		// v19 ghost-entry regression: two schemas equal field-for-field
		// must never produce an EntityChange row.
		{
			name: "v19_no_ghost_entity_change",
			oldS: diff.Schema{Entities: []diff.Entity{
				{Name: "User", Fields: []diff.Field{{Name: "ID", Type: "int"}}},
				{Name: "Post", Fields: []diff.Field{{Name: "Title", Type: "string"}}},
			}},
			newS: diff.Schema{Entities: []diff.Entity{
				{Name: "User", Fields: []diff.Field{{Name: "ID", Type: "int"}}},
				{Name: "Post", Fields: []diff.Field{{Name: "Title", Type: "string"}}},
			}},
			wantAdd: 0, wantRem: 0, wantMod: 0,
		},
	}

	for _, c := range cases {
		got := diff.Diff(c.oldS, c.newS)
		pass := len(got.AddEntities) == c.wantAdd &&
			len(got.RemoveEntities) == c.wantRem &&
			len(got.Modified) == c.wantMod
		s.Cases = append(s.Cases, Case{
			Name: c.name,
			Pass: pass,
			Got:  fmt.Sprintf("add=%d rem=%d mod=%d", len(got.AddEntities), len(got.RemoveEntities), len(got.Modified)),
			Want: fmt.Sprintf("add=%d rem=%d mod=%d", c.wantAdd, c.wantRem, c.wantMod),
		})
	}

	s.Metrics = []Metric{
		{Name: "diff_cases_total", Value: float64(len(cases)), Unit: "cases", HigherIsBetter: true},
	}
	s.Duration = time.Since(t0)
	return s
}
