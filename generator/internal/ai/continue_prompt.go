// generator/internal/ai/continue_prompt.go — v23.24 (task 4A): the model
// contract for `gen continue`. Given a free-text idea AND a compact summary of
// what the existing codebase already contains (entities, their fields, and the
// service operations already implemented), the model proposes a DELTA — only
// what should be ADDED or REMOVED — tagged per item so the deterministic merge
// layer can apply it precisely:
//
//	NEW       <Entity>{fields}    → a brand-new entity (additive)
//	ADD_FIELD <Entity>.<field>    → a new field on an existing entity (additive)
//	NEW_OP    <Entity>.<Op>       → a new business operation (drives service logic)
//	REMOVE    <Entity>            → drop an existing entity (destructive, gated)
//	REMOVE    <Entity>.<field>    → drop an existing field (destructive, gated)
//
// The model is explicitly told what already exists so it does NOT re-propose an
// entity/field that is already present. The deterministic post-filter in
// planner_v2 is the safety net if it does anyway.
//
// This is GENERATE-side only and must never be linked by understand-server
// (the archtest boundary asserts this).
package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ContinueChangeKind enumerates the tagged delta operations.
const (
	ChangeNew      = "NEW"       // new entity
	ChangeAddField = "ADD_FIELD" // new field on an existing entity
	ChangeNewOp    = "NEW_OP"    // new business operation on an entity
	ChangeRemove   = "REMOVE"    // remove an entity or a field
)

// ContinueChange is a single tagged item in the proposed delta.
//
//   - Kind == NEW       → Entity set, Fields set (the new entity's fields).
//   - Kind == ADD_FIELD → Entity + Field + FieldType set.
//   - Kind == NEW_OP    → Entity + Op set (a verb/operation name).
//   - Kind == REMOVE    → Entity set; Field set too if removing a field.
type ContinueChange struct {
	Kind      string            `json:"kind"`
	Entity    string            `json:"entity"`
	Fields    map[string]string `json:"fields,omitempty"`     // for NEW
	Field     string            `json:"field,omitempty"`      // for ADD_FIELD / REMOVE field
	FieldType string            `json:"field_type,omitempty"` // for ADD_FIELD
	Op        string            `json:"op,omitempty"`         // for NEW_OP
}

// ContinueDelta is the JSON envelope the model returns.
type ContinueDelta struct {
	Changes []ContinueChange `json:"changes"`
}

// continueDeltaSchema constrains the structured-output decode (mirrors the
// discipline used for entitiesSchema / operationsSchema elsewhere in this pkg).
var continueDeltaSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "changes": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "kind":       {"type": "string", "enum": ["NEW","ADD_FIELD","NEW_OP","REMOVE"]},
          "entity":     {"type": "string"},
          "fields":     {"type": "object"},
          "field":      {"type": "string"},
          "field_type": {"type": "string"},
          "op":         {"type": "string"}
        },
        "required": ["kind","entity"]
      }
    }
  },
  "required": ["changes"]
}`)

// BuildContinuePrompt assembles the delta prompt. existingSummary is the compact
// "what already exists" block produced by planner_v2.summarizeForPlanner.
func BuildContinuePrompt(idea, existingSummary string) string {
	return fmt.Sprintf(`You are evolving an EXISTING application. Below is exactly
what the application already contains, followed by a new instruction. Propose
ONLY the DELTA needed to satisfy the instruction — what to ADD or REMOVE — never
re-describe what already exists.

WHAT ALREADY EXISTS (the current application — do NOT re-propose any of this):
%s

NEW INSTRUCTION (what to add or change):
%s

Return ONLY this JSON object, no explanation:
{"changes": [
  {"kind": "NEW",       "entity": "Customer", "fields": {"name": "string", "email": "string"}},
  {"kind": "ADD_FIELD", "entity": "Order",    "field": "email", "field_type": "string"},
  {"kind": "NEW_OP",    "entity": "Order",    "op": "Refund"},
  {"kind": "REMOVE",    "entity": "Order",    "field": "legacyCode"}
]}

Rules for choosing changes:
- A NEW entity is a NOUN the instruction introduces that is STORED and is NOT
  already in the existing list above. If a noun already exists, do NOT emit NEW
  for it — add fields or operations to it instead.
- ADD_FIELD adds ONE field to an entity that already exists OR to a NEW entity
  you are also proposing. Do NOT ADD_FIELD a field the entity already has.
- NEW_OP is a VERB/ACTION the instruction asks for (e.g. "refund an order",
  "archive a project"). Tie it to the entity it acts ON. Do NOT emit NEW_OP for
  an operation that already exists in the entity's ops list above.
- REMOVE is ONLY for things the instruction explicitly says to delete/remove.
  Never REMOVE something just because the instruction doesn't mention it.
- entity names: PascalCase. field types: string, int, bool, float, time, uuid.
- If the instruction needs nothing new (already satisfied), return {"changes": []}.

JSON:`,
		strings.TrimSpace(existingSummary),
		strings.TrimSpace(idea),
	)
}

// ProposeContinueDelta runs the delta model call and parses the tagged result.
// Uses the planning model (RolePlan) since this is structural reasoning about
// the schema, not Go-body writing. Tolerant to a bare array or a fenced object.
func ProposeContinueDelta(idea, existingSummary string) (ContinueDelta, error) {
	prompt := BuildContinuePrompt(idea, existingSummary)
	resp, err := AskLLMJSON(prompt, ModelFor(RolePlan), continueDeltaSchema)
	if err != nil {
		return ContinueDelta{}, fmt.Errorf("continue delta AI call: %w", err)
	}
	return ParseContinueDelta(resp)
}

// ParseContinueDelta extracts a ContinueDelta from a model response. It accepts
// either the {"changes":[...]} envelope or a bare [...] array (some models drop
// the envelope), and tolerates surrounding prose / code fences.
func ParseContinueDelta(resp string) (ContinueDelta, error) {
	s := stripContinueFence(resp)

	// Envelope form first.
	if objStart := strings.Index(s, "{"); objStart != -1 {
		if objEnd := strings.LastIndex(s, "}"); objEnd > objStart {
			var d ContinueDelta
			if err := json.Unmarshal([]byte(s[objStart:objEnd+1]), &d); err == nil && d.Changes != nil {
				return d, nil
			}
		}
	}

	// Bare-array fallback.
	if arrStart := strings.Index(s, "["); arrStart != -1 {
		if arrEnd := strings.LastIndex(s, "]"); arrEnd > arrStart {
			var changes []ContinueChange
			if err := json.Unmarshal([]byte(s[arrStart:arrEnd+1]), &changes); err == nil {
				return ContinueDelta{Changes: changes}, nil
			}
		}
	}

	return ContinueDelta{}, fmt.Errorf("ParseContinueDelta: no parseable delta in response")
}

// stripContinueFence removes ```json ... ``` fences if present.
func stripContinueFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl != -1 {
			s = s[nl+1:]
		}
		if end := strings.LastIndex(s, "```"); end != -1 {
			s = s[:end]
		}
	}
	return strings.TrimSpace(s)
}
