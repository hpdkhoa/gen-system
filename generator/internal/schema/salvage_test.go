// generator/internal/schema/salvage_test.go — v23.1: repair+salvage unit
// tests (small fixture + trailing-comma repair).
package schema

import "testing"

// The actual malformed output from deepseek-coder in the user's run:
const userBroken = `{
  "entities": [
    {
      "name":"User",
      "fields" : {"userId":"INT","username":"STRING","password":"STRING"}
    },  	    			      									         	   	     	       		        	{
         	"parentEntities":["Project"],              							          								                   }],                                                    ],                                                                  {
              "name":"Task",             
               "fields" : {"task_id":"INT","title":"STRING","status":"BOOL","due date":"TIME"} ,  	    			     							         	 	   	     	       		
    }
  ]
}`

func TestSalvage_UserBrokenOutput(t *testing.T) {
	// strict/tolerant parse should fail on this
	if _, err := ParseFileChecked(userBroken); err == nil {
		t.Logf("note: ParseFileChecked unexpectedly parsed it")
	}
	// salvage should recover User and Task
	sf := SalvageEntities(userBroken)
	if len(sf.Entities) != 2 {
		t.Fatalf("want 2 salvaged entities, got %d: %+v", len(sf.Entities), sf.Entities)
	}
	names := map[string]int{}
	for _, e := range sf.Entities {
		names[e.Name] = len(e.Fields)
	}
	if names["User"] != 3 {
		t.Fatalf("User should have 3 fields, got %d", names["User"])
	}
	if names["Task"] != 4 {
		t.Fatalf("Task should have 4 fields, got %d", names["Task"])
	}
	// the spaced key "due date" must have been repaired to due_date
	var task Entity
	for _, e := range sf.Entities {
		if e.Name == "Task" {
			task = e
		}
	}
	found := false
	for _, f := range task.Fields {
		if f.Name == "due_date" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected due_date field after repair, got %+v", task.Fields)
	}
}

func TestRepair_TrailingCommas(t *testing.T) {
	in := `{"entities":[{"name":"A","fields":{"id":"int",}},]}`
	got := RepairJSON(in)
	if _, err := ParseFileChecked(got); err != nil {
		t.Fatalf("repaired JSON still fails: %v\nrepaired=%s", err, got)
	}
}
