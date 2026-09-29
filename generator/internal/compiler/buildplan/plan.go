package buildplan

// Minimal interface to avoid importing compiler
type EntityIR struct {
	Name string
	// v26.58 (D072): the fields let Plan compare content. With the name alone,
	// every entity of both schemas counted as changed.
	Fields []FieldIR
}

// FieldIR is the part of a field that decides whether an entity changed.
type FieldIR struct {
	Name string
	Type string
}

type SchemaIR struct {
	Entities []EntityIR
}

type BuildPlan struct {
	Entities []string
	Changed  []string
}

func Plan(oldIR, newIR SchemaIR) BuildPlan {

	old := map[string]EntityIR{}
	newm := map[string]EntityIR{}

	for _, e := range oldIR.Entities {
		old[e.Name] = e
	}

	for _, e := range newIR.Entities {
		newm[e.Name] = e
	}

	var plan BuildPlan

	// only changed entities are rebuilt
	for name, ne := range newm {
		oe, ok := old[name]
		if !ok {
			plan.Entities = append(plan.Entities, name)
		} else if !sameFields(oe.Fields, ne.Fields) {
			// v26.58 (D072): an entity in both schemas is changed only when its
			// field set (name and type) differs; before, every one was.
			plan.Changed = append(plan.Changed, name)
		}
	}

	return plan
}

// sameFields reports whether two field lists hold the same names with the same
// types, in any order. v26.58 (D072).
func sameFields(a, b []FieldIR) bool {
	ma, mb := fieldTypes(a), fieldTypes(b)
	if len(ma) != len(mb) {
		return false
	}
	for name, t := range ma {
		if u, ok := mb[name]; !ok || u != t {
			return false
		}
	}
	return true
}

func fieldTypes(fs []FieldIR) map[string]string {
	m := make(map[string]string, len(fs))
	for _, f := range fs {
		m[f.Name] = f.Type
	}
	return m
}
