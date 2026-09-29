package migration

import "github.com/hpdkhoa/gen-system/generator/internal/compiler/diff"

// HasBreakingChanges reports whether applying the change set can lose data:
// a breaking type change, a dropped table, or a dropped column. gen migrate
// refuses such a change set unless the user passes --force.
//
// v26.57 (D005): before, only type changes counted, so a removed entity
// produced "DROP TABLE IF EXISTS <entity>;" with no gate at all, and its down
// SQL is a comment, so the table could not come back.
func HasBreakingChanges(cs diff.ChangeSet) bool {
	if len(cs.RemoveEntities) > 0 {
		return true
	}
	for _, mod := range cs.Modified {
		if len(mod.Removed) > 0 {
			return true
		}
		for _, f := range mod.Modified {
			if f.Breaking {
				return true
			}
		}
	}
	return false
}
