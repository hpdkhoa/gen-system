// generator/internal/schema/parse_tolerant.go (v23.1 planner fix).
// WHAT: tolerant schema JSON parsing. WHY: the planner's plan->schema prompt
// asks the model for "fields" as an object ({"name":"type"}), but Entity.Fields
// is a []Field array, so a strict json.Unmarshal failed and ParseFile silently
// returned zero entities ("generated schema has no entities"). This accepts
// either shape, strips markdown fences, and exposes the parse error so callers
// can diagnose instead of guessing.
package schema

import (
	"encoding/json"
	"fmt"
	"strings"
)

// tolerantEntity defers field decoding so we can accept either an array or an
// object for "fields" (see fieldsFromRaw).
type tolerantEntity struct {
	Name      string          `json:"name"`
	Fields    json.RawMessage `json:"fields"`
	Relations []Relation      `json:"relations"`
}

type tolerantFile struct {
	Entities []tolerantEntity `json:"entities"`
}

// stripFence removes a leading ```json / ``` fence and a trailing ``` if the
// model wrapped its output in markdown despite being told not to. It also trims
// to the outermost {...} so leading/trailing prose is dropped.
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// drop first line (``` or ```json) and any trailing fence
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		s = s[start : end+1]
	}
	return s
}

// fieldsFromRaw decodes a "fields" value that may be either:
//
//	[ {"name":"id","type":"int"}, ... ]            (canonical array)
//	{ "id":"int", "title":"string" }               (object the prompts ask for)
//
// Object form does not guarantee key order, so we keep insertion order stable
// by sorting? No. Go map order is random, which would make generation
// non-deterministic. To preserve determinism we decode the object into an
// ordered slice by re-parsing the raw bytes in textual order.
func fieldsFromRaw(raw json.RawMessage) ([]Field, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	switch trimmed[0] {
	case '[':
		var arr []Field
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, err
		}
		return arr, nil
	case '{':
		return orderedObjectFields(raw)
	default:
		return nil, fmt.Errorf("fields is neither array nor object: %.40s", trimmed)
	}
}

// orderedObjectFields walks the object in source order using a json.Decoder so
// field order (and therefore generated output) stays deterministic.
func orderedObjectFields(raw json.RawMessage) ([]Field, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	// consume opening '{'
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	var out []Field
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyTok.(string)
		// v26.59 (N05): a failed dec.Decode(&typ) had already consumed the value,
		// so the fallback decode met the next ',' or '}' and always failed. Read
		// the value once as raw bytes, then try the string and the generic value
		// from the same bytes.
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		var typ string
		if err := json.Unmarshal(val, &typ); err != nil {
			// value might be a nested object/number; capture as string-ish
			var any interface{}
			if err2 := json.Unmarshal(val, &any); err2 != nil {
				return nil, err
			}
			// v26.59 (N05): an object value like {"type":"int"} gave the type
			// text map[type:int], which no generator knows. Use its string
			// "type" key; keep %v only for other values.
			if obj, ok := any.(map[string]interface{}); ok {
				if s, ok := obj["type"].(string); ok {
					typ = s
				} else {
					typ = fmt.Sprintf("%v", any)
				}
			} else {
				typ = fmt.Sprintf("%v", any)
			}
		}
		out = append(out, Field{Name: key, Type: typ})
	}
	return out, nil
}

// ParseFileChecked is the error-returning, tolerant parser. ParseFile (in
// load.go) stays as the lossy convenience wrapper; this is what the planner
// uses so it can report exactly what went wrong.
func ParseFileChecked(input string) (SchemaFile, error) {
	clean := stripFence(input)
	var tf tolerantFile
	if err := json.Unmarshal([]byte(clean), &tf); err != nil {
		return SchemaFile{}, fmt.Errorf("schema JSON did not parse: %w", err)
	}
	var sf SchemaFile
	for _, te := range tf.Entities {
		fields, err := fieldsFromRaw(te.Fields)
		if err != nil {
			return SchemaFile{}, fmt.Errorf("entity %q fields: %w", te.Name, err)
		}
		sf.Entities = append(sf.Entities, Entity{
			Name:      te.Name,
			Fields:    fields,
			Relations: te.Relations,
		})
	}
	return sf, nil
}

// ParseFileTolerant is the non-error convenience form, used where the old
// lossy behaviour is fine but the dual field-shape support is still wanted.
func ParseFileTolerant(input string) SchemaFile {
	sf, _ := ParseFileChecked(input)
	return sf
}
