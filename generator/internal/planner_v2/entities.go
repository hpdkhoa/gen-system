package planner_v2

import (
	"fmt"

	"github.com/hpdkhoa/gen-system/generator/internal/ai"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// GenerateEntities generates entity specs from the idea (goal) + features +
// architecture. The goal text is the primary anchor (v23.20) so the model
// designs the described product, not a generic one.
func GenerateEntities(goal string, features []string, arch Architecture) ([]EntitySpec, error) {
	if len(features) == 0 {
		return nil, fmt.Errorf("no features provided")
	}

	rawSpecs, err := ai.PlanEntities(goal, features, arch.Type, arch.Components)
	if err != nil {
		return nil, fmt.Errorf("entity generation: %w", err)
	}

	specs := make([]EntitySpec, 0, len(rawSpecs))
	for _, r := range rawSpecs {
		specs = append(specs, EntitySpec{
			Name:   r.Name,
			Fields: r.Fields,
		})
	}
	return specs, nil
}

// EntitySpecsToSchema converts planner_v2 EntitySpecs into schema.Entity
// values compatible with the existing task graph and engine.
func EntitySpecsToSchema(specs []EntitySpec) []schema.Entity {
	var entities []schema.Entity
	for _, s := range specs {
		e := schema.Entity{Name: s.Name}
		for fname, ftype := range s.Fields {
			e.Fields = append(e.Fields, schema.Field{
				Name: capitalize(fname),
				Type: normalizeFieldType(ftype),
			})
		}
		entities = append(entities, e)
	}
	return entities
}
