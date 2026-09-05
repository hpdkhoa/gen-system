package ai

import (
	"fmt"
	"strings"
)

// ─────────────────────────────────────────────
// RepairCtx carries all inputs needed to build a repair prompt.
// agent/healer.go populates this struct and calls BuildRepairPrompt.
// The ai package owns the prompt format — healer owns the logic of
// WHEN to escalate and WHAT context to collect.
// ─────────────────────────────────────────────
type RepairCtx struct {
	// Core context (always present)
	TaskType     string
	EntityName   string
	FuncName     string
	ErrMsg       string
	ErrType      string // from intelligence.ClassifyError
	ErrHint      string // from ClassificationResult.Hint
	Strategy     string // from ClassificationResult.Strategy
	ExistingCode string

	// Attempt 2+: architecture summary
	ArchSummary string // empty = omit from prompt

	// Attempt 2+: RAG-retrieved framework documentation snippets (Phase 2.2)
	// Populated by rag.SearchSummary(errMsg, 3) when docs have been ingested.
	// Empty string = no docs ingested yet or no relevant match — omit silently.
	RAGContext string

	// Attempt 3: related file content
	// key = relative file path, value = file content
	RelatedFiles map[string]string // nil/empty = omit from prompt

	// v26.33: team style rules (style.PromptBlock) when a profile is active,
	// so a repaired body is not repaired into a style violation. Empty = omit.
	StyleRules string
}

// BuildRepairPrompt constructs the AI repair prompt from a RepairCtx.
//
// Context richness is controlled by the caller (agent/healer.go):
//
//	Attempt 1: set only core fields
//	Attempt 2: also set ArchSummary + RAGContext
//	Attempt 3: also set RelatedFiles
//
// This function only formats — it has no notion of "attempts".
func BuildRepairPrompt(ctx RepairCtx) string {
	var b strings.Builder

	b.WriteString("You are a senior Go backend engineer fixing a compilation error.\n\n")
	b.WriteString(fmt.Sprintf("TASK TYPE: %s\n", ctx.TaskType))
	b.WriteString(fmt.Sprintf("ENTITY: %s\n", ctx.EntityName))
	b.WriteString(fmt.Sprintf("FUNCTION TO FIX: %s\n", ctx.FuncName))
	b.WriteString(fmt.Sprintf("ERROR: %s\n", ctx.ErrMsg))
	b.WriteString(fmt.Sprintf("ERROR TYPE: %s\n", ctx.ErrType))
	b.WriteString(fmt.Sprintf("REPAIR HINT: %s\n\n", ctx.ErrHint))
	b.WriteString(fmt.Sprintf("EXISTING CODE:\n%s\n", ctx.ExistingCode))

	// Phase 2.2 — RAG framework docs injected at attempt 2+
	if ctx.RAGContext != "" {
		b.WriteString("\n# RELEVANT FRAMEWORK DOCUMENTATION:\n")
		b.WriteString(ctx.RAGContext)
		b.WriteString("\n")
	}

	if ctx.ArchSummary != "" {
		b.WriteString("\n# PROJECT ARCHITECTURE:\n")
		b.WriteString(ctx.ArchSummary)
	}

	if len(ctx.RelatedFiles) > 0 {
		b.WriteString("\n# RELATED FILES IN CALL CHAIN:\n")
		filesAdded := 0
		for path, content := range ctx.RelatedFiles {
			if content != "" && len(content) < 1500 && filesAdded < 4 {
				b.WriteString(fmt.Sprintf("\n--- %s ---\n%s\n", path, content))
				filesAdded++
			}
		}
	}

	if ctx.StyleRules != "" {
		b.WriteString("\n# TEAM STYLE RULES (the fixed body must follow these too):\n")
		b.WriteString(ctx.StyleRules)
	}

	b.WriteString("\nSTRICT RULES:\n")
	b.WriteString("- Output ONLY the function body (no func signature, no package declaration)\n")
	b.WriteString("- Do NOT include markdown code fences\n")
	b.WriteString("- Keep error handling intact\n")
	b.WriteString(fmt.Sprintf("- Fix specifically: %s\n", ctx.Strategy))
	b.WriteString("\nOUTPUT:\n")

	return b.String()
}
