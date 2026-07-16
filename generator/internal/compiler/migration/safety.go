package migration

import "gen-system/generator/internal/compiler/diff"

func HasBreakingChanges(cs diff.ChangeSet) bool {

	for _, mod := range cs.Modified {
		for _, f := range mod.Modified {
			if f.Breaking {
				return true
			}
		}
	}

	return false
}
