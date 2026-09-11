// consent_gate.go — v25.8: the CONTINUE consent gate (CLI presentation layer).
//
// The conflict-DETECTION logic moved to planner_v2 (conflicts.go) so the server
// continue flow can reuse the exact same rules. This file is now just the CLI
// presentation + prompt around planner_v2.DetectConflicts.
//
// WHY THE GATE. `gen continue` reads existing source, plans a DELTA, then
// stages+generates. This gate summarizes what the existing code is FOR, shows the
// proposed delta, pushes back on conflicts with that purpose, and waits for
// confirmation before the caller acts — the front-of-pipeline analog of
// --verify-understand.
//
// Flags: default prompts; --no-gate restores the old silent behaviour;
// --yes/--force skip the prompt (conflicts still printed); --gate-strict makes a
// conflict default the prompt to explicit-y.
//
// SCOPE: lives only in `continue` (the one flow with a real ContinueResult +
// ExistingState). evolve / evolve_understand use a plain plan and are unaffected.
package cli

import (
	"fmt"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	"github.com/hpdkhoa/gen-system/generator/internal/planner_v2"
)

// RunConsentGate prints the existing-purpose summary, the proposed delta, and any
// conflicts, then decides whether to proceed. Returns true to continue, false to
// abort.
func RunConsentGate(existing planner_v2.ExistingState, res planner_v2.ContinueResult, plan planner.Plan) bool {
	conflicts := planner_v2.DetectConflicts(existing, res)

	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────")
	fmt.Println("│ REVIEW — what exists, and what this will change")
	fmt.Println("└─────────────────────────────────────────────")

	fmt.Println("\n  Existing code:")
	if len(existing.EntityOrder) == 0 {
		fmt.Println("    (no recognizable entities — this looks like a fresh or non-generated target)")
	} else {
		for _, ent := range existing.EntityOrder {
			ops := existing.Ops[ent]
			tag := ""
			if planner_v2.EntityIsReadDominant(ops) {
				tag = "  [read-only]"
			}
			fmt.Printf("    • %s (%d fields, ops: %s)%s\n",
				ent, len(existing.Entities[ent]), joinOr(ops, "none"), tag)
		}
	}

	fmt.Println("\n  Proposed change:")
	fmt.Printf("    %s\n", res.Summary)
	changed := sortedKeys(res.ChangedSet)
	if len(changed) > 0 {
		fmt.Printf("    (re)generates: %s\n", strings.Join(changed, ", "))
	}
	for ent, ops := range res.NewOps {
		if len(ops) > 0 {
			fmt.Printf("    new ops on %s: %s\n", ent, strings.Join(ops, ", "))
		}
	}

	if len(conflicts) > 0 {
		fmt.Printf("\n  ⚠️  %d thing%s to confirm before proceeding:\n", len(conflicts), plural1(len(conflicts)))
		for _, c := range conflicts {
			fmt.Printf("    ⚠️  [%s] %s\n", c.Kind, c.Detail)
		}
	} else {
		fmt.Println("\n  ✓ No conflicts with the existing code's apparent purpose.")
	}

	if HasFlag("yes") || HasFlag("force") {
		fmt.Println("\n  --yes/--force: proceeding without prompt (conflicts above are advisory).")
		return true
	}
	fmt.Println()
	question := "Proceed with generation?"
	if len(conflicts) > 0 && HasFlag("gate-strict") {
		question = "Conflicts detected. Proceed anyway?"
	}
	ok := confirm("  " + question)
	if !ok {
		fmt.Println("  ✋ Aborted. Nothing was staged or generated.")
	}
	return ok
}

// joinOr joins parts with ", " or returns fallback when empty.
func joinOr(parts []string, fallback string) string {
	if len(parts) == 0 {
		return fallback
	}
	return strings.Join(parts, ", ")
}
