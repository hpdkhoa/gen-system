package planner

import "github.com/hpdkhoa/gen-system/generator/internal/schema"

// ─────────────────────────────────────────────
// Core domain types
// ─────────────────────────────────────────────

// Goal is the raw natural-language intent from the user.
type Goal struct {
	Description string
}

// Feature represents a named product capability extracted from the plan.
type Feature struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Entities    []string `json:"entities,omitempty"`
	Actions     []string `json:"actions,omitempty"`
}

// Plan is the structured intermediate representation produced by the AI
// before any code is generated. It can be reviewed and edited by the user
// before execution is triggered.
type Plan struct {
	Goal     string          `json:"goal"`
	Features []Feature       `json:"features"`
	Entities []schema.Entity `json:"entities"`
	Tasks    []Task          `json:"tasks"`
	Notes    []string        `json:"notes,omitempty"`
}

// TaskStatus represents lifecycle state of a single task.
type TaskStatus string

const (
	StatusPending TaskStatus = "pending"
	StatusRunning TaskStatus = "running"
	StatusDone    TaskStatus = "done"
	StatusFailed  TaskStatus = "failed"
	StatusSkipped TaskStatus = "skipped"
)

// Task is a single unit of work in the execution graph.
// Tasks declare explicit dependencies so the executor can
// order them correctly and skip already-completed work.
type Task struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"` // generate_model | generate_repo | generate_service | generate_handler | generate_frontend | generate_mobile | migrate | test | fix
	EntityName   string                 `json:"entity,omitempty"`
	Dependencies []string               `json:"dependencies,omitempty"`
	Payload      map[string]interface{} `json:"payload,omitempty"`
	Status       TaskStatus             `json:"status"`
	Error        string                 `json:"error,omitempty"`
}
