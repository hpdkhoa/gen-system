// skillfile.go — v23.32 (stdlib provenance): SkillRecord/node now carries a
// Provenance field; writer emits it and IndexFromSkillFile round-trips it via
// AddModuleWithProvenance so a reloaded .skill preserves stdlib vs project
// origin. Prior:
// v19.8 (resolve-coldload-edges): IndexFromSkillFile now calls
// idx.ResolveAll() after the AddModule loop so skill-seeded contexts
// have resolved call edges. Same root cause as the LoadContext fix.
// objgraph/skillfile.go — TASK 4 (v19): SkillFile format for structured
// project context. Replaces opaque .md files with a versioned, machine-readable
// JSONL format that LoadContext can ingest directly.
//
// Format: one JSON object per line.
//
//	Line 1 (header):  {"skill_version":1,"project_root":"...","written_at":"...","constraints":[...]}
//	Lines 2..N:       {"kind":"node"|"edge"|"constraint", ...}
//
// Every line is valid JSON, so a person can read the file without special tools.
// A grep by symbol name, layer or edge kind finds the matching lines.
// The version field lets a reader detect a future format change.
package objgraph

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// ─── Format types ─────────────────────────────────────────────

const skillVersion = 1

// SkillHeader is the first line of every .skill file.
type SkillHeader struct {
	SkillVersion int      `json:"skill_version"`
	ProjectRoot  string   `json:"project_root"`
	WrittenAt    string   `json:"written_at"`
	Constraints  []string `json:"constraints,omitempty"`
	NodeCount    int      `json:"node_count"`
	EdgeCount    int      `json:"edge_count"`
}

// SkillRecord is one non-header line in a .skill file.
// Kind is "node", "edge", or "constraint".
type SkillRecord struct {
	Kind string `json:"kind"`

	// node fields
	ID         string   `json:"id,omitempty"`
	Name       string   `json:"name,omitempty"`
	SymKind    string   `json:"sym_kind,omitempty"`
	Language   string   `json:"lang,omitempty"`
	Layer      string   `json:"layer,omitempty"`
	File       string   `json:"file,omitempty"`
	Line       int      `json:"line,omitempty"`
	Exported   bool     `json:"exported,omitempty"`
	Provenance string   `json:"provenance,omitempty"` // v23.32: "", "stdlib", "thirdparty"
	Effects    []string `json:"effects,omitempty"`
	NIT        string   `json:"nit,omitempty"`
	Note       string   `json:"note,omitempty"` // human-authored annotation

	// edge fields
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	EdgeKind string `json:"edge_kind,omitempty"`

	// constraint fields (kind == "constraint")
	Category string `json:"category,omitempty"` // "hard", "arch", "quality"
	Text     string `json:"text,omitempty"`
}

// SkillFile is the decoded in-memory representation.
type SkillFile struct {
	Header      SkillHeader
	Nodes       []SkillRecord
	Edges       []SkillRecord
	Constraints []SkillRecord
}

// ─── Writer ───────────────────────────────────────────────────

