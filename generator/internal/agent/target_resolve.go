// generator/internal/agent/target_resolve.go — v23.15: robust resolution of the
// generated file a heal/repair task targets. The previous resolveTargetFile
// lowercased the entity name for EVERY layer and used a "repository" dir, but
// the generator actually writes:
//
//	model      backend/internal/model/<lower>.go            (e.g. order.go)
//	store      backend/internal/store/<lower>.go            (e.g. order.go)
//	service    backend/internal/service/<lower>_service.go  (e.g. order_service.go)
//	handler    backend/internal/handler/<Entity>_handler.go (e.g. Order_handler.go)  ← CAPITALIZED
//	validation backend/internal/validation/<Entity>_validation.go (CAPITALIZED)
//
// so the healer kept building paths like .../handler/customer_handler.go that do
// not exist, and every AST rewrite failed with "no such file or directory" —
// the self-heal loop could never repair a handler or validation file.
//
// This resolver (a) builds the path with the CORRECT per-layer convention, and
// (b) if that exact path is missing, falls back to a case-insensitive scan of
// the layer directory matching the expected suffix — so a future casing/naming
// drift in the generator degrades to "still finds the file" instead of silently
// breaking healing again.
package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// layerSpec describes, per task type, the backend subdir, the filename suffix
// the generator appends, and whether the base name is the lower-cased route
// name or the exported (capitalized) entity name.
type layerSpec struct {
	dir         string // relative to rootPath
	suffix      string // e.g. "_handler.go", "_service.go", or ".go"
	capitalized bool   // true → <Entity><suffix>; false → <lower><suffix>
}

func layerFor(taskType string) (layerSpec, bool) {
	switch taskType {
	case "generate_model":
		return layerSpec{dir: "backend/internal/model", suffix: ".go", capitalized: false}, true
	case "generate_repo":
		// v23.2+ emits the store layer (the old "repository" dir is gone).
		return layerSpec{dir: "backend/internal/store", suffix: ".go", capitalized: false}, true
	case "generate_service":
		return layerSpec{dir: "backend/internal/service", suffix: "_service.go", capitalized: false}, true
	case "generate_handler":
		return layerSpec{dir: "backend/internal/handler", suffix: "_handler.go", capitalized: true}, true
	case "generate_validation":
		return layerSpec{dir: "backend/internal/validation", suffix: "_validation.go", capitalized: true}, true
	}
	return layerSpec{}, false
}

// sanitizeIdent mirrors the generator's exportName: split on any non-alphanumeric
// run, Title-case each part, join, and guard a leading digit. Kept agent-local so
// the agent package does not import engine (preserves the package boundary).
func sanitizeIdent(s string) string {
	var parts []string
	cur := strings.Builder{}
	flush := func() {
		if cur.Len() > 0 {
			parts = append(parts, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			cur.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	out := strings.Join(parts, "")
	if out == "" {
		return ""
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "F" + out
	}
	return out
}

// pruneOrphanEntityFiles deletes generated per-entity files left in the backend
// layer directories by a PREVIOUS run whose entity set differed from the current
// plan. The planner (especially V2) is non-deterministic, so regenerating into
// the same directory can leave e.g. service/user_service.go from an old run while
// the current run never generates a User store — the orphan then fails the build
// with "undefined: store.UserCRUD". Wiping is surgical: only files whose stem
// matches the <entity><suffix> pattern for an entity NOT in the current plan are
// removed; shared files (interfaces.go, handler.go, require.go, main.go, etc.)
// and current-entity files are left untouched. All generated files are rewritten
// by the run, so this never deletes anything that won't be reproduced.
//
// v23.15.
func pruneOrphanEntityFiles(rootPath string, validEntities []string) {
	// Valid stems (lower-cased) for entities in the current plan. The generator
	// names files by either the lower route name or the exported name; both
	// lower-case to the same stem, so a single set suffices.
	validLower := map[string]bool{}
	for _, name := range validEntities {
		exp := sanitizeIdent(name)
		if exp == "" {
			continue
		}
		validLower[strings.ToLower(exp)] = true
	}

	// Each layer: dir + the per-entity suffix. Files NOT ending in the suffix
	// (e.g. interfaces.go, handler.go, require.go) are shared and never pruned.
	layers := []struct {
		dir, suffix string
	}{
		{"backend/internal/model", ".go"},
		{"backend/internal/store", ".go"},
		{"backend/internal/service", "_service.go"},
		{"backend/internal/service", "_service_test.go"},
		{"backend/internal/handler", "_handler.go"},
		{"backend/internal/handler", "_handler_test.go"},
		{"backend/internal/handler", "_service_handler.go"},
		{"backend/internal/validation", "_validation.go"},
	}

	// Shared/per-layer files that happen to end in ".go" but are NOT per-entity;
	// must never be pruned by the ".go" layers (model/store).
	sharedBase := map[string]bool{
		"interfaces.go": true, "handler.go": true, "require.go": true,
		"main.go": true, "router.go": true, "routes.go": true,
	}

	for _, L := range layers {
		dir := filepath.Join(rootPath, L.dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		suffixLower := strings.ToLower(L.suffix)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			nameLower := strings.ToLower(name)
			if sharedBase[nameLower] {
				continue
			}
			if !strings.HasSuffix(nameLower, suffixLower) {
				continue
			}
			stem := strings.TrimSuffix(nameLower, suffixLower)
			if stem == "" {
				continue
			}
			// For the bare ".go" layers (model/store), the dir holds only
			// <entity>.go plus shared files; never treat a suffixed file from
			// another layer (defensive) as an entity stem.
			if L.suffix == ".go" && (strings.HasSuffix(stem, "_service") ||
				strings.HasSuffix(stem, "_handler") || strings.HasSuffix(stem, "_validation") ||
				strings.HasSuffix(stem, "_test")) {
				continue
			}
			if !validLower[stem] {
				os.Remove(filepath.Join(dir, name))
			}
		}
	}
}

// targetFileFor returns the absolute path of the generated file for (entity,
// taskType) under rootPath, with a case-insensitive directory fallback. Returns
// "" if the task type has no associated file.
func targetFileFor(entity, taskType, rootPath string) string {
	spec, ok := layerFor(taskType)
	if !ok {
		return ""
	}
	exported := sanitizeIdent(entity)
	if exported == "" {
		return ""
	}
	var base string
	if spec.capitalized {
		base = exported + spec.suffix
	} else {
		base = strings.ToLower(exported) + spec.suffix
	}
	exact := filepath.Join(rootPath, spec.dir, base)
	if _, err := os.Stat(exact); err == nil {
		return exact
	}

	// Fallback: scan the layer dir and match the suffix case-insensitively on
	// the entity stem. Handles any casing drift between generator and resolver.
	dir := filepath.Join(rootPath, spec.dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return exact // dir missing; return the computed path (caller reports the open error)
	}
	wantStem := strings.ToLower(exported)
	wantSuffix := strings.ToLower(spec.suffix)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		nameLower := strings.ToLower(e.Name())
		if !strings.HasSuffix(nameLower, wantSuffix) {
			continue
		}
		stem := strings.TrimSuffix(nameLower, wantSuffix)
		// For the ".go" layers the stem IS the entity; for suffixed layers the
		// stem is the entity too (suffix already stripped). Exclude *_test.go.
		if strings.HasSuffix(stem, "_test") {
			continue
		}
		if stem == wantStem {
			return filepath.Join(dir, e.Name())
		}
	}
	return exact
}
