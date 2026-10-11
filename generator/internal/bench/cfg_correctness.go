// generator/internal/bench/cfg_correctness.go (v19.7): validates that every
// CFG produced by `flow.Build` is structurally sound. The four invariants:
//
//  1. exactly one entry block, and Entry must index into Blocks
//  2. every Exits[i] must index into Blocks
//  3. no orphan blocks: every block (except Entry) must have ≥1 predecessor
//  4. every successor ID referenced from any block must index into Blocks
//
// These properties have to hold for the graph view in the Studio to render
// without breaking, and for any downstream analysis (dataflow, dominator
// trees, slicing) to be correct. We treat them as bench-time invariants
// rather than runtime panics because some are theoretically violatable by a
// frontend bug.
package bench

import (
	"fmt"
	"time"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
)

type cfgIssue struct {
	FuncID string
	Detail string
}

func CFGCorrectness(root string, copybookPath []string) Suite {
	t0 := time.Now()
	opts := agentctx.LoadContextOpts{CopybookPath: copybookPath}
	ctx := agentctx.LoadContextWithOpts(root, opts)

	suite := Suite{Name: "cfg_correctness"}

	if ctx.CFGs == nil || len(ctx.CFGs) == 0 {
		suite.Cases = append(suite.Cases, Case{
			Name: "has_cfgs", Pass: false,
			Note: "ctx.CFGs is empty; nothing to validate",
		})
		suite.Duration = time.Since(t0)
		return suite
	}

	total := 0
	var issues []cfgIssue
	// per-rule counters so each rule gets its own pass/fail case
	var (
		entryOK, exitOK, orphanOK, succOK     int
		entryBad, exitBad, orphanBad, succBad int
	)

	for _, cfgs := range ctx.CFGs {
		for funcID, c := range cfgs {
			if c == nil {
				continue
			}
			total++
			// Rule 1: Entry must be a valid block ID.
			if c.Entry < 0 || c.Entry >= len(c.Blocks) {
				entryBad++
				issues = append(issues, cfgIssue{funcID, fmt.Sprintf("Entry=%d out of range [0,%d)", c.Entry, len(c.Blocks))})
			} else {
				entryOK++
			}
			// Rule 2: every Exit must be a valid block ID.
			exitFail := false
			for _, ex := range c.Exits {
				if ex < 0 || ex >= len(c.Blocks) {
					exitBad++
					issues = append(issues, cfgIssue{funcID, fmt.Sprintf("Exit=%d out of range", ex)})
					exitFail = true
					break
				}
			}
			if !exitFail {
				exitOK++
			}
			// Rule 3: no orphan blocks (every non-entry block must have
			// at least one predecessor).
			orphanFail := false
			for _, b := range c.Blocks {
				if b == nil {
					continue
				}
				if b.ID == c.Entry {
					continue
				}
				if len(b.Preds) == 0 {
					orphanBad++
					issues = append(issues, cfgIssue{funcID, fmt.Sprintf("block %d (%s) has no preds", b.ID, b.Label)})
					orphanFail = true
					break
				}
			}
			if !orphanFail {
				orphanOK++
			}
			// Rule 4: every successor referenced must index into Blocks.
			succFail := false
			for _, b := range c.Blocks {
				if b == nil {
					continue
				}
				for _, s := range b.Succs {
					if s < 0 || s >= len(c.Blocks) {
						succBad++
						issues = append(issues, cfgIssue{funcID, fmt.Sprintf("block %d succ=%d out of range", b.ID, s)})
						succFail = true
						break
					}
				}
				if succFail {
					break
				}
			}
			if !succFail {
				succOK++
			}
		}
	}

	addCase := func(name string, ok, bad int) {
		suite.Cases = append(suite.Cases, Case{
			Name: name,
			Pass: bad == 0,
			Got:  fmt.Sprintf("%d ok, %d bad", ok, bad),
			Want: "all ok",
		})
	}
	addCase("rule1_entry_in_range", entryOK, entryBad)
	addCase("rule2_exits_in_range", exitOK, exitBad)
	addCase("rule3_no_orphan_blocks", orphanOK, orphanBad)
	addCase("rule4_successors_in_range", succOK, succBad)

	suite.Metrics = []Metric{
		{Name: "cfgs_total", Value: float64(total), Unit: "cfgs", HigherIsBetter: true},
		{Name: "cfgs_with_issue", Value: float64(entryBad + exitBad + orphanBad + succBad), Unit: "cfgs"},
	}

	// Stash up to 5 example issues in Notes so the report is actionable.
	limit := len(issues)
	if limit > 5 {
		limit = 5
	}
	if limit > 0 {
		suite.Notes = "first issues: "
		for i := 0; i < limit; i++ {
			suite.Notes += fmt.Sprintf("[%s: %s] ", issues[i].FuncID, issues[i].Detail)
		}
	}

	// Cheap import-keeper so 'flow' is referenced even if no CFG had issues
	// (older Go versions wouldn't complain, but be explicit).
	_ = flow.EffectPure

	suite.Duration = time.Since(t0)
	return suite
}
