package engine

import (
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func mapToDTO(e schema.Entity, rows []map[string]interface{}) interface{} {

	var result []map[string]interface{}

	for _, r := range rows {

		item := map[string]interface{}{}

		// base fields
		for _, f := range e.Fields {
			key := f.Name
			item[key] = r[key]
		}

		// relations
		for _, rel := range e.Relations {
			if rel.Type == "belongsTo" {
				nested := map[string]interface{}{}

				// naive prefix: user.email etc.
				for k, v := range r {
					if len(k) > len(rel.Target) && k[:len(rel.Target)] == rel.Target {
						nested[k] = v
					}
				}

				item[rel.Target] = nested
			}
		}

		result = append(result, item)
	}

	return result

}
