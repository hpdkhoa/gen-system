// generator/internal/ai/logic_prompt.go — v23.10 (Phase 4-B): builds the
// business-logic prompt from the user's IDEA text + the resolved schema. The
// idea drives WHAT the handlers should do (new), not just which entities exist.
package ai

import (
	"fmt"
	"strings"

	"gen-system/generator/internal/schema"
)

// BuildLogicPrompt renders the per-entity prompt. The fixed method signature is
// stated so the model only writes the body; the JSON-schema (operationsSchema)
// constrains the response shape.
func BuildLogicPrompt(idea string, e schema.Entity, all []schema.Entity) string {
	var fields []string
	for _, f := range e.Fields {
		fields = append(fields, fmt.Sprintf("%s %s", f.Name, f.Type))
	}
	var others []string
	for _, o := range all {
		if o.Name != e.Name {
			others = append(others, o.Name)
		}
	}
	otherList := strings.Join(others, ", ")
	if otherList == "" {
		otherList = "(none)"
	}

	// Positive hint: deterministically bind operation phrases from the idea to
	// THIS entity, so the model is pulled toward the right ops rather than only
	// told which to avoid. (Fixes Product receiving no ops because the prompt
	// was purely prohibitive.)
	bindings := bindOperationsToEntities(idea, all)
	var hint string
	if mine := bindings[e.Name]; len(mine) > 0 {
		hint = fmt.Sprintf(`
OPERATIONS THE IDEA IMPLIES FOR %s (you SHOULD implement these, naming each
sensibly in PascalCase): %s
`, e.Name, strings.Join(mine, "; "))
	} else {
		hint = fmt.Sprintf(`
The idea implies NO operation whose subject is %s beyond CRUD. Return
{"operations": []} unless you are certain an operation acts primarily on %s.
`, e.Name, e.Name)
	}

	return fmt.Sprintf(`You are generating Go business-logic methods for one entity in an HTTP+store app.

APP IDEA (the product the user described):
%s

ENTITY: %s
FIELDS: %s
OTHER ENTITIES: %s

The app already has working CRUD (create/read/update/delete) and a thread-safe
store with methods: List() []%s, Get(id int) (%s, bool),
Create(m %s) %s, Update(id int, m %s) (%s, bool), Delete(id int) bool.
A service struct 's' holds the store as 's.store'.

Propose the BUSINESS OPERATIONS this entity needs BEYOND CRUD, implied by the app
idea. For each, write the Go method body (statements only — no func signature).
Use only the Go standard library and s.store. Do not import or invent other packages.

CRITICAL — only propose operations whose SUBJECT is %s itself:
- The operation must act primarily on a %s record (loaded via s.store.Get(id)).
- If a verb in the idea is about a DIFFERENT entity, do NOT propose it here.
  Example: "checkout an order" belongs on Order, NOT on Product, Customer, or
  OrderItem. "adjust stock" belongs on Product. "apply a discount to an order"
  belongs on Order.
- If the idea implies NO operation whose subject is %s, return {"operations": []}.
  An empty list is the correct, expected answer for entities that only need CRUD.
- Do not invent generic operations (Checkout, ApplyDiscount, …) for an entity
  just because the app has them elsewhere.
%s
Return ONLY JSON matching the schema: an object {"operations": [ ... ]} where each
operation has:
  name        PascalCase Go method name (e.g. "Checkout", "ApplyDiscount")
  http_method one of GET, POST, PUT, DELETE
  subpath     "" or a short url segment (e.g. "checkout")
  doc         one short sentence
  body        Go statements implementing it. The method has signature:
                func (s *%sService) <name>(id int, in map[string]any) (any, error)
              so you may use id and in. Return (value, nil) or (nil, error).

              CRITICAL — reading the payload: JSON numbers ALWAYS decode as
              float64, so in["x"].(int) PANICS at runtime. NEVER type-assert
              payload values. Use the provided helpers instead (already in scope):
                payloadInt(in, "amount")    -> (int, ok bool)
                payloadFloat(in, "percent") -> (float64, ok bool)
                payloadString(in, "status") -> (string, ok bool)
              e.g.  amt, ok := payloadInt(in, "amount")
                    if !ok { return nil, errors.New("amount is required") }
  helpers     optional array of complete package-level helper funcs (or [])

Rules: every code path returns (value, nil) or (nil, error). No panics. No TODOs.
ENFORCE PRECONDITIONS the idea states: if the idea says an action is only allowed
in a certain state (e.g. "check out only if unpaid", "stock can never go
negative"), the body MUST load the record, check that condition, and return an
error if it is violated BEFORE mutating. A checkout that doesn't reject an
already-paid order, or a stock adjustment that allows a negative result, is wrong.
When a precondition is about a string status, DON'T require one exact spelling of
the "not yet done" state — a new record may have status "open", "new", "pending",
or "". Guard on the TERMINAL state instead: e.g. for checkout, reject only when
status == "paid" (if order.Status == "paid" { return error }), rather than
requiring status == "unpaid". This avoids rejecting valid records that simply use
a different initial value.
If the idea implies no extra operations, return {"operations": []}.`,
		strings.TrimSpace(idea),
		e.Name,
		strings.Join(fields, ", "),
		otherList,
		e.Name, e.Name, e.Name, e.Name, e.Name, e.Name, // 6× store method types
		e.Name, e.Name, e.Name, // CRITICAL block (SUBJECT is / record / NO operation)
		hint,                   // v23.24 positive op-binding hint
		e.Name,                 // signature %sService
	) + styleAddendum(e.Name, bindings[e.Name])
}

