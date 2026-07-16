package versioning

import "gen-system/generator/internal/schema"

type Diff struct {
	Added   []schema.Field
	Removed []schema.Field
	Changed []FieldChange
}

type FieldChange struct {
	Old schema.Field
	New schema.Field
}

func Compare(oldS, newS schema.Entity) Diff {
	diff := Diff{}

	oldMap := map[string]schema.Field{}
	newMap := map[string]schema.Field{}

	for _, f := range oldS.Fields {
		oldMap[f.Name] = f
	}
	for _, f := range newS.Fields {
		newMap[f.Name] = f
	}

	// Added
	for name, f := range newMap {
		if _, ok := oldMap[name]; !ok {
			diff.Added = append(diff.Added, f)
		}
	}

	// Removed
	for name, f := range oldMap {
		if _, ok := newMap[name]; !ok {
			diff.Removed = append(diff.Removed, f)
		}
	}

	// Changed
	for name, oldF := range oldMap {
		if newF, ok := newMap[name]; ok {
			if oldF.Type != newF.Type {
				diff.Changed = append(diff.Changed, FieldChange{
					Old: oldF,
					New: newF,
				})
			}
		}
	}

	return diff

}
