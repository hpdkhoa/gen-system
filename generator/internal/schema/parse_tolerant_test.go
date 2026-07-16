// generator/internal/schema/parse_tolerant_test.go — v23.1: proves the
// tolerant parser recovers object-style fields, fenced output, and prose-
// wrapped JSON that the old strict ParseFile dropped to zero entities.
package schema

import "testing"

func TestTolerant_ObjectFields(t *testing.T) {
	// the shape the plan->schema prompt asks the model for (object fields)
	in := "```json\n{\"entities\":[{\"name\":\"Project\",\"fields\":{\"id\":\"int\",\"title\":\"string\"}},{\"name\":\"Task\",\"fields\":{\"id\":\"int\",\"status\":\"string\",\"due_date\":\"time\"}}]}\n```"
	sf, err := ParseFileChecked(in)
	if err != nil { t.Fatalf("unexpected err: %v", err) }
	if len(sf.Entities) != 2 { t.Fatalf("want 2 entities, got %d", len(sf.Entities)) }
	if sf.Entities[0].Name != "Project" || len(sf.Entities[0].Fields) != 2 {
		t.Fatalf("Project fields wrong: %+v", sf.Entities[0])
	}
	// order preserved (deterministic)
	if sf.Entities[0].Fields[0].Name != "id" || sf.Entities[0].Fields[1].Name != "title" {
		t.Fatalf("field order not preserved: %+v", sf.Entities[0].Fields)
	}
	if sf.Entities[1].Fields[2].Name != "due_date" || sf.Entities[1].Fields[2].Type != "time" {
		t.Fatalf("Task due_date wrong: %+v", sf.Entities[1].Fields)
	}
}

func TestTolerant_ArrayFields(t *testing.T) {
	in := `{"entities":[{"name":"User","fields":[{"name":"id","type":"int"},{"name":"email","type":"string"}]}]}`
	sf, err := ParseFileChecked(in)
	if err != nil { t.Fatalf("unexpected err: %v", err) }
	if len(sf.Entities) != 1 || len(sf.Entities[0].Fields) != 2 {
		t.Fatalf("array form wrong: %+v", sf.Entities)
	}
}

func TestTolerant_OldParseFileFailedThisBefore(t *testing.T) {
	// The real-world failures that produced zero entities were markdown fences
	// and surrounding prose (Go's json.Unmarshal is partial, so bare object
	// fields still yielded a named entity — but a fenced doc fails outright).
	fenced := "```json\n{\"entities\":[{\"name\":\"Project\",\"fields\":{\"id\":\"int\"}}]}\n```"
	if n := len(ParseFile(fenced).Entities); n != 0 {
		t.Fatalf("expected old strict ParseFile to drop fenced doc, got %d", n)
	}
	prose := `Sure! Here is the schema: {"entities":[{"name":"Project","fields":{"id":"int"}}]} — enjoy.`
	if n := len(ParseFile(prose).Entities); n != 0 {
		t.Fatalf("expected old strict ParseFile to drop prose-wrapped doc, got %d", n)
	}
	// new tolerant path recovers both
	for _, in := range []string{fenced, prose} {
		sf, err := ParseFileChecked(in)
		if err != nil || len(sf.Entities) != 1 {
			t.Fatalf("checked parse should recover %q: err=%v n=%d", in, err, len(sf.Entities))
		}
	}
}
