// generator/internal/ai/planner_prompts.go — v23.1: CompileIdea resolves the
// model via Model() instead of hard-coded qwen2.5-coder:7b.
package ai

// ─────────────────────────────────────────────
// Planner AI functions
//
// These functions own the COMPLETE responsibility for:
//   - building the prompt string
//   - calling AskLLM
//   - parsing the raw LLM response
//   - returning primitive Go types (no planner_v2 types imported)
//
// planner_v2 calls these and maps the results to its own domain types.
// ─────────────────────────────────────────────

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ── STEP 1: Feature extraction ───────────────────────────────────

// ExtractFeatures extracts 3–7 named product features from a goal string.
// Returns a plain string slice — no planner_v2 dependency.
func ExtractFeatures(goal string) ([]string, error) {
	prompt := fmt.Sprintf(`You are a product analyst. Extract ONLY the backend
features the idea ACTUALLY states. Do not invent features that a similar product
"usually" has.

IDEA: %s

Return ONLY this JSON, no explanation:
{"features": ["feature one", "feature two"]}

Rules:
- List ONLY features grounded in the idea's own words. If the idea describes one
  thing, return one feature. Never pad the list to reach a count.
- Each feature: 2–5 words, noun phrase, backend-focused.
- Hard ceiling of 7, but most ideas have 1–4. Fewer is better than invented.

JSON:`, strings.TrimSpace(goal))

	resp, err := AskLLMWithModelRaw(prompt, ModelFor(RolePlan))
	if err != nil {
		return nil, fmt.Errorf("ExtractFeatures AI call: %w", err)
	}

	start := strings.Index(resp, "{")
	end := strings.LastIndex(resp, "}")
	if start == -1 || end == -1 {
		return nil, fmt.Errorf("ExtractFeatures: no JSON in response")
	}

	var result struct {
		Features []string `json:"features"`
	}
	if err := json.Unmarshal([]byte(resp[start:end+1]), &result); err != nil {
		return nil, fmt.Errorf("ExtractFeatures parse: %w", err)
	}
	return result.Features, nil
}

// ── STEP 2: Architecture design ──────────────────────────────────

// PlanArchitecture designs backend architecture from extracted features.
// Returns three parallel slices — no Architecture struct imported here.
//
//	archType   — "monolith" or "microservice"
//	components — e.g. ["api", "database", "auth"]
//	decisions  — brief rationale strings
func PlanArchitecture(features []string) (archType string, components []string, decisions []string, err error) {
	if len(features) == 0 {
		return "monolith", []string{"api", "database"}, []string{"default"}, nil
	}

	prompt := fmt.Sprintf(`You are a backend architect.

FEATURES:
- %s

Design the backend architecture. Return ONLY this JSON, no explanation:
{
  "type": "monolith",
  "components": ["api", "database"],
  "decisions": ["reason 1", "reason 2"]
}

Rules:
- type: monolith OR microservice
- components: choose from: api, database, auth, worker, cache, queue
- decisions: max 3 short strings explaining why

JSON:`, strings.Join(features, "\n- "))

	resp, callErr := AskLLMWithModelRaw(prompt, ModelFor(RolePlan))
	if callErr != nil {
		return "monolith", []string{"api", "database"}, nil,
			fmt.Errorf("PlanArchitecture AI call: %w", callErr)
	}

	start := strings.Index(resp, "{")
	end := strings.LastIndex(resp, "}")
	if start == -1 || end == -1 {
		return "monolith", []string{"api", "database"}, nil,
			fmt.Errorf("PlanArchitecture: no JSON in response")
	}

	var result struct {
		Type       string   `json:"type"`
		Components []string `json:"components"`
		Decisions  []string `json:"decisions"`
	}
	if parseErr := json.Unmarshal([]byte(resp[start:end+1]), &result); parseErr != nil {
		return "monolith", []string{"api", "database"}, nil,
			fmt.Errorf("PlanArchitecture parse: %w", parseErr)
	}

	if result.Type == "" {
		result.Type = "monolith"
	}
	if len(result.Components) == 0 {
		result.Components = []string{"api", "database"}
	}
	// Always ensure database is present
	hasDB := false
	for _, c := range result.Components {
		if strings.EqualFold(c, "database") {
			hasDB = true
			break
		}
	}
	if !hasDB {
		result.Components = append(result.Components, "database")
	}

	return result.Type, result.Components, result.Decisions, nil
}

// ── STEP 3: Entity generation ─────────────────────────────────────

