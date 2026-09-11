// conflicts_test.go — v25.8: pins the push-back logic (moved from cli). Internal
// to planner_v2, so it exercises DetectConflicts + EntityIsReadDominant directly.
package planner_v2

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func mkExisting() ExistingState {
	return ExistingState{
		Entities: map[string][]string{
			"Report":  {"id:int", "title:string"},
			"Account": {"id:int", "balance:int"},
		},
		EntityOrder: []string{"Account", "Report"},
		Ops: map[string][]string{
			"Report":  {"GetReport", "ListReports"}, // read-only service
			"Account": {"GetAccount", "UpdateAccount"},
		},
	}
}

func TestDetectConflicts_WriteOnReadOnlyService(t *testing.T) {
	res := ContinueResult{NewOps: map[string][]string{"Report": {"DeleteReport"}}}
	cs := DetectConflicts(mkExisting(), res)
	if len(cs) != 1 || cs[0].Kind != ConflictWriteOnReadOnly {
		t.Fatalf("want 1 write-on-read conflict, got %+v", cs)
	}
}

func TestDetectConflicts_WriteOnMixedService_NoFlag(t *testing.T) {
	res := ContinueResult{NewOps: map[string][]string{"Account": {"CloseAccount"}}}
	if cs := DetectConflicts(mkExisting(), res); len(cs) != 0 {
		t.Fatalf("mixed service should not flag, got %+v", cs)
	}
}

func TestDetectConflicts_DuplicateEntity(t *testing.T) {
	res := ContinueResult{
		ChangedSet: map[string]bool{"account": true},
		Merged: schema.SchemaFile{Entities: []schema.Entity{
			{Name: "account", Fields: []schema.Field{{Name: "id", Type: "int"}}},
		}},
	}
	cs := DetectConflicts(mkExisting(), res)
	if len(cs) != 1 || cs[0].Kind != ConflictDuplicateEntity {
		t.Fatalf("want duplicate-entity conflict, got %+v", cs)
	}
}

func TestDetectConflicts_Removal(t *testing.T) {
	res := ContinueResult{Removed: map[string][]string{"Report": {"*"}}}
	cs := DetectConflicts(mkExisting(), res)
	if len(cs) != 1 || cs[0].Kind != ConflictRemoval {
		t.Fatalf("want removal conflict, got %+v", cs)
	}
}

func TestDetectConflicts_CleanDelta(t *testing.T) {
	res := ContinueResult{
		ChangedSet: map[string]bool{"Invoice": true},
		Merged: schema.SchemaFile{Entities: []schema.Entity{
			{Name: "Invoice", Fields: []schema.Field{{Name: "id", Type: "int"}}},
		}},
		NewOps: map[string][]string{"Account": {"GetAccountHistory"}},
	}
	if cs := DetectConflicts(mkExisting(), res); len(cs) != 0 {
		t.Fatalf("clean delta should have no conflicts, got %+v", cs)
	}
}

func TestEntityIsReadDominant(t *testing.T) {
	if !EntityIsReadDominant([]string{"GetX", "ListX"}) {
		t.Error("all-reads should be read-dominant")
	}
	if EntityIsReadDominant([]string{"GetX", "UpdateX"}) {
		t.Error("has a write → not read-dominant")
	}
	if EntityIsReadDominant(nil) {
		t.Error("no ops → cannot judge → not read-dominant")
	}
}
