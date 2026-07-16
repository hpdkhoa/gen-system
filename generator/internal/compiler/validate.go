package compiler

import "errors"

func validateIR(ir SchemaIR) error {

	entityMap := map[string]bool{}

	for _, e := range ir.Entities {
		if entityMap[e.Name] {
			return errors.New("duplicate entity: " + e.Name)
		}
		entityMap[e.Name] = true

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
