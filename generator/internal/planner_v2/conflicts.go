// conflicts.go — v25.8: push-back conflict detection for the continue/evolve
// consent gate, in a neutral package (planner_v2) so BOTH the CLI gate
// (package cli) and the server continue flow (package services) reuse ONE
// implementation. Previously this lived in cli; services cannot import cli, so
// the logic moved here (planner_v2 already owns ContinueResult + ExistingState).
//
// Pure and deterministic: given what exists and the proposed delta, it flags
// where the delta conflicts with the existing code's apparent purpose.
package planner_v2

import (
	"fmt"
	"sort"
	"strings"
)

// ConflictKind classifies a push-back finding.
type ConflictKind string

const (
	ConflictWriteOnReadOnly ConflictKind = "write-on-read-service"
	ConflictDuplicateEntity ConflictKind = "duplicate-entity"
	ConflictRemoval         ConflictKind = "removal-in-use"
)

// Conflict is one reason the gate pushes back.
type Conflict struct {
	Kind    ConflictKind `json:"kind"`
	Subject string       `json:"subject"`
	Detail  string       `json:"detail"`
}

// mutatingPrefixes name operations that change state.
var mutatingPrefixes = []string{
	"create", "add", "update", "edit", "delete", "remove", "set",
	"apply", "checkout", "cancel", "refund", "adjust", "process",
	"submit", "approve", "reject", "assign", "archive", "close", "pay",
}

// readDominant is judged by the ABSENCE of mutating prefixes (below), so there's
// no separate read-prefix list to maintain.

func hasPrefixAny(name string, prefixes []string) bool {
	l := strings.ToLower(name)
	for _, p := range prefixes {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

// EntityIsReadDominant reports whether an existing entity's operations are all
// reads — so adding a write to it is a purpose change worth flagging. Needs at
// least one existing op to judge.
func EntityIsReadDominant(ops []string) bool {
	if len(ops) == 0 {
		return false
	}
	for _, o := range ops {
		if hasPrefixAny(o, mutatingPrefixes) {
			return false
		}
	}
	return true
}

// DetectConflicts compares the proposed delta against the existing state and
// returns the push-back findings (possibly empty).
func DetectConflicts(existing ExistingState, res ContinueResult) []Conflict {
	var out []Conflict

	// 1) write-op added to a read-dominant existing service.
	for ent, newOps := range res.NewOps {
		existingOps, known := existing.Ops[ent]
		if !known || !EntityIsReadDominant(existingOps) {
			continue
		}
		for _, op := range newOps {
			if hasPrefixAny(op, mutatingPrefixes) {
				out = append(out, Conflict{
					Kind:    ConflictWriteOnReadOnly,
					Subject: ent + "." + op,
					Detail: fmt.Sprintf("%q adds a state-changing operation to %s, which currently exposes only reads (%s) — is this codebase meant to mutate %s?",
						op, ent, strings.Join(existingOps, ", "), ent),
				})
			}
		}
	}

	// 2) a "new" entity that duplicates or near-duplicates an existing one.
	existingLower := map[string]string{}
	for _, e := range existing.EntityOrder {
		existingLower[strings.ToLower(e)] = e
	}
	for _, e := range res.Merged.Entities {
		if !res.ChangedSet[e.Name] {
			continue
		}
		if _, alreadyExisted := existing.Entities[e.Name]; alreadyExisted {
			continue
		}
		if orig, clash := existingLower[strings.ToLower(e.Name)]; clash && orig != e.Name {
			out = append(out, Conflict{
				Kind:    ConflictDuplicateEntity,
				Subject: e.Name,
				Detail: fmt.Sprintf("new entity %q differs only in case from existing %q — likely a duplicate that will fragment the model",
					e.Name, orig),
			})
		}
	}

	// 3) removal of an entity/field.
	for ent, removed := range res.Removed {
		for _, r := range removed {
			what := "field " + r
			if r == "*" {
				what = "the entire entity"
			}
			out = append(out, Conflict{
				Kind:    ConflictRemoval,
				Subject: ent,
				Detail:  fmt.Sprintf("delta removes %s from %s — anything referencing it will break", what, ent),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Subject < out[j].Subject
	})
	return out
}
