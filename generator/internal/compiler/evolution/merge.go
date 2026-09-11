package evolution

import "sort"

// ------------------------------------------
// Minimal IR (decoupled from compiler)
// ------------------------------------------

type Field struct {
	Name string
	Type string
}

type Entity struct {
	Name   string
	Fields []Field
}

type Relation struct {
	FromEntity string
	ToEntity   string
	FromField  string
	Type       string
}

type Schema struct {
	Entities  []Entity
	Relations []Relation
}

// DiffIntent signals which entities and fields the user explicitly
// wants removed. Used by MergeWithDiff for safe deletion.
type DiffIntent struct {
	// RemoveEntities is a set of entity names to drop entirely.
	RemoveEntities map[string]bool
	// RemoveFields maps entity name → set of field names to remove.
	RemoveFields map[string]map[string]bool
	// RemoveRelations is a set of relation keys (FromEntity+ToEntity+FromField) to remove.
	RemoveRelations map[string]bool
}

// ------------------------------------------
// Merge: safe additive merge (upsert only)
//
// This is the default "safe evolution" mode. It never deletes
// existing fields or entities — only adds or updates them.
// The report correctly identifies this as intentional safety.
// ------------------------------------------
func Merge(oldIR, newIR Schema) Schema {
	result := oldIR

	// 1. merge entities (upsert)
	for _, newE := range newIR.Entities {
		found := false
		for i, oldE := range result.Entities {
			if oldE.Name == newE.Name {
				result.Entities[i] = mergeEntity(oldE, newE)
				found = true
				break
			}
		}
		if !found {
			result.Entities = append(result.Entities, newE)
		}
	}

	// 2. merge relations (dedup)
	for _, r := range newIR.Relations {
		if !relationExists(result, r) {
			result.Relations = append(result.Relations, r)
		}
	}

	return result
}

// ------------------------------------------
// MergeWithDiff: safe evolution WITH explicit deletion support.
//
// Use this when the user has explicitly signaled deletions via a
// plan (e.g. "Remove the 'age' field from User").
//
// Flow:
//  1. Start with additive Merge (safe base)
//  2. Apply DiffIntent removals (explicit, user-signaled)
//
// This keeps the "non-destructive by default" guarantee while
// enabling intentional deletions when the user asks for them.
// ------------------------------------------
func MergeWithDiff(oldIR, newIR Schema, intent DiffIntent) Schema {
	// Start with standard safe merge
	result := Merge(oldIR, newIR)

	// Apply entity removals
	if len(intent.RemoveEntities) > 0 {
		var kept []Entity
		for _, e := range result.Entities {
			if !intent.RemoveEntities[e.Name] {
				kept = append(kept, e)
			}
		}
		result.Entities = kept
	}

	// Apply field removals per entity
	if len(intent.RemoveFields) > 0 {
		for i, e := range result.Entities {
			if removeSet, ok := intent.RemoveFields[e.Name]; ok {
				var kept []Field
				for _, f := range e.Fields {
					if !removeSet[f.Name] {
						kept = append(kept, f)
					}
				}
				result.Entities[i].Fields = kept
			}
		}
	}

	// Apply relation removals
	if len(intent.RemoveRelations) > 0 {
		var kept []Relation
		for _, r := range result.Relations {
			key := r.FromEntity + "→" + r.ToEntity + "." + r.FromField
			if !intent.RemoveRelations[key] {
				kept = append(kept, r)
			}
		}
		result.Relations = kept
	}

	return result
}

// RelationKey produces the canonical key for a Relation (used in DiffIntent).
func RelationKey(r Relation) string {
	return r.FromEntity + "→" + r.ToEntity + "." + r.FromField
}

// ------------------------------------------
// mergeEntity: merge two entities' fields (upsert, preserves order)
// ------------------------------------------
func mergeEntity(oldE, newE Entity) Entity {
	fieldMap := map[string]Field{}

	for _, f := range oldE.Fields {
		fieldMap[f.Name] = f
	}
	for _, f := range newE.Fields {
		fieldMap[f.Name] = f // new value wins on type change
	}

	// Sort by name for deterministic output
	names := make([]string, 0, len(fieldMap))
	for name := range fieldMap {
		names = append(names, name)
	}
	sort.Strings(names)

	merged := make([]Field, 0, len(names))
	for _, name := range names {
		merged = append(merged, fieldMap[name])
	}

	oldE.Fields = merged
	return oldE
}

// ------------------------------------------
// relationExists: deduplication check
// ------------------------------------------
func relationExists(ir Schema, r Relation) bool {
	for _, ex := range ir.Relations {
		if ex.FromEntity == r.FromEntity &&
			ex.ToEntity == r.ToEntity &&
			ex.FromField == r.FromField {
			return true
		}
	}
	return false
}
