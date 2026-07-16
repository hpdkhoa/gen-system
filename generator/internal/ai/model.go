// generator/internal/ai/model.go — v23.35: default models now match the installer
// (qwen3:14b for planning, qwen2.5-coder:14b for code). WHAT changed & WHY:
// the role-based selector (added v23.20) still fell back to a single
// "deepseek-coder" default when no env var was set, which contradicted what
// install.sh actually pulls (the qwen split) and what gen-system.env.example
// ships. That mismatch made the docs self-contradictory and meant a
// nothing-configured run reached for a model the box may not have. The env
// override contract is unchanged: OLLAMA_MODEL_PLAN / OLLAMA_MODEL_CODE win,
// then the legacy single OLLAMA_MODEL, then the per-role default below.
// v23.20: role-based model selection (original rationale retained):
// a single coder model was doing three different jobs — idea analysis,
// architecture, and code. Coder models are tuned to emit code, not to reason
// about whether "stock can't go negative" is an entity or a rule, which is what
// caused entity over-generation. The planner (analysis/architecture) and the
// code generator now use different models, each env-overridable.
package ai

import "os"

// DefaultPlanModel / DefaultCodeModel are the models install.sh pulls by
// default (a reasoning model for planning, a coder model for code). They are
// the last-resort fallbacks for their respective roles when no env var is set.
const (
	DefaultPlanModel = "qwen3:14b"
	DefaultCodeModel = "qwen2.5-coder:14b"
)

// DefaultModel is the legacy single-model default (the fallback for the
// back-compat Model() accessor and for OLLAMA_MODEL). It points at the code
// model, since the historic single-model use was code generation.
const DefaultModel = DefaultCodeModel

// ModelRole names the kind of work a model call is doing, so the right model
// can be selected per task.
type ModelRole int

const (
	// RolePlan: reading an idea, extracting features, designing architecture,
	// proposing entities. Wants reasoning + clean structured output.
	RolePlan ModelRole = iota
	// RoleCode: writing Go business-logic bodies. Wants a code model.
	RoleCode
)

// Model returns the configured generation model (legacy single-model accessor).
// Set OLLAMA_MODEL to override. Retained for back-compat and as the universal
// fallback for the role-based accessors below.
func Model() string {
	if m := os.Getenv("OLLAMA_MODEL"); m != "" {
		return m
	}
	return DefaultModel
}

// ModelFor returns the model to use for a given role.
//
// Resolution order (first non-empty wins):
//
//	RolePlan: OLLAMA_MODEL_PLAN → OLLAMA_MODEL → DefaultPlanModel (qwen3:14b)
//	RoleCode: OLLAMA_MODEL_CODE → OLLAMA_MODEL → DefaultCodeModel (qwen2.5-coder:14b)
//
// This means: users who set only OLLAMA_MODEL (the old behaviour) get that one
// model for everything, exactly as before. Users who want the recommended split
// set OLLAMA_MODEL_PLAN and OLLAMA_MODEL_CODE (a Qwen3 14B for planning and a
// Qwen2.5-Coder 14B for code — what install.sh pulls) and each phase uses the
// right tool. With nothing set at all, each role now falls back to its own
// installer default rather than one shared model.
func ModelFor(role ModelRole) string {
	switch role {
	case RolePlan:
		if m := os.Getenv("OLLAMA_MODEL_PLAN"); m != "" {
			return m
		}
		if m := os.Getenv("OLLAMA_MODEL"); m != "" {
			return m
		}
		return DefaultPlanModel
	case RoleCode:
		if m := os.Getenv("OLLAMA_MODEL_CODE"); m != "" {
			return m
		}
	}
	return Model()
}
