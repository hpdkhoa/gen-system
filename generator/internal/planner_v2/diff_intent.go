package planner_v2

import (
	"encoding/json"
	"os"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler/evolution"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

const intentPath = ".gen/diff_intent.json"

// ComputeDiffIntent compares the proposed entities from BuildPlanV2
// against the current IR to produce a DiffIntent that signals what
// should be explicitly removed during the next MergeWithDiff call.
//
// This is how "Remove the 'age' field" instructions from the AI
// actually reach the compiler: through an explicit removal signal.
func ComputeDiffIntent(proposed []schema.Entity) evolution.DiffIntent {
	intent := evolution.DiffIntent{
		RemoveEntities:  map[string]bool{},
		RemoveFields:    map[string]map[string]bool{},
		RemoveRelations: map[string]bool{},
	}

	ir, err := compiler.LoadPreviousIR()
	if err != nil || len(ir.Entities) == 0 {
		// No current IR: first build, nothing to remove.
		return intent
	}

	// Build lookup of proposed entity names
	proposedNames := map[string]bool{}
	proposedFields := map[string]map[string]bool{}
	for _, e := range proposed {
		proposedNames[e.Name] = true
		proposedFields[e.Name] = map[string]bool{}
		for _, f := range e.Fields {
			proposedFields[e.Name][f.Name] = true
		}
	}

	for _, currentEntity := range ir.Entities {
		name := currentEntity.Name

		// Entity in current IR but not in proposed → mark for removal
		if !proposedNames[name] {
			intent.RemoveEntities[name] = true
			continue
		}

		// Field in current entity but not in proposed → mark for removal
		pFields := proposedFields[name]
		for _, f := range currentEntity.Fields {
			if !pFields[f.Name] {
				if intent.RemoveFields[name] == nil {
					intent.RemoveFields[name] = map[string]bool{}
				}
				intent.RemoveFields[name][f.Name] = true
			}
		}
	}

	return intent
}

// SaveDiffIntent persists the intent to .gen/diff_intent.json so
// the compiler pipeline can load it during the next build.
func SaveDiffIntent(intent evolution.DiffIntent) {
	data, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(".gen", 0755)
	_ = os.WriteFile(intentPath, data, 0644)
}

// LoadDiffIntent loads a previously saved DiffIntent.
// Returns an empty intent if the file doesn't exist.
func LoadDiffIntent() evolution.DiffIntent {
	empty := evolution.DiffIntent{
		RemoveEntities:  map[string]bool{},
		RemoveFields:    map[string]map[string]bool{},
		RemoveRelations: map[string]bool{},
	}
	data, err := os.ReadFile(intentPath)
	if err != nil {
		return empty
	}
	var intent evolution.DiffIntent
	if json.Unmarshal(data, &intent) != nil {
		return empty
	}
	if intent.RemoveEntities == nil {
		intent.RemoveEntities = map[string]bool{}
	}
	if intent.RemoveFields == nil {
		intent.RemoveFields = map[string]map[string]bool{}
	}
	if intent.RemoveRelations == nil {
		intent.RemoveRelations = map[string]bool{}
	}
	return intent
}
