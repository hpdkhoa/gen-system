// diff.go — TASK 3 (v19): skip appending EntityChange to Modified when
// Added, Removed, and Modified are all empty (was: always appended).
package diff

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

type Schema struct {
	Entities []Entity
}

// ------------------------------------------
// ChangeSet
// ------------------------------------------

type ChangeSet struct {
	AddEntities    []Entity
	RemoveEntities []Entity
	Modified       []EntityChange
}

type EntityChange struct {
	EntityName string
	Added      []Field
	Removed    []Field
	Modified   []FieldChange
}

type FieldChange struct {
	Field    string
	OldType  string
	NewType  string
	Breaking bool
}

// ------------------------------------------
// MAIN ENTRY
// ------------------------------------------

func Diff(oldIR, newIR Schema) ChangeSet {

	cs := ChangeSet{}

	oldMap := map[string]Entity{}
	newMap := map[string]Entity{}

	for _, e := range oldIR.Entities {
		oldMap[e.Name] = e
	}

	for _, e := range newIR.Entities {
		newMap[e.Name] = e
	}

	// detect adds + modifies
	for name, newE := range newMap {
		oldE, exists := oldMap[name]

		if !exists {
			cs.AddEntities = append(cs.AddEntities, newE)
			continue
		}

		change := EntityChange{
			EntityName: name,
		}

		oldFields := map[string]Field{}
		for _, f := range oldE.Fields {
			oldFields[f.Name] = f
		}

		for _, nf := range newE.Fields {
			of, ok := oldFields[nf.Name]

			if !ok {
				change.Added = append(change.Added, nf)
				continue
			}

			if of.Type != nf.Type {
				change.Modified = append(change.Modified, FieldChange{
					Field:    nf.Name,
					OldType:  of.Type,
					NewType:  nf.Type,
					Breaking: true,
				})
			}
		}

		// v26.58 (D040): the loop above walks only the new fields, so a field
		// the entity lost never reached Removed, and gen migrate reported no
		// change while the column stayed. Old fields absent from the new
		// entity are removed; old order keeps the output stable.
		newFields := map[string]bool{}
		for _, nf := range newE.Fields {
			newFields[nf.Name] = true
		}
		for _, of := range oldE.Fields {
			if !newFields[of.Name] {
				change.Removed = append(change.Removed, of)
			}
		}

		// Only record this entity as modified when there is at least one
		// field-level change. Appending an empty EntityChange for unchanged
		// entities caused ghost entries in the UI and redundant work in
		// BuildMigration / HasBreakingChanges.
		if len(change.Added) > 0 || len(change.Removed) > 0 || len(change.Modified) > 0 {
			cs.Modified = append(cs.Modified, change)
		}
	}

	// detect removals
	for name, oldE := range oldMap {
		if _, exists := newMap[name]; !exists {
			cs.RemoveEntities = append(cs.RemoveEntities, oldE)
		}
	}

	return cs
}
