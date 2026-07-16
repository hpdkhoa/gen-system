package buildplan

// Minimal interface to avoid importing compiler
type EntityIR struct {
	Name string
}

type SchemaIR struct {
	Entities []EntityIR
}

type BuildPlan struct {
	Entities []string
	Changed  []string
}

func Plan(oldIR, newIR SchemaIR) BuildPlan {

	old := map[string]bool{}
	newm := map[string]bool{}

	for _, e := range oldIR.Entities {
		old[e.Name] = true
	}

	for _, e := range newIR.Entities {
		newm[e.Name] = true
	}

	var plan BuildPlan

	// only changed entities are rebuilt
	for name := range newm {
		if !old[name] {
			plan.Entities = append(plan.Entities, name)
		} else {
			plan.Changed = append(plan.Changed, name)
		}
	}

	return plan
}
