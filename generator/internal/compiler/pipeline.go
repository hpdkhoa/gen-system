package compiler

import (
	"encoding/json"
	"os"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler/buildplan"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler/dag"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler/diff"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler/evolution"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

const diffIntentPath = ".gen/diff_intent.json"

// loadDiffIntent reads the intent saved by planner_v2.SaveDiffIntent().
// Returns an empty intent (no deletions) when the file is absent or invalid.
// Lives here to avoid import cycles (compiler ← planner_v2 would be cyclic).
func loadDiffIntent() evolution.DiffIntent {
	empty := evolution.DiffIntent{
		RemoveEntities:  map[string]bool{},
		RemoveFields:    map[string]map[string]bool{},
		RemoveRelations: map[string]bool{},
	}
	data, err := os.ReadFile(diffIntentPath)
	if err != nil {
		return empty // file absent → first build, nothing to delete
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

// hasDeletions returns true when the intent has any non-empty removal set.
func hasDeletions(intent evolution.DiffIntent) bool {
	return len(intent.RemoveEntities) > 0 ||
		len(intent.RemoveFields) > 0 ||
		len(intent.RemoveRelations) > 0
}

// mergWithIntent applies MergeWithDiff if a deletion intent is saved,
// otherwise falls back to the safe additive Merge.
func mergeWithIntent(oldIR, newIR evolution.Schema) evolution.Schema {
	intent := loadDiffIntent()
	if hasDeletions(intent) {
		return evolution.MergeWithDiff(oldIR, newIR, intent)
	}
	return evolution.Merge(oldIR, newIR)
}

// =====================================================
// Compile v1: schema → IR
// =====================================================

func Compile(s schema.SchemaFile) (SchemaIR, error) {
	ir := SchemaIR{}
	ir = buildInitialIR(s)
	ir = inferRelations(ir)
	if err := validateIR(ir); err != nil {
		return SchemaIR{}, err
	}
	ir = finalizeIR(ir)
	return ir, nil
}

// =====================================================
// Compile v2: IR evolution + DAG ordering
// Now uses MergeWithDiff when a deletion intent is present.
// =====================================================

func CompileV2(oldIR SchemaIR, newIR SchemaIR) (SchemaIR, []string, error) {
	// PASS 1: evolution merge (deletion-aware if intent exists)
	merged := toCompilerIR(
		mergeWithIntent(
			toEvolutionSchema(oldIR),
			toEvolutionSchema(newIR),
		),
	)

	// PASS 2: validate
	if err := validateIR(merged); err != nil {
		return SchemaIR{}, nil, err
	}

	// PASS 3: dependency graph
	graph := dag.Build(toDagSchema(merged))
	order := dag.Order(graph)

	// PASS 4: finalize IR
	final := finalizeIR(merged)
	return final, order, nil
}

// =====================================================
// Compile v3: diff + build plan
// Now uses MergeWithDiff when a deletion intent is present.
// =====================================================

func CompileV3(oldIR, newIR SchemaIR) (diff.ChangeSet, buildplan.BuildPlan, error) {
	// PASS 1: evolution merge (deletion-aware if intent exists)
	merged := toCompilerIR(
		mergeWithIntent(
			toEvolutionSchema(oldIR),
			toEvolutionSchema(newIR),
		),
	)

	// PASS 2: diff engine
	cs := diff.Diff(
		toDiffSchema(oldIR),
		toDiffSchema(newIR),
	)

	// PASS 3: build plan (incremental compilation)
	plan := buildplan.Plan(
		toBuildPlanIR(oldIR),
		toBuildPlanIR(merged),
	)

	return cs, plan, nil
}
