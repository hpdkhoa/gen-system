// planner.go — v23.25 (task A): BuildPlanV2 runs validateEntities after entity
// generation — a deterministic noun-trace that drops entities absent from the
// idea (one re-prompt before dropping). See entity_validation.go.
// planner.go — v23.10 fix: ProgressHook streams the 5 planning steps to the
// caller (gen-server -> SSE) so long model calls no longer look like a stuck UI.
package planner_v2

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gen-system/generator/internal/ai"
	"gen-system/generator/internal/planner"
	"gen-system/generator/internal/schema"
)

// ─────────────────────────────────────────────
// ProgressHook, if set, receives planner step progress so the caller (gen-server)
// can stream it to the UI over SSE. Without it, planning is silent to the UI —
// which made long model calls look like the build was stuck. v23.10 fix.
var ProgressHook func(dot, message string)

func progress(dot, msg string) {
	fmt.Println(msg)
	if ProgressHook != nil {
		ProgressHook(dot, msg)
	}
}

// ─────────────────────────────────────────────
// BuildPlanV2 — 5-step multi-step reasoning pipeline.
// ─────────────────────────────────────────────
func BuildPlanV2(goal string) (planner.Plan, error) {
	if strings.TrimSpace(goal) == "" {
		return planner.Plan{}, fmt.Errorf("goal cannot be empty")
	}

	// ── STEP 1: Feature extraction ────────────────────────────────
	progress("cyan", "Step 1/5 — Extracting features from your idea (model call)…")
	features, err := ai.ExtractFeatures(goal)
	if err != nil || len(features) == 0 {
		fmt.Printf("     ⚠️  Feature extraction failed (%v) — using goal as single feature\n", err)
		features = []string{goal}
	}
	fmt.Printf("     ✅ %d features: %s\n", len(features), strings.Join(features, ", "))

	// ── STEP 2: Architecture design ───────────────────────────────
	progress("cyan", "Step 2/5 — Designing architecture (model call)…")
	archType, components, decisions, err := ai.PlanArchitecture(features)
	if err != nil {
		fmt.Printf("     ⚠️  Architecture design failed (%v) — using default\n", err)
		archType, components, decisions = "monolith", []string{"api", "database"}, []string{"default"}
	}
	arch := Architecture{Type: archType, Components: components, Decisions: decisions}
	fmt.Printf("     ✅ %s with components: %s\n", archType, strings.Join(components, ", "))

	// ── STEP 3: Entity generation ─────────────────────────────────
	progress("cyan", "Step 3/5 — Generating entities (model call — slowest step)…")
	rawSpecs, err := ai.PlanEntities(goal, features, archType, components)
	if err != nil || len(rawSpecs) == 0 {
		fmt.Printf("     ⚠️  Entity generation failed (%v) — entities will be empty\n", err)
	}
	specs := make([]EntitySpec, 0, len(rawSpecs))
	for _, r := range rawSpecs {
		specs = append(specs, EntitySpec{Name: r.Name, Fields: r.Fields})
	}
	// v23.25 (task A): deterministic noun-trace guard — drop entities the idea
	// never mentioned (with one re-prompt before dropping).
	specs = validateEntities(specs, goal, features, archType, components)
	fmt.Printf("     ✅ %d entities defined\n", len(specs))
	schemaEntities := entitySpecsToSchema(specs)

	// ── STEP 3b: Save proposed schema for EntityDiff (Fix 4) ──────
	saveProposedSchema(schemaEntities)

	// ── STEP 4: Build architecture-aware task graph ───────────────
	progress("cyan", "Step 4/5 — Building task graph…")
	tasks := buildAgentTasks(schemaEntities, arch)
	fmt.Printf("     ✅ %d tasks planned\n", len(tasks))

	// ── STEP 5: Assemble planner.Plan ─────────────────────────────
	progress("cyan", "Step 5/5 — Assembling plan…")
	planFeatures := stringsToFeatures(features)
	notes := []string{
		"Planner: V2 multi-step reasoning",
		fmt.Sprintf("Architecture: %s", archType),
		fmt.Sprintf("Components: %s", strings.Join(components, ", ")),
	}
	notes = append(notes, decisions...)

	plan := planner.Plan{
		Goal:     goal,
		Features: planFeatures,
		Entities: schemaEntities,
		Tasks:    tasks,
		Notes:    notes,
	}
	fmt.Printf("     ✅ Plan ready: %d entities, %d tasks\n", len(schemaEntities), len(tasks))
	return plan, nil
}

