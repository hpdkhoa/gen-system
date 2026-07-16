package smartdiff

import "gen-system/generator/internal/schema"

type Diff struct {
	AddedFields    []schema.Field
	RemovedFields  []schema.Field
	RenamedFields  []Rename
	RelationAdded  []schema.Relation
	RelationRemove []schema.Relation
}

type Rename struct {
	From schema.Field
	To   schema.Field
}

func Compare(oldE, newE schema.Entity) Diff {

	diff := Diff{}

	oldMap := map[string]schema.Field{}
	newMap := map[string]schema.Field{}

	for _, f := range oldE.Fields {
		oldMap[f.Name] = f
	}
	for _, f := range newE.Fields {
		newMap[f.Name] = f
	}

	// detect rename (same type, different name)
	for _, oldF := range oldE.Fields {
		for _, newF := range newE.Fields {
			if oldF.Type == newF.Type && oldF.Name != newF.Name {
				diff.RenamedFields = append(diff.RenamedFields, Rename{
					From: oldF,
					To:   newF,
				})
			}
		}
	}

	// added / removed
	for name, f := range newMap {
		if _, ok := oldMap[name]; !ok {
			diff.AddedFields = append(diff.AddedFields, f)
		}
	}

	for name, f := range oldMap {
		if _, ok := newMap[name]; !ok {
			diff.RemovedFields = append(diff.RemovedFields, f)
		}
	}

	// relations
	oldRel := map[string]bool{}
	newRel := map[string]bool{}

	for _, r := range oldE.Relations {
		oldRel[r.Target+r.ForeignKey] = true
	}
	for _, r := range newE.Relations {
		newRel[r.Target+r.ForeignKey] = true
	}

	for _, r := range newE.Relations {
		if !oldRel[r.Target+r.ForeignKey] {
			diff.RelationAdded = append(diff.RelationAdded, r)
		}
	}

	for _, r := range oldE.Relations {
		if !newRel[r.Target+r.ForeignKey] {
			diff.RelationRemove = append(diff.RelationRemove, r)
		}
	}

	return diff

}
