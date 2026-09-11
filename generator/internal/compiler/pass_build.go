package compiler

import "github.com/hpdkhoa/gen-system/generator/internal/schema"

func buildInitialIR(s schema.SchemaFile) SchemaIR {
	var ir SchemaIR

	for _, e := range s.Entities {
		entity := EntityIR{
			Name: e.Name,
		}

		for _, f := range e.Fields {
			entity.Fields = append(entity.Fields, FieldIR{
				Name: f.Name,
				Type: f.Type,
			})
		}

		ir.Entities = append(ir.Entities, entity)
	}

	return ir
}