// ─────────────────────────────────────────────
// buildAgentTasks — architecture-aware task graph builder.
// Returns []planner.Task with full ID, Dependencies, Status fields
// so agent.RunFromPlan can topologically sort and execute them.
//
// Task graph (per entity):
//
//	migrate → model → repo → service → handler → validation
//	                                      ↓            ↓
//	                                 frontend      generate_types
//	                                 evolve_frontend (evolution only)
//	                                 mobile
//
// Global tasks (after all handlers):
//
//	generate_router → generate_env → generate_types_barrel → test
//
// ─────────────────────────────────────────────
func buildAgentTasks(entities []schema.Entity, arch Architecture) []planner.Task {
	var tasks []planner.Task

	// Pre-generation: auth setup (Phase 3 stub — executor skips gracefully)
	if containsStr(arch.Components, "auth") {
		tasks = append(tasks, planner.Task{
			ID: "generate_auth", Type: "generate_auth", Status: planner.StatusPending,
			Payload: map[string]interface{}{"strategy": "jwt", "source": "v2_architecture"},
		})
	}

	// Database migration
	if containsStr(arch.Components, "database") {
		tasks = append(tasks, planner.Task{
			ID: "migrate", Type: "migrate", Status: planner.StatusPending,
			Payload: map[string]interface{}{"dry": false, "source": "v2_architecture"},
		})
	}

	// Per-entity generation tasks
	for _, e := range entities {
		name := e.Name
		modelID := fmt.Sprintf("generate_model_%s", name)
		repoID := fmt.Sprintf("generate_repo_%s", name)
		serviceID := fmt.Sprintf("generate_service_%s", name)
		handlerID := fmt.Sprintf("generate_handler_%s", name)
		validationID := fmt.Sprintf("generate_validation_%s", name)
		frontendID := fmt.Sprintf("generate_frontend_%s", name)
		mobileID := fmt.Sprintf("generate_mobile_%s", name)
		typesID := fmt.Sprintf("generate_types_%s", name) // Phase 0.3

		modelDeps := []string{}
		if containsStr(arch.Components, "database") {
			modelDeps = append(modelDeps, "migrate")
		}
		if containsStr(arch.Components, "auth") && strings.EqualFold(name, "user") {
			modelDeps = append(modelDeps, "generate_auth")
		}

		tasks = append(tasks,
			planner.Task{ID: modelID, Type: "generate_model", EntityName: name,
				Dependencies: modelDeps, Status: planner.StatusPending,
				Payload: map[string]interface{}{"entity": name}},
			planner.Task{ID: repoID, Type: "generate_repo", EntityName: name,
				Dependencies: []string{modelID}, Status: planner.StatusPending,
				Payload: map[string]interface{}{"entity": name}},
			planner.Task{ID: serviceID, Type: "generate_service", EntityName: name,
				Dependencies: []string{repoID}, Status: planner.StatusPending,
				Payload: map[string]interface{}{"entity": name}},
			planner.Task{ID: handlerID, Type: "generate_handler", EntityName: name,
				Dependencies: []string{serviceID}, Status: planner.StatusPending,
				Payload: map[string]interface{}{"entity": name}},
			planner.Task{ID: validationID, Type: "generate_validation", EntityName: name,
				Dependencies: []string{modelID}, Status: planner.StatusPending,
				Payload: map[string]interface{}{"entity": name}},
			planner.Task{ID: frontendID, Type: "generate_frontend", EntityName: name,
				Dependencies: []string{handlerID}, Status: planner.StatusPending,
				Payload: map[string]interface{}{"entity": name}},
			planner.Task{ID: mobileID, Type: "generate_mobile", EntityName: name,
				Dependencies: []string{handlerID}, Status: planner.StatusPending,
				Payload: map[string]interface{}{"entity": name}},
			// Phase 0.3: TypeScript interface — depends on handler (schema is stable by then)
			planner.Task{ID: typesID, Type: "generate_types", EntityName: name,
				Dependencies: []string{handlerID}, Status: planner.StatusPending,
				Payload: map[string]interface{}{"entity": name}},
		)
	}

	if len(tasks) == 0 {
		return tasks
	}

	// Global tasks: router → env → types barrel → test
	var handlerIDs []string
	var typesIDs []string
	for _, t := range tasks {
		if t.Type == "generate_handler" {
			handlerIDs = append(handlerIDs, t.ID)
		}
		if t.Type == "generate_types" {
			typesIDs = append(typesIDs, t.ID)
		}
	}

	if len(handlerIDs) > 0 {
		tasks = append(tasks,
			planner.Task{ID: "generate_router", Type: "generate_router",
				Dependencies: handlerIDs, Status: planner.StatusPending},

			// Phase 1.2: .env + .env.example + docker-compose.yml
			// components payload tells executor what services to include
			planner.Task{ID: "generate_env", Type: "generate_env",
				Dependencies: []string{"generate_router"}, Status: planner.StatusPending,
				Payload: map[string]interface{}{
					"components": interfaceSlice(arch.Components),
				}},

			// Phase 0.3: barrel index — depends on all per-entity generate_types tasks
			planner.Task{ID: "generate_types_barrel", Type: "generate_types_barrel",
				Dependencies: append(typesIDs, "generate_router"), Status: planner.StatusPending},

			planner.Task{ID: "test", Type: "test",
				Dependencies: []string{"generate_router"}, Status: planner.StatusPending},
		)
	}

	// Optional: worker task (Phase 3 stub)
	if containsStr(arch.Components, "worker") {
		tasks = append(tasks, planner.Task{
			ID: "generate_worker", Type: "generate_worker",
			Dependencies: []string{"test"}, Status: planner.StatusPending,
			Payload: map[string]interface{}{"source": "v2_architecture"},
		})
	}
	return tasks
}

