package intelligence

import (
	"path/filepath"
	"strings"
)

// ─────────────────────────────────────────────
// SystemGraph captures structural relationships between code layers.
//
// Unlike Context.Files (a flat path→content map), this builds:
//
//	Entity → all files that define or reference it
//	Layer  → file paths grouped by architectural role
//	Call chain: Handler → Service → Repo → Model (inferred from imports)
//
// This enables the healer to find ALL related files for an entity
// across layers, not just the single file that errored.
// ─────────────────────────────────────────────
type SystemGraph struct {
	// EntityToFiles: entity name → all file paths that reference it.
	// Populated by scanning struct definitions and usage patterns.
	EntityToFiles map[string][]string

	// LayerFiles: architectural layer → file paths in that layer.
	// Layers: "model", "repo", "service", "handler", "validation", "frontend", "mobile"
	LayerFiles map[string][]string

	// CallChain: for a given entity, the ordered layer files.
	// Useful for building a repair prompt that shows the full stack.
	CallChain map[string][]string
}

// BuildSystemGraph constructs the dependency graph from the loaded file map.
// This is fast (single pass over files) and allocation-efficient.
func BuildSystemGraph(files map[string]string) SystemGraph {
	g := SystemGraph{
		EntityToFiles: map[string][]string{},
		LayerFiles:    map[string][]string{},
		CallChain:     map[string][]string{},
	}

	for path := range files {
		lower := strings.ToLower(path)
		switch {
		case strings.Contains(lower, "/model/"):
			g.LayerFiles["model"] = appendUnique(g.LayerFiles["model"], path)
		case strings.Contains(lower, "/repository/"):
			g.LayerFiles["repo"] = appendUnique(g.LayerFiles["repo"], path)
		case strings.Contains(lower, "/service/"):
			g.LayerFiles["service"] = appendUnique(g.LayerFiles["service"], path)
		case strings.Contains(lower, "/handler/"):
			g.LayerFiles["handler"] = appendUnique(g.LayerFiles["handler"], path)
		case strings.Contains(lower, "/validation/"):
			g.LayerFiles["validation"] = appendUnique(g.LayerFiles["validation"], path)
		case strings.Contains(lower, "frontend/"):
			g.LayerFiles["frontend"] = appendUnique(g.LayerFiles["frontend"], path)
		case strings.Contains(lower, "mobile/"):
			g.LayerFiles["mobile"] = appendUnique(g.LayerFiles["mobile"], path)
		}
	}

	// Build entity→files map from struct definitions and entity name usage.
	for path, content := range files {
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			// Detect struct definitions: "type EntityName struct {"
			if strings.HasPrefix(trimmed, "type ") &&
				strings.Contains(trimmed, " struct") {
				parts := strings.Fields(trimmed)
				if len(parts) >= 3 {
					entityName := parts[1]
					if isExportedName(entityName) && !isGenericGoType(entityName) {
						g.EntityToFiles[entityName] =
							appendUnique(g.EntityToFiles[entityName], path)
					}
				}
			}
		}
	}

	// Build call chain per entity: model → repo → service → handler
	for entityName := range g.EntityToFiles {
		lower := strings.ToLower(entityName)
		var chain []string
		for _, layer := range []string{"model", "repo", "service", "handler", "validation"} {
			for _, f := range g.LayerFiles[layer] {
				if strings.Contains(strings.ToLower(filepath.Base(f)), lower) {
					chain = appendUnique(chain, f)
				}
			}
		}
		if len(chain) > 0 {
			g.CallChain[entityName] = chain
		}
	}

	return g
}

// GetRelatedFiles returns all files that are related to the given entity,
// across all architectural layers. Used by the healer to provide full context.
// Files are ordered: model → repo → service → handler (deepest context first).
func (g *SystemGraph) GetRelatedFiles(entityName string, files map[string]string) []string {
	seen := map[string]bool{}
	var related []string

	entityLower := strings.ToLower(entityName)

	// Layer-ordered traversal for structured context
	for _, layer := range []string{"model", "repo", "service", "handler", "validation"} {
		for _, f := range g.LayerFiles[layer] {
			baseLower := strings.ToLower(filepath.Base(f))
			if strings.Contains(baseLower, entityLower) && !seen[f] {
				seen[f] = true
				related = append(related, f)
			}
		}
	}

	// Also include any files from EntityToFiles that reference this entity
	if entityFiles, ok := g.EntityToFiles[entityName]; ok {
		for _, f := range entityFiles {
			if !seen[f] {
				seen[f] = true
				related = append(related, f)
			}
		}
	}

	return related
}

// CallChainFor returns the ordered layer files for an entity.
// Returns nil if entity has no known call chain.
func (g *SystemGraph) CallChainFor(entityName string) []string {
	return g.CallChain[entityName]
}

// Summary produces a compact text description of the graph
// suitable for inclusion in an AI repair prompt.
func (g *SystemGraph) Summary() string {
	var b strings.Builder
	b.WriteString("# System Graph Summary\n")
	b.WriteString("## Entities detected:\n")
	for entity, files := range g.EntityToFiles {
		b.WriteString("  " + entity + " → " + strings.Join(files, ", ") + "\n")
	}
	b.WriteString("## Layer file counts:\n")
	for layer, files := range g.LayerFiles {
		b.WriteString("  " + layer + ": " + strings.Join(uniqueBaseNames(files), ", ") + "\n")
	}
	return b.String()
}

// ─────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────

func isExportedName(s string) bool {
	if s == "" {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

// isGenericGoType filters out standard Go type names that appear in struct defs
// but are not entity names we care about.
func isGenericGoType(s string) bool {
	generic := map[string]bool{
		"Builder": true, "Config": true, "Options": true, "Client": true,
		"Server": true, "Request": true, "Response": true, "Error": true,
		"Context": true, "Handler": true, "Router": true, "Logger": true,
	}
	return generic[s]
}

func appendUnique(slice []string, item string) []string {
	for _, v := range slice {
		if v == item {
			return slice
		}
	}
	return append(slice, item)
}

func uniqueBaseNames(paths []string) []string {
	var names []string
	for _, p := range paths {
		names = append(names, filepath.Base(p))
	}
	return names
}
