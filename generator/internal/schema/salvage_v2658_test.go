// D042: SalvageEntities dropped every entity whose fields came in array form,
// because the "name" key of each field was taken as the next entity anchor and
// cut the region before the array closed.
package schema

import "testing"

const arrayFieldsBroken = `{"entities": [
 {"name":"User","fields":[{"name":"id","type":"int"},{"name":"email","type":"string"}]},,
 {"name":"Task","fields":{"id":"int","title":"string"}}
]}`

func TestSalvage_ArrayFormFieldsKeepEntity(t *testing.T) {
	if _, err := ParseFileChecked(arrayFieldsBroken); err == nil {
		t.Fatalf("fixture should fail the checked parse")
	}
	sf := SalvageEntities(arrayFieldsBroken)
	got := map[string]int{}
	for _, e := range sf.Entities {
		got[e.Name] = len(e.Fields)
	}
	if len(sf.Entities) != 2 || got["User"] != 2 || got["Task"] != 2 {
		t.Fatalf("want User(2) and Task(2), got %+v", sf.Entities)
	}
}