// interfaceSlice converts []string to []interface{} for Payload storage.
func interfaceSlice(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// ─────────────────────────────────────────────
// saveProposedSchema persists planned entities to .gen/proposed_schema.json
// so EntityDiff can compare proposed vs current IR. (Fix 4)
// ─────────────────────────────────────────────
func saveProposedSchema(entities []schema.Entity) {
	type proposedField struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	type proposedEntity struct {
		Name   string          `json:"name"`
		Fields []proposedField `json:"fields"`
	}
	var proposed []proposedEntity
	for _, e := range entities {
		pe := proposedEntity{Name: e.Name}
		for _, f := range e.Fields {
			pe.Fields = append(pe.Fields, proposedField{Name: f.Name, Type: f.Type})
		}
		proposed = append(proposed, pe)
	}
	data, err := json.MarshalIndent(proposed, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(".gen", 0755)
	_ = os.WriteFile(".gen/proposed_schema.json", data, 0644)
}

// ─────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────

// containsStr is defined in architecture.go — same package, no redecl needed.

func entitySpecsToSchema(specs []EntitySpec) []schema.Entity {
	var entities []schema.Entity
	for _, s := range specs {
		e := schema.Entity{Name: s.Name}
		for fname, ftype := range s.Fields {
			e.Fields = append(e.Fields, schema.Field{
				Name: capitalize(fname),
				Type: normalizeFieldType(ftype),
			})
		}
		entities = append(entities, e)
	}
	return entities
}

func normalizeFieldType(t string) string {
	switch strings.ToLower(t) {
	case "uuid", "string", "text", "varchar":
		return "string"
	case "int", "integer", "number", "bigint":
		return "int"
	case "bool", "boolean":
		return "bool"
	case "float", "double", "decimal":
		return "float"
	case "time", "timestamp", "datetime", "date":
		return "time"
	default:
		return "string"
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func stringsToFeatures(ss []string) []planner.Feature {
	var out []planner.Feature
	for _, s := range ss {
		out = append(out, planner.Feature{Name: s})
	}
	return out
}

func SavePlanV2(plan planner.Plan, path string) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// verifyDiffIntent logs the planned removals after SaveDiffIntent so
// LoadDiffIntent has a production caller within the planner_v2 package.
// Called at the end of BuildPlanV2 for traceability.
func verifyDiffIntent() {
	intent := LoadDiffIntent()
	removed := len(intent.RemoveEntities)
	fields := 0
	for _, fm := range intent.RemoveFields {
		fields += len(fm)
	}
	if removed > 0 || fields > 0 {
		fmt.Printf("  [planner] diff intent: %d entities + %d fields marked for removal\n",
			removed, fields)
	} else {
		fmt.Printf("  [planner] diff intent: additive build (no removals)\n")
	}
}
