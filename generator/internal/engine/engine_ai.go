package engine

import (
	"github.com/hpdkhoa/gen-system/generator/internal/ai"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// RunAIMutation runs AI-based handler mutation for a single entity.
// This is called by `gen ai <path>` — NOT by engine.RunAll.
//
// The actual implementation lives in ai/engine_mutation.go.
// engine_ai.go only bridges the schema.Entity type to the ai package call.
func RunAIMutation(s schema.Entity, path string) {
	ai.RunAIMutation(s.Name, path)
}
