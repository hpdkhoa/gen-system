package planner

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// ─────────────────────────────────────────────
// TaskGraphFromSchema builds an ordered task dependency graph.
// Static version, used when no existing project context is available.
// ─────────────────────────────────────────────
func TaskGraphFromSchema(s schema.SchemaFile) []Task {
	return buildTaskGraph(s, "", nil)
}

// ─────────────────────────────────────────────
// TaskGraphDynamic builds a context-aware task graph.
// Skips tasks for files that already exist and haven't changed.
// Only adds migration tasks for entities that actually differ.
//
// rootPath: existing project root (empty = treat as fresh)
// changedEntities: entity names that changed (nil = treat all as new)
// ─────────────────────────────────────────────
func TaskGraphDynamic(s schema.SchemaFile, rootPath string, changedEntities map[string]bool) []Task {
	return buildTaskGraph(s, rootPath, changedEntities)
}

// buildTaskGraph is the shared implementation.
func buildTaskGraph(s schema.SchemaFile, rootPath string, changedEntities map[string]bool) []Task {
	var tasks []Task

	isFresh := rootPath == "" || changedEntities == nil
	needsMigrate := false

	// ── PER-ENTITY TASKS ────────────────────────────────────────
	for _, e := range s.Entities {
		name := e.Name

		// In dynamic mode: skip entities that haven't changed
		if !isFresh && !changedEntities[name] {
			continue
		}

		// Determine if this entity needs migration
		// (new entity OR existing entity with changed fields)
		entityNeedsMigrate := isFresh || entityFilesMissing(rootPath, name)
		if entityNeedsMigrate {
			needsMigrate = true
		}

		modelID := taskID("generate_model", name)
		repoID := taskID("generate_repo", name)
		serviceID := taskID("generate_service", name)
		handlerID := taskID("generate_handler", name)
		validationID := taskID("generate_validation", name)
		frontendID := taskID("generate_frontend", name)
		mobileID := taskID("generate_mobile", name)

		// Determine model dep: fresh → depends on migrate; existing → no migrate dep
		modelDeps := []string{}
		if entityNeedsMigrate {
			modelDeps = []string{"migrate"}
		}

		tasks = append(tasks,
			Task{
				ID:           modelID,
				Type:         "generate_model",
				EntityName:   name,
				Dependencies: modelDeps,
				Status:       StatusPending,
				Payload:      map[string]interface{}{"entity": name, "fresh": isFresh},
			},
			Task{
				ID:           repoID,
				Type:         "generate_repo",
				EntityName:   name,
				Dependencies: []string{modelID},
				Status:       StatusPending,
				Payload:      map[string]interface{}{"entity": name},
			},
			Task{
				ID:           serviceID,
				Type:         "generate_service",
				EntityName:   name,
				Dependencies: []string{repoID},
				Status:       StatusPending,
				Payload:      map[string]interface{}{"entity": name},
			},
			Task{
				ID:           handlerID,
				Type:         "generate_handler",
				EntityName:   name,
				Dependencies: []string{serviceID},
				Status:       StatusPending,
				Payload:      map[string]interface{}{"entity": name},
			},
			Task{
				ID:           validationID,
				Type:         "generate_validation",
				EntityName:   name,
				Dependencies: []string{modelID},
				Status:       StatusPending,
				Payload:      map[string]interface{}{"entity": name},
			},
			Task{
				ID:           frontendID,
				Type:         "generate_frontend",
				EntityName:   name,
				Dependencies: []string{handlerID},
				Status:       StatusPending,
				Payload:      map[string]interface{}{"entity": name},
			},
			Task{
				ID:           mobileID,
				Type:         "generate_mobile",
				EntityName:   name,
				Dependencies: []string{handlerID},
				Status:       StatusPending,
				Payload:      map[string]interface{}{"entity": name},
			},
		)
	}

	// ── MIGRATE (only if needed) ─────────────────────────────────
	if needsMigrate || isFresh {
		migrateTask := Task{
			ID:     "migrate",
			Type:   "migrate",
			Status: StatusPending,
			Payload: map[string]interface{}{
				"dry": false,
			},
		}
		tasks = append([]Task{migrateTask}, tasks...)
	}

	if len(tasks) == 0 {
		return tasks
	}

	// ── GLOBAL TASKS ─────────────────────────────────────────────
	allHandlerIDs := collectHandlerIDsFromTasks(tasks)
	if len(allHandlerIDs) > 0 {
		tasks = append(tasks,
			Task{
				ID:           "generate_router",
				Type:         "generate_router",
				Dependencies: allHandlerIDs,
				Status:       StatusPending,
			},
			Task{
				ID:           "test",
				Type:         "test",
				Dependencies: []string{"generate_router"},
				Status:       StatusPending,
			},
		)
	}

	return tasks
}

// entityFilesMissing returns true if the model/handler files don't exist yet.
// Used to determine if a migration task is needed for this entity.
func entityFilesMissing(rootPath, entityName string) bool {
	if rootPath == "" {
		return true
	}
	modelFile := filepath.Join(rootPath, "backend/internal/model",
		entityNameToFileName(entityName)+".go")
	_, err := os.Stat(modelFile)
	return os.IsNotExist(err)
}

// entityNameToFileName converts PascalCase entity to lowercase file name.
func entityNameToFileName(name string) string {
	result := ""
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result += "_"
		}
		result += string(r | 32) // tolower
	}
	return result
}

// ─────────────────────────────────────────────
// TopologicalOrder returns tasks in dependency order.
// ─────────────────────────────────────────────
func TopologicalOrder(tasks []Task) []Task {
	byID := map[string]Task{}
	for _, t := range tasks {
		byID[t.ID] = t
	}

	visited := map[string]bool{}
	var result []Task

	var visit func(id string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		t, ok := byID[id]
		if !ok {
			return
		}
		for _, dep := range t.Dependencies {
			visit(dep)
		}
		result = append(result, t)
	}

	for _, t := range tasks {
		visit(t.ID)
	}

	return result
}

// ─────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────

func taskID(taskType, entityName string) string {
	return fmt.Sprintf("%s_%s", taskType, entityName)
}

func collectHandlerIDs(entities []schema.Entity) []string {
	var ids []string
	for _, e := range entities {
		ids = append(ids, taskID("generate_handler", e.Name))
	}
	return ids
}

func collectHandlerIDsFromTasks(tasks []Task) []string {
	var ids []string
	for _, t := range tasks {
		if t.Type == "generate_handler" {
			ids = append(ids, t.ID)
		}
	}
	return ids
}
