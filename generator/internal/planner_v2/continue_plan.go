// generator/internal/planner_v2/continue_plan.go, v23.24 (task 4A): turns a
// free-text idea + an existing codebase into a delta plan. It asks the model
// (ai.ProposeContinueDelta) for tagged changes, applies them deterministically
// to the existing entities (additive NEW/ADD_FIELD; REMOVE only when allowed),
// computes which entities were touched, and returns a planner.Plan whose Goal is
// the idea so the agent's per-entity service-logic generation (gate + repair
// loop) derives the new operations from that idea text.
//
// The deterministic application is the safety net for a model that re-proposes
// something that already exists: NEW for an existing entity degrades to "merge
// its fields"; ADD_FIELD of an existing field is a no-op; NEW_OP is advisory
// (logic generation is idea-driven). REMOVE is collected always but applied only
// when allowRemove is true.
package planner_v2

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/ai"
	"github.com/hpdkhoa/gen-system/generator/internal/planner"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// ContinueResult is the outcome of planning a continuation.
type ContinueResult struct {
	Plan       planner.Plan
	Merged     schema.SchemaFile
	ChangedSet map[string]bool // entities to (re)generate
	NewOps     map[string][]string
	Removed    map[string][]string // entity → fields removed (entity-level uses key "*")
	Summary    string              // human-readable change summary
}

// deltaSource is the function used to obtain the proposed delta. It defaults to
// the live model call (ai.ProposeContinueDelta) but can be overridden for
// deterministic offline verification (tests / sandbox), mirroring the
// replay-provider discipline used for business-logic generation.
//
// Offline override: if GEN_CONTINUE_DELTA names a readable JSON file (a
// ContinueDelta envelope or a bare changes array), it is used instead of a
// model call. This lets a user (or the sandbox) drive a fully deterministic
// continuation with a hand-written delta and no Ollama present.
var deltaSource = func(idea, existingSummary string) (ai.ContinueDelta, error) {
	if path := strings.TrimSpace(os.Getenv("GEN_CONTINUE_DELTA")); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return ai.ContinueDelta{}, fmt.Errorf("GEN_CONTINUE_DELTA: %w", err)
		}
		return ai.ParseContinueDelta(string(data))
	}
	return ai.ProposeContinueDelta(idea, existingSummary)
}

// SetDeltaSourceForTest overrides the delta source. Returns a restore func.
// Used by offline end-to-end verification where no model is present.
func SetDeltaSourceForTest(fn func(idea, existingSummary string) (ai.ContinueDelta, error)) func() {
	prev := deltaSource
	deltaSource = fn
	return func() { deltaSource = prev }
}

// BuildContinuePlan is the planner_v2 entry point for `gen continue`.
//
//	existing    : current entities (from the loaded context IR), name→[]Field
//	existingSum : rendered "what exists" block for the prompt
//	idea        : the natural-language instruction
//	writeDir    : where generation will be staged/applied (drives the task graph)
//	allowRemove : honor REMOVE tags (destructive); the CLI gates it
func BuildContinuePlan(existing []schema.Entity, existingSum, idea, writeDir string, allowRemove bool) (ContinueResult, error) {
	if strings.TrimSpace(idea) == "" {
		return ContinueResult{}, fmt.Errorf("continue: idea cannot be empty")
	}

	delta, err := deltaSource(idea, existingSum)
	if err != nil {
		return ContinueResult{}, err
	}

	res := applyDelta(existing, delta, allowRemove)

	// Build the task graph for only the touched entities, then a plan. The plan
	// must carry all merged entities, the unchanged ones too: the agent's
	// orphan-pruning step deletes per-entity files for any entity not in
	// plan.Entities, so omitting unchanged entities (e.g. Product) would wrongly
	// prune their files from the staged copy. The task graph (built from
	// changedSet) still restricts actual regeneration to the touched entities.
	merged := schema.SchemaFile{Entities: res.mergedEntities}
	tasks := planner.TaskGraphDynamic(merged, writeDir, res.changedSet)

	plan := planner.Plan{
		Goal:     idea,
		Entities: merged.Entities, // full set → pruning preserves unchanged entities
		Tasks:    tasks,
		Notes: []string{
			"Planner: continue-from-existing (v23.24)",
			res.summary,
		},
	}

	return ContinueResult{
		Plan:       plan,
		Merged:     merged,
		ChangedSet: res.changedSet,
		NewOps:     res.newOps,
		Removed:    res.removed,
		Summary:    res.summary,
	}, nil
}

// deltaApplication is the internal result of folding the tagged delta into the
// existing entity set.
type deltaApplication struct {
	mergedEntities []schema.Entity
	changedSet     map[string]bool
	newOps         map[string][]string
	removed        map[string][]string
	summary        string
}

