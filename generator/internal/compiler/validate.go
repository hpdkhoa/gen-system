package compiler

import "errors"

func validateIR(ir SchemaIR) error {

	// v26.57 (D006): collect every entity first, then check the fields. The
	// set used to fill inside the field loop, so a child listed before its
	// parent (Post with userId before User) failed with "invalid FK
	// reference: User" although the schema is valid. The planner lets the
	// model choose the order, so a valid plan could fail on order alone.
	entityMap := map[string]bool{}
	for _, e := range ir.Entities {
		if entityMap[e.Name] {
			return errors.New("duplicate entity: " + e.Name)
		}
		entityMap[e.Name] = true
	}

	for _, e := range ir.Entities {
		fieldMap := map[string]bool{}

		for _, f := range e.Fields {
			if fieldMap[f.Name] {
				return errors.New("duplicate field: " + e.Name + "." + f.Name)
			}
			fieldMap[f.Name] = true

			// semantic check: FK must point to valid entity
			if f.IsForeign && f.RefEntity != "" {
				if !entityMap[f.RefEntity] {
					return errors.New("invalid FK reference: " + f.RefEntity)
				}
			}
		}
	}

	return nil
}