// RawEntitySpec is the primitive entity type returned by PlanEntities.
// planner_v2 maps this to its own EntitySpec type with additional logic.
type RawEntitySpec struct {
	Name   string            `json:"name"`
	Fields map[string]string `json:"fields"`
}

// PlanEntities generates database entity specs from features + architecture.
// Takes only primitives — no planner_v2 types in the signature.
//
//	archType   — "monolith" | "microservice"
//	components — e.g. ["api", "database", "auth"]
//
// PlanEntities generates database entity specs from the ORIGINAL idea plus the
// derived features + architecture. v23.20: the idea text is now passed in and is
// the primary anchor — features alone are abstract ("Inventory Management") and
// led the model to autocomplete a generic schema (Order, Product, Customer,
// Payment, Review, …) up to the old "max 8" ceiling. The prompt now insists on
// only the nouns the idea names, and that operations/rules are NOT entities.
func PlanEntities(goal string, features []string, archType string, components []string) ([]RawEntitySpec, error) {
	if len(features) == 0 {
		return nil, fmt.Errorf("PlanEntities: no features provided")
	}

	needsAuth := false
	for _, c := range components {
		if strings.EqualFold(c, "auth") {
			needsAuth = true
			break
		}
	}

	authNote := ""
	if needsAuth {
		authNote = "\n- Auth is required, so a User entity (email string, password_hash string) IS justified even if the idea is terse about it."
	}

	prompt := fmt.Sprintf(`You are a database schema designer. Design entities for
EXACTLY the product described below — not a generic version of it.

ORIGINAL IDEA (the source of truth):
%s

DERIVED FEATURES (for reference only; the IDEA above governs):
- %s

ARCHITECTURE: %s with components: %s

Return ONLY this JSON array, no explanation:
[
  {"name": "EntityName", "fields": {"field_name": "type"}}
]

How to decide what is an entity:
- An entity is a NOUN the idea names as something that is STORED and has its own
  records (e.g. "an Order has a total and status" → Order is an entity).
- A VERB or ACTION is NOT an entity. "check out", "apply a discount", "adjust
  stock" are operations on an entity, not entities themselves. Do NOT create a
  Discount, Checkout, or Payment entity from a verb.
- A RULE is NOT an entity. "stock can't go negative" is a constraint on a field,
  not a new entity.
- A FIELD is NOT an entity. total, status, stock are fields on an entity.
- A value that is only an INPUT to an operation is NOT necessarily a stored
  field. "apply a discount percentage to an order" means Order needs an
  ApplyDiscount operation that takes a percentage argument; it does NOT
  automatically mean Order stores a discountPercentage field. Only add a field
  if the idea says the value is PART OF the record's stored state.
- Do NOT add entities the idea does not mention just because similar apps have
  them (no Customer, Review, Payment, OrderItem, AuditLog unless the idea says so).%s

Rules:
- entity names: PascalCase. field types: string, int, bool, float, time, uuid.
- always include an "id" field.
- Most ideas need 1–4 entities. Use the FEWEST that faithfully model the idea.
  A hard ceiling of 8 exists only as a safety limit — it is NOT a target.
- max 10 fields each.

JSON:`,
		strings.TrimSpace(goal),
		strings.Join(features, "\n- "),
		archType,
		strings.Join(components, ", "),
		authNote,
	)

	resp, err := AskLLMWithModelRaw(prompt, ModelFor(RolePlan))
	if err != nil {
		return nil, fmt.Errorf("PlanEntities AI call: %w", err)
	}

	start := strings.Index(resp, "[")
	end := strings.LastIndex(resp, "]")
	if start == -1 || end == -1 {
		return nil, fmt.Errorf("PlanEntities: no JSON array in response")
	}

	var specs []RawEntitySpec
	if err := json.Unmarshal([]byte(resp[start:end+1]), &specs); err != nil {
		return nil, fmt.Errorf("PlanEntities parse: %w", err)
	}
	return specs, nil
}

// ── STEP 0: Compile idea → schema JSON (moved from compiler/idea/) ──

// CompileIdea converts a product idea string into a schema JSON string.
// Previously lived in compiler/idea/compiler.go — moved here because
// it is fundamentally an AI call, not a compiler transformation.
func CompileIdea(input string) (string, error) {
	prompt := `You are a schema compiler.

Convert the following product idea into a STRICT JSON schema.

RULES:
- Output only JSON
- Entities must include fields
- Include foreign keys explicitly when obvious
- Use PascalCase for entities
- Use snake or camel case for fields consistently

IDEA:
` + input

	return AskLLMWithModelRaw(prompt, ModelFor(RolePlan))
}