// StyleAddendum (v26.33) returns extra prompt text for one entity: the team
// style rules and reference code, when a style profile is active. Set by the
// agent (SetStyle); nil means byte-identical prompts to v26.32. The hints are
// the operation phrases bound to the entity, so exemplar retrieval can pick
// team functions with matching verbs. Package-level seam, like SetLogicRun.
var StyleAddendum func(entity string, hints []string) string

func styleAddendum(entity string, hints []string) string {
	if StyleAddendum == nil {
		return ""
	}
	return StyleAddendum(entity, hints)
}

// BuildRepairPrompt asks the model to FIX one operation whose body failed to
// compile, giving it the exact build error. The same fixed signature + field
// context as the original prompt is restated so the model has what it needs.
// v23.23.
func BuildLogicRepairPrompt(idea string, e schema.Entity, all []schema.Entity, op Operation, buildErr string) string {
	var fields []string
	for _, f := range e.Fields {
		fields = append(fields, fmt.Sprintf("%s %s", f.Name, f.Type))
	}

	return fmt.Sprintf(`You wrote a Go business-logic method for an HTTP+store app,
but it does NOT compile. Fix it. Return ONLY the corrected operation as JSON.

APP IDEA:
%s

ENTITY: %s
FIELDS: %s

The method has this EXACT signature (do not change it):
    func (s *%sService) %s(id int, in map[string]any) (any, error)

The store offers: List() []%s, Get(id int) (%s, bool),
Create(m %s) %s, Update(id int, m %s) (%s, bool), Delete(id int) bool.
Access it as s.store. Get/Update return (value, ok bool) — NOT (value, error).

The payload 'in' is map[string]any. JSON numbers decode as float64, so read a
numeric field like: amount := int(in["amount"].(float64)) with a comma-ok guard,
e.g. raw, ok := in["amount"].(float64); if !ok { return nil, errors.New("amount required") }.
Use only the Go standard library and s.store. Every path returns (value, nil) or
(nil, error). No panics. No TODOs.

YOUR PREVIOUS BODY:
%s

THE COMPILER ERROR:
%s

Return ONLY this JSON (one operation, the fixed one):
{"operations": [{"name": %q, "http_method": %q, "subpath": %q, "doc": %q, "body": "…fixed Go statements…", "helpers": []}]}`,
		strings.TrimSpace(idea),
		e.Name,
		strings.Join(fields, ", "),
		e.Name, op.Name,
		e.Name, e.Name,
		e.Name, e.Name, e.Name, e.Name,
		strings.TrimSpace(op.Body),
		strings.TrimSpace(buildErr),
		op.Name, op.HTTPMethod, op.Subpath, op.Doc,
	) + styleAddendum(e.Name, []string{op.Name})
}
