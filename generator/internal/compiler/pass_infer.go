package compiler

import "strings"

// simple v1 heuristics
func inferRelations(ir SchemaIR) SchemaIR {

	for i := range ir.Entities {
		for j := range ir.Entities {
			if i == j {
				continue
			}

			a := ir.Entities[i]
			b := ir.Entities[j]

			for fi := range a.Fields {
				f := a.Fields[fi]

				// heuristic: userId → User
				if strings.HasSuffix(f.Name, "Id") {
					ref := strings.TrimSuffix(f.Name, "Id")

					if equalsIgnoreCase(ref, b.Name) {
						ir.Entities[i].Fields[fi].IsForeign = true
						ir.Entities[i].Fields[fi].RefEntity = b.Name

						ir.Relations = append(ir.Relations, RelationIR{
							FromEntity: a.Name,
							ToEntity:   b.Name,
							FromField:  f.Name,
							ToField:    "id",
							Type:       "N:1",
						})
					}
				}
			}
		}
	}

	return ir
}

func equalsIgnoreCase(a, b string) bool {
	return strings.ToLower(a) == strings.ToLower(b)
}
