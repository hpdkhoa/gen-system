package planner_v2

import (
	"gen-system/generator/internal/ai"
)

// DesignArchitecture converts extracted features into an Architecture struct.
// All prompt building and LLM calls are delegated to ai.PlanArchitecture.
// This function only: calls AI, handles the fallback, converts to Architecture.
func DesignArchitecture(features []string) (Architecture, error) {
	archType, components, decisions, err := ai.PlanArchitecture(features)
	if err != nil {
		return defaultArchitecture(), err
	}
	return Architecture{
		Type:       archType,
		Components: components,
		Decisions:  decisions,
	}, nil
}

// defaultArchitecture is the safe fallback when AI calls fail.
func defaultArchitecture() Architecture {
	return Architecture{
		Type:       "monolith",
		Components: []string{"api", "database"},
		Decisions:  []string{"default monolith for simplicity"},
	}
}

// containsComponent checks if a component name is in the list (case-insensitive).
// Used by tasks.go to check arch.Components.
func containsComponent(components []string, name string) bool {
	for _, c := range components {
		if len(c) == len(name) {
			match := true
			for i := range c {
				lc, ln := c[i], name[i]
				if lc >= 'A' && lc <= 'Z' {
					lc += 32
				}
				if ln >= 'A' && ln <= 'Z' {
					ln += 32
				}
				if lc != ln {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

// containsStr is the alias used by planner.go helpers.
func containsStr(slice []string, val string) bool {
	return containsComponent(slice, val)
}
