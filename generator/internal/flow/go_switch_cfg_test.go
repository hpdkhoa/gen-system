// go_switch_cfg_test.go (v25.9): a Go `switch` must build as a multi-way CFG
// (one deciding block fanning out to a case-block per clause, all rejoining at a
// shared merge), not flattened into an if/else cascade.
//
// The bug this pins: the Go frontend concatenated every case body into
// NodeBranch.Children with no case values and no Else, so flow.Build's if/else
// path produced a chain of fake decisions whose conditions were borrowed from the
// `if`s *inside* the case bodies (e.g. a `h.store.Delete(id)` diamond standing in
// for the switch discriminant), and the switch itself rendered as a blank diamond.
package flow

import (
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

const switchSrc = `package p

import "net/http"

type H struct{}

func (h *H) item(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		get()
	case http.MethodPut:
		put()
	case http.MethodDelete:
		del()
	default:
		other()
	}
}

func get()   {}
func put()   {}
func del()   {}
func other() {}
`

// buildSwitchCFG parses switchSrc and builds the CFG for H.item.
func buildSwitchCFG(t *testing.T) *CFG {
	t.Helper()
	fe := &ast.GoFrontend{}
	m, err := fe.Parse("h.go", []byte(switchSrc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	idx := symbols.NewIndex()
	idx.AddModule(m)
	set := Build(m, idx)
	for id, cfg := range set {
		if strings.HasSuffix(id, "item") {
			return cfg
		}
	}
	t.Fatalf("no CFG built for H.item (got %d cfgs)", len(set))
	return nil
}

// The switch must produce a deciding block whose Cond names the tag, and one
// case block per clause carrying its case value.
func TestGoSwitch_IsMultiWayNotIfElseCascade(t *testing.T) {
	cfg := buildSwitchCFG(t)

	var decider *BasicBlock
	var caseConds []string
	for _, b := range cfg.Blocks {
		if strings.HasPrefix(b.Cond, "switch ") {
			decider = b
		}
	}
	if decider == nil {
		t.Fatal("no deciding block with a `switch <tag>` condition — the switch was flattened")
	}
	if !strings.Contains(decider.Cond, "r.Method") {
		t.Errorf("deciding block Cond = %q, want it to name the tag r.Method", decider.Cond)
	}

	// Every successor of the decider should be a case block carrying a case value.
	if len(decider.Succs) < 4 {
		t.Fatalf("switch fans out to %d blocks, want >=4 (Get/Put/Delete/default)", len(decider.Succs))
	}
	for _, sid := range decider.Succs {
		for _, b := range cfg.Blocks {
			if b.ID == sid && b.Cond != "" {
				caseConds = append(caseConds, b.Cond)
			}
		}
	}
	want := []string{"http.MethodGet", "http.MethodPut", "http.MethodDelete", "default"}
	for _, w := range want {
		found := false
		for _, got := range caseConds {
			if got == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing case arm %q; got arms %v", w, caseConds)
		}
	}
}

// Guard the specific symptom: no block's condition may be borrowed from a
// statement that isn't a real branch condition. With the old bug, the switch's
// diamond was blank and inner statements supplied the conditions.
func TestGoSwitch_DecidingBlockNotBlank(t *testing.T) {
	cfg := buildSwitchCFG(t)
	for _, b := range cfg.Blocks {
		if len(b.Succs) >= 4 && b.Cond == "" {
			t.Errorf("block %d fans out to %d successors with a BLANK condition — "+
				"a multi-way decision must carry its tag", b.ID, len(b.Succs))
		}
	}
}
