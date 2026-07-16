// generator/internal/ai/logic.go — v23.10 (Phase 4-B): LLM business-logic
// generation via a PROVIDER SEAM. The pipeline depends only on LogicProvider, so
// the whole generate→inject→compile→test path is verifiable offline with the
// deterministic replay provider; GEN_LOGIC=ollama opts into the live model
// (mirrors GEN_BACKEND=postgres). The model fills a SERVICE layer, never the CRUD
// handlers — a bad response fails one method (caught by the self-heal loop /
// dropped to a "not implemented" stub), never the build.
package ai

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gen-system/generator/internal/schema"
)

// Operation is one business operation the model proposes for an entity. The
// method signature is fixed by the generator; the model supplies Body (+Helpers).
type Operation struct {
	Name       string   `json:"name"`        // PascalCase Go method name, e.g. "Checkout"
	HTTPMethod string   `json:"http_method"` // GET|POST|PUT|DELETE
	Subpath    string   `json:"subpath"`     // "" or url segment, e.g. "checkout"
	Doc        string   `json:"doc"`         // one-line comment
	Body       string   `json:"body"`        // Go statements (method body)
	Helpers    []string `json:"helpers"`     // optional package-level helper funcs
}

// LogicProvider turns an idea + schema into named business operations.
// Implementations: ollamaLogicProvider (live model) and replayLogicProvider
// (deterministic, offline default).
type LogicProvider interface {
	Operations(idea string, e schema.Entity, all []schema.Entity) ([]Operation, error)
}

// OperationRepairer is an OPTIONAL capability: a provider that can take a single
// operation whose body failed to compile, plus the build error, and return a
// fixed operation. The engine type-asserts for this; providers that don't
// implement it (e.g. the deterministic replay provider) simply get no repair
// attempt and fall back to a stub. v23.23.
type OperationRepairer interface {
	RepairOperation(idea string, e schema.Entity, all []schema.Entity, op Operation, buildErr string) (Operation, error)
}

// SelectLogicProvider returns the live Ollama provider when GEN_LOGIC=ollama,
// otherwise the deterministic replay provider (default). Keeping the default
// offline means nothing requires a model unless explicitly opted in.
func SelectLogicProvider() LogicProvider {
	if strings.EqualFold(os.Getenv("GEN_LOGIC"), "ollama") {
		return ollamaLogicProvider{}
	}
	return ReplayProvider()
}

// operationsSchema is the JSON Schema enforced via Ollama structured output, so
// the model returns parseable operations (or the AskLLMJSON retry ladder runs).
var operationsSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "operations": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "name":        {"type": "string"},
          "http_method": {"type": "string", "enum": ["GET","POST","PUT","DELETE"]},
          "subpath":     {"type": "string"},
          "doc":         {"type": "string"},
          "body":        {"type": "string"},
          "helpers":     {"type": "array", "items": {"type": "string"}}
        },
        "required": ["name","http_method","body"]
      }
    }
  },
  "required": ["operations"]
}`)

// operationsEnvelope matches operationsSchema. An object-with-array envelope is
// used because Ollama structured output requires a top-level object.
type operationsEnvelope struct {
	Operations []Operation `json:"operations"`
}

// ───────────────────────── ollama provider (live, your box) ─────────────────

type ollamaLogicProvider struct{}

func (ollamaLogicProvider) Operations(idea string, e schema.Entity, all []schema.Entity) ([]Operation, error) {
	prompt := BuildLogicPrompt(idea, e, all)
	raw, err := AskLLMJSON(prompt, ModelFor(RoleCode), operationsSchema)
	if err != nil {
		return nil, fmt.Errorf("logic provider (ollama) for %s: %w", e.Name, err)
	}
	ops, err := ParseOperations(raw)
	if err != nil {
		return nil, fmt.Errorf("logic provider (ollama) parse for %s: %w", e.Name, err)
	}
	return ops, nil
}

// RepairOperation re-prompts the model to fix ONE operation whose body failed to
// compile, feeding back the exact go build error. Returns the repaired operation
// (the engine re-runs the compile gate on it). v23.23.
func (ollamaLogicProvider) RepairOperation(idea string, e schema.Entity, all []schema.Entity, op Operation, buildErr string) (Operation, error) {
	prompt := BuildLogicRepairPrompt(idea, e, all, op, buildErr)
	raw, err := AskLLMJSON(prompt, ModelFor(RoleCode), operationsSchema)
	if err != nil {
		return op, fmt.Errorf("repair (ollama) for %s.%s: %w", e.Name, op.Name, err)
	}
	fixed, err := ParseOperations(raw)
	if err != nil {
		return op, fmt.Errorf("repair (ollama) parse for %s.%s: %w", e.Name, op.Name, err)
	}
	// Prefer the operation with the matching name; else the first returned.
	for _, f := range fixed {
		if strings.EqualFold(f.Name, op.Name) {
			return f, nil
		}
	}
	if len(fixed) > 0 {
		f := fixed[0]
		f.Name = op.Name // keep the original name/route stable
		return f, nil
	}
	return op, fmt.Errorf("repair (ollama) for %s.%s: model returned no operation", e.Name, op.Name)
}

// ParseOperations decodes the model's JSON (envelope or bare array) into []Operation.
// Tolerant of a stray code fence, matching the rest of the v23.8 parse discipline.
func ParseOperations(raw string) ([]Operation, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	// Try the object envelope first.
	var env operationsEnvelope
	if err := json.Unmarshal([]byte(s), &env); err == nil && env.Operations != nil {
		return env.Operations, nil
	}
	// Fall back to a bare array.
	var arr []Operation
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		return arr, nil
	}
	return nil, fmt.Errorf("could not parse operations from model output")
}

// ───────────────────────── replay provider (offline default) ────────────────

// replayLogicProvider returns canned operations keyed by entity name. Used by
// tests and as the offline default, so the full generation path is deterministic
// without a model. Fixtures live in logic_fixtures.go.
type replayLogicProvider struct {
	fixtures map[string][]Operation
}

// ReplayProvider builds a replay provider from the built-in fixtures.
func ReplayProvider() LogicProvider {
	return replayLogicProvider{fixtures: builtinLogicFixtures()}
}

// ReplayProviderWith builds a replay provider from caller-supplied fixtures
// (used by tests that want to exercise specific shapes, incl. the "garbage" case).
func ReplayProviderWith(fixtures map[string][]Operation) LogicProvider {
	return replayLogicProvider{fixtures: fixtures}
}

func (p replayLogicProvider) Operations(idea string, e schema.Entity, all []schema.Entity) ([]Operation, error) {
	if ops, ok := p.fixtures[e.Name]; ok {
		return ops, nil
	}
	// No fixture for this entity → no extra operations (valid: CRUD only).
	return nil, nil
}
