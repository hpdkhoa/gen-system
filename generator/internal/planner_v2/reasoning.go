package planner_v2

// ─────────────────────────────────────────────
// Reasoning is the full intermediate state built across all 5 steps.
// Passed implicitly through BuildPlanV2 — each step enriches it.
// ─────────────────────────────────────────────
type Reasoning struct {
	Goal         string
	Features     []string
	Architecture Architecture
	Entities     []EntitySpec
}

// Architecture captures the AI's structural decisions about the backend.
// Components drives conditional task inclusion in Step 4.
type Architecture struct {
	Type       string   `json:"type"`       // monolith | microservice
	Components []string `json:"components"` // api, database, auth, worker, cache, queue
	Decisions  []string `json:"decisions"`  // short rationale strings
}

// EntitySpec is the planner_v2 lightweight entity representation.
// Converted to schema.Entity via entitySpecsToSchema() before task graph.
type EntitySpec struct {
	Name   string            `json:"name"`
	Fields map[string]string `json:"fields"`
}

// FeatureList is the JSON envelope for Step 1 feature extraction response.
type FeatureList struct {
	Features []string `json:"features"`
}
