// generator/internal/ai/model_role_test.go — v23.35: all-unset now yields the
// per-role installer defaults (qwen3:14b plan / qwen2.5-coder:14b code) instead
// of one shared default. Other cases (legacy single model, role override,
// partial split) are unchanged and still locked.
package ai

import (
	"os"
	"testing"
)

func clearModelEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"OLLAMA_MODEL", "OLLAMA_MODEL_PLAN", "OLLAMA_MODEL_CODE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func TestModelFor_AllUnset_FallsBackToRoleDefaults(t *testing.T) {
	clearModelEnv(t)
	if got := ModelFor(RolePlan); got != DefaultPlanModel {
		t.Errorf("RolePlan with nothing set: got %q want %q", got, DefaultPlanModel)
	}
	if got := ModelFor(RoleCode); got != DefaultCodeModel {
		t.Errorf("RoleCode with nothing set: got %q want %q", got, DefaultCodeModel)
	}
}

func TestModelFor_LegacySingleModel_AppliesToBothRoles(t *testing.T) {
	clearModelEnv(t)
	t.Setenv("OLLAMA_MODEL", "deepseek-coder-v2:16b")
	// Old behaviour preserved: one model drives every role.
	if got := ModelFor(RolePlan); got != "deepseek-coder-v2:16b" {
		t.Errorf("RolePlan should fall back to OLLAMA_MODEL: got %q", got)
	}
	if got := ModelFor(RoleCode); got != "deepseek-coder-v2:16b" {
		t.Errorf("RoleCode should fall back to OLLAMA_MODEL: got %q", got)
	}
}

func TestModelFor_RoleSpecific_OverridesLegacy(t *testing.T) {
	clearModelEnv(t)
	t.Setenv("OLLAMA_MODEL", "fallback-model")
	t.Setenv("OLLAMA_MODEL_PLAN", "qwen3:14b")
	t.Setenv("OLLAMA_MODEL_CODE", "qwen2.5-coder:14b")
	if got := ModelFor(RolePlan); got != "qwen3:14b" {
		t.Errorf("RolePlan should use OLLAMA_MODEL_PLAN: got %q", got)
	}
	if got := ModelFor(RoleCode); got != "qwen2.5-coder:14b" {
		t.Errorf("RoleCode should use OLLAMA_MODEL_CODE: got %q", got)
	}
}

func TestModelFor_PartialSplit_OtherRoleUsesFallback(t *testing.T) {
	clearModelEnv(t)
	t.Setenv("OLLAMA_MODEL", "fallback-model")
	t.Setenv("OLLAMA_MODEL_PLAN", "qwen3:14b")
	// CODE not set → falls back to OLLAMA_MODEL, not to the PLAN model.
	if got := ModelFor(RoleCode); got != "fallback-model" {
		t.Errorf("RoleCode with only PLAN set should fall back to OLLAMA_MODEL: got %q", got)
	}
	if got := ModelFor(RolePlan); got != "qwen3:14b" {
		t.Errorf("RolePlan should use its override: got %q", got)
	}
}
