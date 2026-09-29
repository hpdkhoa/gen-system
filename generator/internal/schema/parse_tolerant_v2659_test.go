// N05: orderedObjectFields decoded each value straight into a string. On a type
// error the decoder had already consumed the value, so the fallback decode met
// the next ',' or '}' and always failed. A fields object with a nested object or
// number value failed the whole entity, and salvage dropped it.
package schema

import "testing"

func TestTolerant_ObjectFieldsNestedAndNumberValues(t *testing.T) {
	in := `{"entities":[{"name":"Order","fields":{"id":{"type":"int"},"count":5,"note":"string"}}]}`
	sf, err := ParseFileChecked(in)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(sf.Entities) != 1 || len(sf.Entities[0].Fields) != 3 {
		t.Fatalf("want Order with 3 fields, got %+v", sf.Entities)
	}
	f := sf.Entities[0].Fields
	if f[0].Name != "id" || f[1].Name != "count" || f[2].Name != "note" {
		t.Fatalf("field order not preserved: %+v", f)
	}
	if f[0].Type != "int" || f[1].Type != "5" || f[2].Type != "string" {
		t.Fatalf("field types wrong: %+v", f)
	}
}

func TestSalvage_ObjectFieldsNestedValueKeepsEntity(t *testing.T) {
	broken := `{"entities": [
 {"name":"User","fields":{"id":"int","email":"string"}},,
 {"name":"Order","fields":{"id":{"type":"int"},"count":5}}
]}`
	sf := SalvageEntities(broken)
	got := map[string]int{}
	for _, e := range sf.Entities {
		got[e.Name] = len(e.Fields)
	}
	if len(sf.Entities) != 2 || got["User"] != 2 || got["Order"] != 2 {
		t.Fatalf("want User(2) and Order(2), got %+v", sf.Entities)
	}
}