// WriteSkillFile serialises an ObjectGraph plus a set of constraint strings
// into a .skill file at path.
//
// g must not be nil. constraints is a list of freeform constraint strings
// (e.g. from the project's hard-constraint section in the instructions).
// They are written as "constraint" records so LoadContext can surface them.
func WriteSkillFile(
	path string,
	g *ObjectGraph,
	constraints []string,
	notes map[string]string, // optional symbolID → human note
) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("skillfile.Write: %w", err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	// Header line.
	hdr := SkillHeader{
		SkillVersion: skillVersion,
		ProjectRoot:  g.ProjectRoot,
		WrittenAt:    time.Now().UTC().Format(time.RFC3339),
		NodeCount:    len(g.Nodes),
		EdgeCount:    len(g.Edges),
		Constraints:  constraints,
	}
	if err := enc.Encode(hdr); err != nil {
		return err
	}

	// Constraint records (one per constraint for grepping).
	for _, c := range constraints {
		rec := SkillRecord{
			Kind:     "constraint",
			Category: classifyConstraint(c),
			Text:     c,
		}
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}

	// Node records.
	for _, n := range g.Nodes {
		note := ""
		if notes != nil {
			note = notes[n.ID]
		}
		rec := SkillRecord{
			Kind:       "node",
			ID:         n.ID,
			Name:       n.Name,
			SymKind:    n.Kind,
			Language:   n.Language,
			Layer:      n.Layer,
			File:       n.File,
			Line:       n.Line,
			Exported:   n.Exported,
			Provenance: n.Provenance,
			Effects:    n.Effects,
			NIT:        n.NIT,
			Note:       note,
		}
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}

	// Edge records.
	for _, e := range g.Edges {
		rec := SkillRecord{
			Kind:     "edge",
			From:     e.From,
			To:       e.To,
			EdgeKind: string(e.Kind),
		}
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}

	return w.Flush()
}

// classifyConstraint heuristically assigns a category to a constraint string.
func classifyConstraint(s string) string {
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(lower, "offline") ||
		strings.Contains(lower, "no cdn") ||
		strings.Contains(lower, "no external") ||
		strings.Contains(lower, "no new external"):
		return "hard"
	case strings.Contains(lower, "arch") ||
		strings.Contains(lower, "layer") ||
		strings.Contains(lower, "pattern"):
		return "arch"
	default:
		return "quality"
	}
}

// ─── Reader ───────────────────────────────────────────────────

// ReadSkillFile parses a .skill file from path.
// Returns an error if the file does not exist or is malformed.
func ReadSkillFile(path string) (*SkillFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("skillfile.Read: %w", err)
	}
	defer f.Close()

	sf := &SkillFile{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024) // 4MB lines (large graphs)

	lineNum := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lineNum++

		if lineNum == 1 {
			// First non-empty line is always the header.
			if err := json.Unmarshal([]byte(line), &sf.Header); err != nil {
				return nil, fmt.Errorf("skillfile.Read: header line: %w", err)
			}
			if sf.Header.SkillVersion != skillVersion {
				return nil, fmt.Errorf("skillfile.Read: unsupported skill_version %d (want %d)",
					sf.Header.SkillVersion, skillVersion)
			}
			continue
		}

		var rec SkillRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			// Tolerate malformed lines — log and skip.
			continue
		}
		switch rec.Kind {
		case "node":
			sf.Nodes = append(sf.Nodes, rec)
		case "edge":
			sf.Edges = append(sf.Edges, rec)
		case "constraint":
			sf.Constraints = append(sf.Constraints, rec)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("skillfile.Read: scan: %w", err)
	}
	return sf, nil
}

// ─── SkillFile → ObjectGraph ──────────────────────────────────

// ToObjectGraph converts a SkillFile back into an ObjectGraph.
// The graph can then be used to seed a symbols.Index via IndexFromSkillFile.
func (sf *SkillFile) ToObjectGraph() *ObjectGraph {
	g := &ObjectGraph{
		ProjectRoot: sf.Header.ProjectRoot,
		LayerIndex:  make(map[string][]string),
	}
	for _, r := range sf.Nodes {
		n := ObjectNode{
			ID:       r.ID,
			Name:     r.Name,
			Kind:     r.SymKind,
			Language: r.Language,
			Layer:    r.Layer,
			File:     r.File,
			Line:     r.Line,
			Exported: r.Exported,
			Effects:  r.Effects,
			NIT:      r.NIT,
		}
		g.Nodes = append(g.Nodes, n)
		if r.Layer != "" {
			g.LayerIndex[r.Layer] = append(g.LayerIndex[r.Layer], r.ID)
		}
	}
	for _, r := range sf.Edges {
		g.Edges = append(g.Edges, ObjectEdge{
			From: r.From,
			To:   r.To,
			Kind: EdgeKind(r.EdgeKind),
		})
	}
	return g
}