func applyDelta(existing []schema.Entity, delta ai.ContinueDelta, allowRemove bool) deltaApplication {
	// Index existing entities by name (order preserved via slice).
	byName := map[string]int{}
	merged := make([]schema.Entity, len(existing))
	copy(merged, existing)
	for i, e := range merged {
		byName[e.Name] = i
	}

	changed := map[string]bool{}
	newOps := map[string][]string{}
	removed := map[string][]string{}
	var notes []string

	ensureEntity := func(name string) int {
		if idx, ok := byName[name]; ok {
			return idx
		}
		merged = append(merged, schema.Entity{Name: name})
		idx := len(merged) - 1
		byName[name] = idx
		return idx
	}

	hasField := func(idx int, fname string) bool {
		for _, f := range merged[idx].Fields {
			if strings.EqualFold(f.Name, fname) {
				return true
			}
		}
		return false
	}

	for _, c := range delta.Changes {
		ent := strings.TrimSpace(c.Entity)
		if ent == "" {
			continue
		}
		switch strings.ToUpper(c.Kind) {

		case ai.ChangeNew:
			idx := ensureEntity(ent)
			// Always ensure an id field exists on a new entity.
			if !hasField(idx, "id") {
				merged[idx].Fields = append(merged[idx].Fields, schema.Field{Name: "id", Type: "int"})
			}
			added := 0
			for _, fname := range sortedKeysOf(c.Fields) {
				if !hasField(idx, fname) {
					merged[idx].Fields = append(merged[idx].Fields,
						schema.Field{Name: fname, Type: normalizeFieldType(c.Fields[fname])})
					added++
				}
			}
			changed[ent] = true
			notes = append(notes, fmt.Sprintf("NEW entity %s (%d fields)", ent, added))

		case ai.ChangeAddField:
			if c.Field == "" {
				continue
			}
			idx := ensureEntity(ent)
			if !hasField(idx, c.Field) {
				ft := c.FieldType
				if strings.TrimSpace(ft) == "" {
					ft = "string"
				}
				merged[idx].Fields = append(merged[idx].Fields,
					schema.Field{Name: c.Field, Type: normalizeFieldType(ft)})
				changed[ent] = true
				notes = append(notes, fmt.Sprintf("ADD_FIELD %s.%s", ent, c.Field))
			}

		case ai.ChangeNewOp:
			if c.Op == "" {
				continue
			}
			// Operation generation is idea-driven in the agent; we record it so
			// the entity is (re)generated and the op intent is reported.
			ensureEntity(ent)
			newOps[ent] = appendUniqueStr(newOps[ent], c.Op)
			changed[ent] = true
			notes = append(notes, fmt.Sprintf("NEW_OP %s.%s", ent, c.Op))

		case ai.ChangeRemove:
			if !allowRemove {
				notes = append(notes, fmt.Sprintf("(skipped REMOVE %s%s; pass --allow-remove to apply)",
					ent, fieldSuffix(c.Field)))
				continue
			}
			idx, ok := byName[ent]
			if !ok {
				continue
			}
			if c.Field == "" {
				// Entity-level removal: drop it entirely.
				merged = removeEntityAt(merged, idx)
				byName = reindex(merged)
				removed[ent] = appendUniqueStr(removed[ent], "*")
				changed[ent] = true
				notes = append(notes, fmt.Sprintf("REMOVE entity %s", ent))
			} else {
				var kept []schema.Field
				for _, f := range merged[idx].Fields {
					if strings.EqualFold(f.Name, c.Field) {
						removed[ent] = appendUniqueStr(removed[ent], c.Field)
						continue
					}
					kept = append(kept, f)
				}
				merged[idx].Fields = kept
				changed[ent] = true
				notes = append(notes, fmt.Sprintf("REMOVE %s.%s", ent, c.Field))
			}
		}
	}

	if len(notes) == 0 {
		notes = append(notes, "no changes proposed (instruction already satisfied)")
	}

	// If any entity was removed, the shared/global files (store interfaces,
	// main.go wiring, router) still reference it. Force every surviving entity
	// into the changed set so those files are regenerated without the removed
	// entity. Otherwise the staged app fails to build with dangling references.
	entityRemoved := false
	for _, fields := range removed {
		for _, f := range fields {
			if f == "*" {
				entityRemoved = true
			}
		}
	}
	if entityRemoved {
		for _, e := range merged {
			changed[e.Name] = true
		}
		// Drop any removed entity names that are no longer present.
		for name := range changed {
			if _, ok := byName[name]; !ok {
				delete(changed, name)
			}
		}
	}

	return deltaApplication{
		mergedEntities: merged,
		changedSet:     changed,
		newOps:         newOps,
		removed:        removed,
		summary:        strings.Join(notes, "; "),
	}
}

// ── helpers ──────────────────────────────────────────────────

func fieldSuffix(field string) string {
	if field == "" {
		return ""
	}
	return "." + field
}

func removeEntityAt(es []schema.Entity, idx int) []schema.Entity {
	out := make([]schema.Entity, 0, len(es)-1)
	out = append(out, es[:idx]...)
	out = append(out, es[idx+1:]...)
	return out
}

func reindex(es []schema.Entity) map[string]int {
	m := map[string]int{}
	for i, e := range es {
		m[e.Name] = i
	}
	return m
}

// sortedKeysOf returns map keys in deterministic order (the model returns an
// unordered object; determinism matters for reproducible output).
func sortedKeysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
