package compiler

import (
	"github.com/hpdkhoa/gen-system/generator/internal/compiler/buildplan"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler/dag"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler/diff"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler/evolution"
)

// =====================================================
// DIFF
// =====================================================

func toDiffSchema(ir SchemaIR) diff.Schema {
	var entities []diff.Entity

	for _, e := range ir.Entities {
		var fields []diff.Field

		for _, f := range e.Fields {
			fields = append(fields, diff.Field{
				Name: f.Name,
				Type: f.Type,
			})
		}

		entities = append(entities, diff.Entity{
			Name:   e.Name,
			Fields: fields,
		})
	}

	return diff.Schema{
		Entities: entities,
	}
}

func ToDiffSchema(ir SchemaIR) diff.Schema {
	var entities []diff.Entity

	for _, e := range ir.Entities {
		var fields []diff.Field

		for _, f := range e.Fields {
			fields = append(fields, diff.Field{
				Name: f.Name,
				Type: f.Type,
			})
		}

		entities = append(entities, diff.Entity{
			Name:   e.Name,
			Fields: fields,
		})
	}

	return diff.Schema{
		Entities: entities,
	}
}

// =====================================================
// BUILD PLAN
// =====================================================

func toBuildPlanIR(ir SchemaIR) buildplan.SchemaIR {
	var entities []buildplan.EntityIR

	for _, e := range ir.Entities {
		// v26.58 (D072): pass the fields so buildplan.Plan can tell a changed
		// entity from an unchanged one.
		var fields []buildplan.FieldIR
		for _, f := range e.Fields {
			fields = append(fields, buildplan.FieldIR{Name: f.Name, Type: f.Type})
		}
		entities = append(entities, buildplan.EntityIR{
			Name:   e.Name,
			Fields: fields,
		})
	}

	return buildplan.SchemaIR{
		Entities: entities,
	}
}

// =====================================================
// DAG
// =====================================================

func toDagSchema(ir SchemaIR) dag.Schema {
	var entities []dag.Entity
	var relations []dag.Relation

	for _, e := range ir.Entities {
		entities = append(entities, dag.Entity{
			Name: e.Name,
		})
	}

	for _, r := range ir.Relations {
		relations = append(relations, dag.Relation{
			FromEntity: r.FromEntity,
			ToEntity:   r.ToEntity,
			Type:       r.Type,
		})
	}

	return dag.Schema{
		Entities:  entities,
		Relations: relations,
	}
}

// =====================================================
// EVOLUTION
// =====================================================

func toEvolutionSchema(ir SchemaIR) evolution.Schema {
	var entities []evolution.Entity
	var relations []evolution.Relation

	for _, e := range ir.Entities {
		var fields []evolution.Field

		for _, f := range e.Fields {
			fields = append(fields, evolution.Field{
				Name: f.Name,
				Type: f.Type,
			})
		}

		entities = append(entities, evolution.Entity{
			Name:   e.Name,
			Fields: fields,
		})
	}

	for _, r := range ir.Relations {
		relations = append(relations, evolution.Relation{
			FromEntity: r.FromEntity,
			ToEntity:   r.ToEntity,
			FromField:  r.FromField,
			Type:       r.Type,
		})
	}

	return evolution.Schema{
		Entities:  entities,
		Relations: relations,
	}
}

func toCompilerIR(s evolution.Schema) SchemaIR {
	var entities []EntityIR
	var relations []RelationIR

	for _, e := range s.Entities {
		var fields []FieldIR

		for _, f := range e.Fields {
			fields = append(fields, FieldIR{
				Name: f.Name,
				Type: f.Type,
			})
		}

		entities = append(entities, EntityIR{
			Name:   e.Name,
			Fields: fields,
		})
	}

	for _, r := range s.Relations {
		relations = append(relations, RelationIR{
			FromEntity: r.FromEntity,
			ToEntity:   r.ToEntity,
			FromField:  r.FromField,
			Type:       r.Type,
		})
	}

	return SchemaIR{
		Entities:  entities,
		Relations: relations,
	}
}
