package schema

import (
	"strings"
)

// Parse builds a Schema from an inline "field:type,field:type" string.
// Entity / Field / Relation types are declared in schema_rel.go.
func Parse(name, input string) Schema {
	fields := []Field{
		{Name: "ID", Type: "int"},
	}

	if input != "" {
		parts := strings.Split(input, ",")
		for _, p := range parts {
			x := strings.Split(p, ":")
			if len(x) == 2 {
				fields = append(fields, Field{
					Name: capitalize(x[0]),
					Type: mapType(x[1]),
				})
			}
		}
	} else {
		fields = append(fields, Field{Name: "Name", Type: "string"})
	}

	return Schema{
		Name:   name,
		Fields: fields,
	}
}

// Schema is a lightweight named+fields view used by the CLI/core layer.
// For full entity definitions (with Relations) use Entity from schema_rel.go.
type Schema struct {
	Name   string
	Fields []Field
}

func mapType(t string) string {
	switch t {
	case "string":
		return "string"
	case "int":
		return "int"
	case "bool":
		return "bool"
	default:
		return "string"
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(string(s[0])) + s[1:]
}
