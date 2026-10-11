package services

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
)

const proposedSchemaPath = ".gen/proposed_schema.json"

type FieldDiff struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Status  string `json:"status"` // added | removed | modified | unchanged
	OldType string `json:"old_type,omitempty"`
}

type EntityDiff struct {
	Name   string      `json:"name"`
	Fields []FieldDiff `json:"fields"`
	IsNew  bool        `json:"is_new"`
}

type SchemaService struct{ root string }

func NewSchemaService(root string) *SchemaService { return &SchemaService{root: root} }

// CurrentEntities returns entities from the live IR snapshot.
func (s *SchemaService) CurrentEntities() []map[string]interface{} {
	ir, err := compiler.LoadPreviousIR()
	if err != nil || len(ir.Entities) == 0 {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(ir.Entities))
	for _, e := range ir.Entities {
		fields := make([]map[string]string, 0, len(e.Fields))
		for _, f := range e.Fields {
			fields = append(fields, map[string]string{"name": f.Name, "type": f.Type})
		}
		out = append(out, map[string]interface{}{"name": e.Name, "fields": fields})
	}
	return out
}

// EntityDiff computes a real field-level diff between:
//   - current entity in the live IR  (what's on disk now)
//   - proposed entity in .gen/proposed_schema.json (what BuildPlanV2 just planned)
//
// Status values per field:
//
//	"added":      in proposed but not in current
//	"removed":    in current but not in proposed
//	"modified":   in both, type changed
//	"unchanged":  identical in both
func (s *SchemaService) EntityDiff(entityName string) *EntityDiff {
	// Load current IR fields
	currentFields := map[string]string{}
	ir, _ := compiler.LoadPreviousIR()
	for _, e := range ir.Entities {
		if e.Name == entityName {
			for _, f := range e.Fields {
				currentFields[f.Name] = f.Type
			}
			break
		}
	}

	// Load proposed fields from .gen/proposed_schema.json
	proposedFields := map[string]string{}
	isNew := len(currentFields) == 0
	if data, err := os.ReadFile(proposedSchemaPath); err == nil {
		var proposed []struct {
			Name   string `json:"name"`
			Fields []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"fields"`
		}
		if json.Unmarshal(data, &proposed) == nil {
			for _, pe := range proposed {
				if pe.Name == entityName {
					for _, f := range pe.Fields {
						proposedFields[f.Name] = f.Type
					}
					break
				}
			}
		}
	}

	// If no proposed schema exists, fall back to current as proposed
	// (diff will be all "unchanged", which is better than a broken response)
	if len(proposedFields) == 0 {
		for name, typ := range currentFields {
			proposedFields[name] = typ
		}
	}

	// Compute diff
	var diffs []FieldDiff

	// Fields in proposed
	for name, proposedType := range proposedFields {
		if currentType, exists := currentFields[name]; !exists {
			diffs = append(diffs, FieldDiff{Name: name, Type: proposedType, Status: "added"})
		} else if currentType != proposedType {
			diffs = append(diffs, FieldDiff{Name: name, Type: proposedType, Status: "modified", OldType: currentType})
		} else {
			diffs = append(diffs, FieldDiff{Name: name, Type: proposedType, Status: "unchanged"})
		}
	}

	// Fields in current but not in proposed → removed
	for name, currentType := range currentFields {
		if _, exists := proposedFields[name]; !exists {
			diffs = append(diffs, FieldDiff{Name: name, Type: currentType, Status: "removed"})
		}
	}

	return &EntityDiff{Name: entityName, Fields: diffs, IsNew: isNew}
}

// FilePreview describes a single file that would be written by the generator.
type FilePreview struct {
	Path   string `json:"path"`   // relative to project root
	Exists bool   `json:"exists"` // true if the file is already on disk
	Status string `json:"status"` // "new" | "overwrite" | "evolve"
}

// PreviewEntity returns the list of files that would be generated for
// the named entity. It mirrors the path conventions in engine/exports.go
// so the Studio UI can show exactly what will change before a build runs.
//
// Paths use lowercase entity name for file names (e.g. "User" → "user").
// Returns an error if the entity is not found in the proposed schema.
func (s *SchemaService) PreviewEntity(entityName string) ([]FilePreview, error) {
	// ── 1. Verify entity exists in proposed or current schema ──────
	found := false

	// Check proposed schema first (most up-to-date after a plan step)
	if data, err := os.ReadFile(proposedSchemaPath); err == nil {
		var proposed []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(data, &proposed) == nil {
			for _, e := range proposed {
				if e.Name == entityName {
					found = true
					break
				}
			}
		}
	}

	// Fall back to current IR
	if !found {
		ir, err := compiler.LoadPreviousIR()
		if err == nil {
			for _, e := range ir.Entities {
				if e.Name == entityName {
					found = true
					break
				}
			}
		}
	}

	if !found {
		return nil, fmt.Errorf("entity %q not found in proposed schema or current IR", entityName)
	}

	// ── 2. Derive file paths from gen-system path conventions ──────
	// These mirror the templates used in engine/exports.go and the
	// generator CLI so the preview is always accurate.
	lower := strings.ToLower(entityName)
	root := s.root
	if root == "" {
		root = "."
	}

	candidates := []string{
		// Go backend
		"backend/internal/model/" + lower + ".go",
		"backend/internal/repository/" + lower + "_repo.go",
		"backend/internal/service/" + lower + "_service.go",
		"backend/internal/handler/" + lower + "_handler.go",
		"backend/internal/validation/" + lower + "_validation.go",
		// React frontend
		"frontend/src/components/" + entityName + "Form.jsx",
		"frontend/src/components/" + entityName + "List.jsx",
		// TypeScript types (Phase 0.3)
		"frontend/src/types/" + lower + ".ts",
		// Expo mobile
		"mobile/screens/" + entityName + "Screen.tsx",
		// SQL migration (approximate; the actual filename includes a timestamp)
		"backend/migrations/YYYYMMDD_create_" + lower + "s.sql",
	}

	// ── 3. Annotate each path with disk existence + status ─────────
	previews := make([]FilePreview, 0, len(candidates))
	for _, rel := range candidates {
		full := root + "/" + rel
		exists := fileExists(full)
		status := "new"
		if exists {
			status = "overwrite"
		}
		previews = append(previews, FilePreview{
			Path:   rel,
			Exists: exists,
			Status: status,
		})
	}

	return previews, nil
}

// fileExists returns true if the path exists on disk (file or dir).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
