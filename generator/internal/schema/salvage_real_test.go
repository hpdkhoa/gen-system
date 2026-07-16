// generator/internal/schema/salvage_real_test.go — v23.1 (round 3): the EXACT
// deepseek-coder schema.raw.json from the user's run; asserts salvage now
// recovers BOTH User and Task (round 2 dropped Task).
package schema

import "testing"

// realBroken is the EXACT schema.raw.json deepseek-coder produced in the user's
// run (the one where round-2 salvage only recovered User, not Task).
const realBroken = "{\n  \"entities\": [\n    {\n      \"name\":\"User\",\n      \"fields\" : {\"userId\":\"INT\",\"username\":\"STRING\",\"password\":\"STRING\"}\n    },  \t    \t\t\t      \t\t\t\t\t\t\t\t\t         \t   \t     \t       \t\t        \t{\n       \t\"parentEntities\":[\"Project\"],              \t\t\t\t\t\t\t          \t\t\t\t\t\t\t\t                   }],                                                    ],                                                                  {\n            \"name\":\"Task\",             \n             \"fields\" : {\"task_id\":\"INT\",\"title\":\"STRING\",\"status\":\"BOOL\",\"due date\":\"TIME\"} ,  \t    \t\t\t     \t\t\t\t\t\t\t\t\t         \t \t   \t     \t       \t\t        \t{   \t\"parentEntities\":[\"Project\"]}\t}}]  }]}}}}}"

func TestSalvage_RealRun_RecoversBothEntities(t *testing.T) {
	sf := SalvageEntities(realBroken)
	if len(sf.Entities) != 2 {
		t.Fatalf("want 2 entities (User, Task), got %d: %+v", len(sf.Entities), sf.Entities)
	}
	got := map[string]int{}
	for _, e := range sf.Entities {
		got[e.Name] = len(e.Fields)
	}
	if got["User"] != 3 {
		t.Fatalf("User should have 3 fields, got %d", got["User"])
	}
	if got["Task"] != 4 {
		t.Fatalf("Task should have 4 fields, got %d", got["Task"])
	}
	// due date repaired to due_date
	for _, e := range sf.Entities {
		if e.Name == "Task" {
			ok := false
			for _, f := range e.Fields {
				if f.Name == "due_date" {
					ok = true
				}
			}
			if !ok {
				t.Fatalf("Task should have due_date after repair: %+v", e.Fields)
			}
		}
	}
}