// ─── SkillFile → symbols.Index ────────────────────────────────

// IndexFromSkillFile builds a minimal symbols.Index from the nodes in a
// SkillFile by constructing synthetic ast.Module values per file and
// calling idx.AddModule — the same path used during normal parsing.
// The resulting index has all the symbols but no raw AST or CFG data.
func IndexFromSkillFile(sf *SkillFile) *symbols.Index {
	idx := symbols.NewIndex()

	// Group records by file so each file becomes one synthetic Module.
	byFile := make(map[string][]*SkillRecord)
	for i := range sf.Nodes {
		r := &sf.Nodes[i]
		byFile[r.File] = append(byFile[r.File], r)
	}

	for filePath, recs := range byFile {
		lang := languageFromSkillRecords(recs)
		m := &ast.Module{
			Language: lang,
			FilePath: filePath,
		}
		prov := symbols.ProvProject
		for _, r := range recs {
			if r.Provenance != "" {
				prov = symbols.Provenance(r.Provenance)
			}
			n := &ast.Node{
				ID:       r.ID,
				Name:     r.Name,
				Kind:     skillNodeKind(r.SymKind),
				Language: lang,
				Span:     ast.Span{Start: ast.Pos{Line: r.Line, Column: 1}},
				Exported: r.Exported,
			}
			m.Nodes = append(m.Nodes, n)
		}
		idx.AddModuleWithProvenance(m, prov)
	}
	// v19.8: same fix as the LoadContext path — AddModule does not
	// resolve callees on its own, so synthetic-module construction
	// would otherwise leave every CalleeID empty.
	idx.ResolveAll()
	return idx
}

// languageFromSkillRecords picks the Language for a group of records sharing
// the same file. Uses the first non-unknown language found.
func languageFromSkillRecords(recs []*SkillRecord) ast.Language {
	for _, r := range recs {
		if r.File != "" {
			if lang := ast.DetectLanguage(r.File); lang != ast.LangUnknown {
				return lang
			}
		}
		switch r.Language {
		case "go":
			return ast.LangGo
		case "ts", "typescript":
			return ast.LangTS
		case "python":
			return ast.LangPython
		case "cagen":
			return ast.LangCAGen
		}
	}
	return ast.LangUnknown
}

// skillNodeKind converts a sym_kind string to the matching ast.NodeKind.
// Used when constructing synthetic ast.Module nodes from skill records.
func skillNodeKind(s string) ast.NodeKind {
	switch symbols.SymbolKind(s) {
	case symbols.SymFunc:
		return ast.NodeFunction
	case symbols.SymMethod:
		return ast.NodeMethod
	case symbols.SymType:
		return ast.NodeClass // NodeClass maps to SymType via BuildTable
	case symbols.SymConst:
		return ast.NodeConstant
	case symbols.SymImport:
		return ast.NodeImport
	case symbols.SymProcedure:
		return ast.NodeProcedure
	case symbols.SymVar:
		return ast.NodeVariable
	case symbols.SymModule:
		return ast.NodeModule
	}
	return ast.NodeUnknown
}

// EffectsFromSkillFile builds a symbolID→FuncEffects map from the skill
// file's effect lists. Sufficient for the presentation server's belief-
// building logic and for seeding Context.Effects in LoadContextWithSkill.
func EffectsFromSkillFile(sf *SkillFile) map[string]*flow.FuncEffects {
	out := make(map[string]*flow.FuncEffects)
	for _, r := range sf.Nodes {
		if len(r.Effects) == 0 {
			continue
		}
		fe := &flow.FuncEffects{
			SymbolID: r.ID,
			Effects:  make(map[flow.EffectKind]bool),
		}
		for _, e := range r.Effects {
			fe.Effects[flow.EffectKind(e)] = true
		}
		out[r.ID] = fe
	}
	return out
}
