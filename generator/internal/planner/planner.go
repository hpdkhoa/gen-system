// generator/internal/planner/planner.go — v23.1 (round 2): BuildPlan saves raw
// model output to plan.raw.json / schema.raw.json (always, for inspection) and
// escalates parsing through ParseFileChecked → RepairJSON → SalvageEntities so
// a structurally noisy model response still yields a usable schema instead of
// dead-ending. On total failure it points at the saved raw file.
package planner

import (
	"encoding/json"
	"fmt"
	"os"

	"gen-system/generator/internal/ai"
	"gen-system/generator/internal/schema"
)

// ─────────────────────────────────────────────
// BuildPlan: goal string → full Plan struct
//
// Pipeline:
//   1. AI generates structured plan JSON (features + raw entities)
//   2. AI converts plan JSON → schema JSON
//   3. Schema is parsed into schema.SchemaFile
//   4. Task graph is built from the schema
//   5. Plan returned — ready for user review before execution
// ─────────────────────────────────────────────
func BuildPlan(goal string) (Plan, error) {
	if goal == "" {
		return Plan{}, fmt.Errorf("goal cannot be empty")
	}

	fmt.Println("🧠 Step 1/3 — Generating structured plan from goal...")

	// Step 1: goal → plan JSON (feature/entity/action breakdown)
	planJSON := ai.GeneratePlan(goal)
	if planJSON == "" || planJSON == "{}" {
		return Plan{}, fmt.Errorf("AI failed to generate a plan — is Ollama running?")
	}

	// Parse feature list from plan JSON for the Plan struct
	features := extractFeatures(planJSON)

	fmt.Println("🧠 Step 2/3 — Converting plan to schema...")

	// Save the raw plan for inspection (always, before anything can fail).
	saveRaw("plan.raw.json", planJSON)

	// Step 2: plan JSON → schema JSON
	schemaJSON := ai.GenerateSchemaFromPlan(planJSON)

	// Fallback: if plan→schema fails, generate schema directly from goal
	if schemaJSON == "" || schemaJSON == "{}" {
		fmt.Println("⚠️  Plan→schema fallback: generating schema directly from goal")
		schemaJSON = ai.GenerateSchema(goal)
	}

	// Always persist the raw model schema so it can be inspected even on failure.
	rawPath := saveRaw("schema.raw.json", schemaJSON)

	if schemaJSON == "" || schemaJSON == "{}" {
		return Plan{}, fmt.Errorf("AI failed to generate a schema (raw saved to %s)", rawPath)
	}

	// Step 3: parse schema, escalating through a repair/salvage ladder so a
	// structurally noisy model response (trailing commas, doubled brackets,
	// junk objects, tab storms — all seen from deepseek-coder) still yields a
	// usable schema instead of dead-ending. v23.1 planner fix (round 2).
	sf, perr := schema.ParseFileChecked(schemaJSON)
	if perr != nil || len(sf.Entities) == 0 {
		// (a) mechanical repair, then re-parse
		repaired := schema.RepairJSON(schemaJSON)
		if sf2, e2 := schema.ParseFileChecked(repaired); e2 == nil && len(sf2.Entities) > 0 {
			fmt.Println("ℹ️  recovered schema after JSON repair")
			saveRaw("schema.repaired.json", repaired)
			sf = sf2
		} else {
			// (b) salvage: scan out the well-formed entity objects, skip junk
			if sf3 := schema.SalvageEntities(schemaJSON); len(sf3.Entities) > 0 {
				fmt.Printf("ℹ️  salvaged %d entities from a malformed response\n", len(sf3.Entities))
				sf = sf3
			} else {
				// give up — but leave the artifact and a precise message
				if perr == nil {
					perr = fmt.Errorf("no usable entities found")
				}
				fmt.Println("⚠️  schema parse error:", perr)
				fmt.Println("    raw model output saved to:", rawPath)
				fmt.Println("    inspect it, fix by hand, then: gen apply", rawPath)
				return Plan{}, fmt.Errorf("could not parse generated schema (raw saved to %s): %w", rawPath, perr)
			}
		}
	}

	fmt.Printf("✅ Schema generated: %d entities\n", len(sf.Entities))
	fmt.Println("🧠 Step 3/3 — Building task execution graph...")

	// Step 4: build task dependency graph
	tasks := TaskGraphFromSchema(sf)

	plan := Plan{
		Goal:     goal,
		Features: features,
		Entities: sf.Entities,
		Tasks:    tasks,
	}

	return plan, nil
}

// saveRaw writes raw model output to the current working directory so it can
// always be inspected, even when parsing fails. Returns the path written (or
// the intended path on error). Best-effort: never fatal.
func saveRaw(name, content string) string {
	if content == "" {
		return name
	}
	if err := os.WriteFile(name, []byte(content), 0644); err != nil {
		fmt.Printf("⚠️  could not save %s: %v\n", name, err)
	} else {
		fmt.Printf("📄 raw model output saved: %s (%d bytes)\n", name, len(content))
	}
	return name
}

// ─────────────────────────────────────────────
// SavePlan persists the plan to disk for review.
// The user inspects / edits plan.json before calling ExecutePlan.
// ─────────────────────────────────────────────
func SavePlan(plan Plan, path string) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ─────────────────────────────────────────────
// LoadPlan reads a saved plan from disk.
// ─────────────────────────────────────────────
func LoadPlan(path string) (Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, fmt.Errorf("cannot read plan file %s: %w", path, err)
	}

	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return Plan{}, fmt.Errorf("invalid plan JSON: %w", err)
	}

	return plan, nil
}

// ─────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────

func extractFeatures(planJSON string) []Feature {
	var raw struct {
		Features []Feature `json:"features"`
	}
	_ = json.Unmarshal([]byte(planJSON), &raw)
	return raw.Features
}
