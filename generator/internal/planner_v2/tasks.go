package planner_v2

// ─────────────────────────────────────────────
// Architecture helpers used by both tasks and planner.
// The actual task building is in buildAgentTasks() in planner.go
// because it needs to import planner.Task and schema.Entity.
// ─────────────────────────────────────────────

// HasComponent returns true if the given component is in the architecture.
// Case-insensitive. Used externally if callers need to inspect the arch.
func (a Architecture) HasComponent(name string) bool {
	for _, c := range a.Components {
		if len(c) == len(name) {
			match := true
			for i := range c {
				lc := c[i]
				ln := name[i]
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

// IsMonolith returns true if the architecture type is monolith.
func (a Architecture) IsMonolith() bool {
	return a.Type == "monolith"
}
