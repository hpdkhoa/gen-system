// objgraph/objgraph.go — ObjectGraph format for skill files
// Added for open item 5: ObjectGraph replaces .md context docs.
//
// An ObjectGraph is a structured, queryable representation of a project's
// symbols and their relationships, designed to replace ad-hoc .md context
// documents fed to the AI repair/generation pipeline.
//
// Key differences vs .md docs:
//   - Machine-readable (JSON serialisable), not prose.
//   - Topology-aware: nodes, edges, layers, and effect metadata are explicit.
//   - Built from the live symbol index and flow data — always fresh.
//   - Compact: the NIT serialiser is used for node content, not raw source.
//
// Typical use: build an ObjectGraph for a project, serialise it to JSON,
// include a compact rendering in the AI repair prompt in place of the
// raw architecture markdown.
//
// Package layout (all in one file, no sub-packages):
//
//	ObjectGraph      — the graph type
//	ObjectNode       — one symbol in the graph
//	ObjectEdge       — a directed relationship between two nodes
//	Build()          — constructs the graph from symbols.Index + flow data
//	Render()         — compact text form for LLM consumption
//	JSON marshalling — for persistence / transport
package objgraph

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// ─── Types ────────────────────────────────────────────────────

// EdgeKind classifies what a directed edge means.
type EdgeKind string

const (
	EdgeCalls      EdgeKind = "calls"      // function A calls function B
	EdgeImports    EdgeKind = "imports"    // file A imports package B
	EdgeReferences EdgeKind = "refs"       // symbol A references type B
	EdgeInherits   EdgeKind = "inherits"   // class A extends B
	EdgeImplements EdgeKind = "implements" // type A implements interface B
)

// ObjectNode is one symbol in the graph.
type ObjectNode struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Language   string   `json:"lang"`
	Layer      string   `json:"layer,omitempty"`
	File       string   `json:"file"`
	Line       int      `json:"line,omitempty"`
	Exported   bool     `json:"exported"`
	Provenance string   `json:"provenance,omitempty"` // v23.32: "", "stdlib", "thirdparty"
	Effects    []string `json:"effects,omitempty"`    // inferred effect kinds
	NIT        string   `json:"nit,omitempty"`        // compact NIT representation
}

// ObjectEdge is a directed relationship between two nodes.
type ObjectEdge struct {
	From string   `json:"from"` // ObjectNode.ID
	To   string   `json:"to"`   // ObjectNode.ID
	Kind EdgeKind `json:"kind"`
}

// ObjectGraph is the full project graph.
type ObjectGraph struct {
	ProjectRoot string       `json:"project_root"`
	Nodes       []ObjectNode `json:"nodes"`
	Edges       []ObjectEdge `json:"edges"`
	// LayerIndex maps layer name → []node IDs for fast lookup.
	LayerIndex map[string][]string `json:"layer_index,omitempty"`
}

// ─── Build ────────────────────────────────────────────────────

// BuildOptions controls how the graph is constructed.
type BuildOptions struct {
	// ExportedOnly omits unexported symbols from the graph nodes.
	ExportedOnly bool
	// MaxEdgesPerNode caps outgoing edges per node (0 = unlimited).
	MaxEdgesPerNode int
	// IncludeNIT attaches the NIT compact representation to each node.
	IncludeNIT bool
}

// DefaultBuildOptions are calibrated for AI prompt use.
var DefaultBuildOptions = BuildOptions{
	ExportedOnly:    false,
	MaxEdgesPerNode: 12,
	IncludeNIT:      true,
}

// Build constructs an ObjectGraph from the symbol index and optional flow data.
// effects may be nil if flow analysis has not been run.
func Build(
	root string,
	idx *symbols.Index,
	effects map[string]*flow.FuncEffects,
	opts BuildOptions,
) *ObjectGraph {
	g := &ObjectGraph{
		ProjectRoot: root,
		LayerIndex:  make(map[string][]string),
	}

	if idx == nil {
		return g
	}

	// ── Nodes ─────────────────────────────────────────────
	allSyms := idx.AllSymbols()
	// Sort for deterministic output.
	sort.Slice(allSyms, func(i, j int) bool {
		return allSyms[i].ID < allSyms[j].ID
	})

	nodeSet := map[string]bool{}
	for _, s := range allSyms {
		if opts.ExportedOnly && !s.Exported {
			continue
		}
		if s.Kind == symbols.SymModule {
			continue // skip file-level module nodes (too noisy)
		}

		efxList := effectsForSymbol(s.ID, effects)

		node := ObjectNode{
			ID:         s.ID,
			Name:       s.Name,
			Kind:       string(s.Kind),
			Language:   string(s.Language),
			Layer:      s.Layer,
			File:       s.DefinedIn,
			Line:       s.Span.Start.Line,
			Exported:   s.Exported,
			Provenance: string(s.Provenance),
			Effects:    efxList,
		}

		if opts.IncludeNIT {
			node.NIT = nitForSymbol(s)
		}

		g.Nodes = append(g.Nodes, node)
		nodeSet[s.ID] = true

		if s.Layer != "" {
			g.LayerIndex[s.Layer] = append(g.LayerIndex[s.Layer], s.ID)
		}
	}

	// ── Edges ─────────────────────────────────────────────
	edgeCount := map[string]int{}
	for _, edge := range idx.AllCallEdges() {
		if edge.CalleeID == "" {
			continue
		}
		if !nodeSet[edge.CallerID] || !nodeSet[edge.CalleeID] {
			continue
		}
		if opts.MaxEdgesPerNode > 0 && edgeCount[edge.CallerID] >= opts.MaxEdgesPerNode {
			continue
		}
		g.Edges = append(g.Edges, ObjectEdge{
			From: edge.CallerID,
			To:   edge.CalleeID,
			Kind: EdgeCalls,
		})
		edgeCount[edge.CallerID]++
	}

	return g
}

// ─── Render ───────────────────────────────────────────────────

// Render produces a compact text representation of the graph for
// inclusion in LLM prompts. Format:
//
//	GRAPH  <project_root>  nodes:<N>  edges:<E>
//
//	LAYER handler
//	  pkg/handler.go#func:CreateUser → handler.CreateUser(req:CreateUserRequest) error
//	  ...
//
//	CALLS
//	  CreateUser → validate,userRepo.Save
//	  ...
func Render(g *ObjectGraph) string {
	if g == nil {
		return ""
	}
	var b strings.Builder

	b.WriteString(fmt.Sprintf("GRAPH  %s  nodes:%d  edges:%d\n",
		g.ProjectRoot, len(g.Nodes), len(g.Edges)))

	// Group by layer.
	layers := sortedKeys(g.LayerIndex)
	nodeByID := make(map[string]*ObjectNode, len(g.Nodes))
	for i := range g.Nodes {
		nodeByID[g.Nodes[i].ID] = &g.Nodes[i]
	}

	// Layered section.
	for _, layer := range layers {
		ids := g.LayerIndex[layer]
		if len(ids) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("\nLAYER %s\n", layer))
		for _, id := range ids {
			n := nodeByID[id]
			if n == nil {
				continue
			}
			line := fmt.Sprintf("  %-60s", id)
			if n.NIT != "" {
				line += "  " + n.NIT
			} else {
				line += "  " + n.Name
			}
			if len(n.Effects) > 0 {
				line += "  [" + strings.Join(n.Effects, ",") + "]"
			}
			b.WriteString(line + "\n")
		}
	}

	// Unlayered nodes.
	var unlayered []string
	for _, n := range g.Nodes {
		if n.Layer == "" {
			unlayered = append(unlayered, n.ID)
		}
	}
	if len(unlayered) > 0 {
		b.WriteString("\nOTHER\n")
		for _, id := range unlayered {
			n := nodeByID[id]
			if n == nil {
				continue
			}
			nit := n.NIT
			if nit == "" {
				nit = n.Name
			}
			b.WriteString(fmt.Sprintf("  %-60s  %s\n", id, nit))
		}
	}

	// Call edges summary.
	if len(g.Edges) > 0 {
		// Build caller → []callee names
		callerCallees := map[string][]string{}
		for _, e := range g.Edges {
			if e.Kind != EdgeCalls {
				continue
			}
			callee := nodeByID[e.To]
			calleeName := e.To
			if callee != nil {
				calleeName = callee.Name
			}
			callerCallees[e.From] = append(callerCallees[e.From], calleeName)
		}

		b.WriteString("\nCALLS\n")
		callers := sortedKeys(callerCallees)
		for _, callerID := range callers {
			caller := nodeByID[callerID]
			callerName := callerID
			if caller != nil {
				callerName = caller.Name
			}
			b.WriteString(fmt.Sprintf("  %-30s → %s\n",
				callerName,
				strings.Join(callerCallees[callerID], ",")))
		}
	}

	return b.String()
}

// ─── Persistence ──────────────────────────────────────────────

// Save writes the graph to a JSON file at path.
func (g *ObjectGraph) Save(path string) error {
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return fmt.Errorf("objgraph.Save: %w", err)
	}
	if err := os.MkdirAll(dirOf(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Load reads a previously saved ObjectGraph from path.
// Returns an empty graph if the file does not exist.
func Load(path string) (*ObjectGraph, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &ObjectGraph{LayerIndex: make(map[string][]string)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("objgraph.Load: %w", err)
	}
	var g ObjectGraph
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("objgraph.Load: unmarshal: %w", err)
	}
	if g.LayerIndex == nil {
		g.LayerIndex = make(map[string][]string)
	}
	return &g, nil
}

// DefaultPath is the conventional location for the object graph file.
const DefaultPath = ".gen/objgraph.json"

// ─── Query helpers ────────────────────────────────────────────

// NodesInLayer returns all nodes assigned to the given architectural layer.
func (g *ObjectGraph) NodesInLayer(layer string) []ObjectNode {
	ids := g.LayerIndex[layer]
	nodeByID := g.nodeByIDMap()
	var out []ObjectNode
	for _, id := range ids {
		if n, ok := nodeByID[id]; ok {
			out = append(out, n)
		}
	}
	return out
}

// CalleesOf returns the IDs of all nodes directly called by nodeID.
func (g *ObjectGraph) CalleesOf(nodeID string) []string {
	var out []string
	for _, e := range g.Edges {
		if e.From == nodeID && e.Kind == EdgeCalls {
			out = append(out, e.To)
		}
	}
	return out
}

// CallersOf returns the IDs of all nodes that call nodeID.
func (g *ObjectGraph) CallersOf(nodeID string) []string {
	var out []string
	for _, e := range g.Edges {
		if e.To == nodeID && e.Kind == EdgeCalls {
			out = append(out, e.From)
		}
	}
	return out
}

// NodeByID returns the node with the given ID, or nil.
func (g *ObjectGraph) NodeByID(id string) *ObjectNode {
	m := g.nodeByIDMap()
	if n, ok := m[id]; ok {
		return &n
	}
	return nil
}

func (g *ObjectGraph) nodeByIDMap() map[string]ObjectNode {
	m := make(map[string]ObjectNode, len(g.Nodes))
	for _, n := range g.Nodes {
		m[n.ID] = n
	}
	return m
}

// ─── Helpers ──────────────────────────────────────────────────

// effectsForSymbol extracts effect kind strings for a symbol ID.
func effectsForSymbol(id string, effects map[string]*flow.FuncEffects) []string {
	if effects == nil {
		return nil
	}
	e, ok := effects[id]
	if !ok || e == nil {
		return nil
	}
	var out []string
	for kind, set := range e.Effects {
		if set {
			out = append(out, string(kind))
		}
	}
	sort.Strings(out)
	return out
}

// nitForSymbol produces a one-line NIT summary of a symbol for the graph.
func nitForSymbol(s *symbols.Symbol) string {
	var b strings.Builder
	switch s.Kind {
	case symbols.SymFunc:
		b.WriteString("FN ")
		b.WriteString(s.Name)
	case symbols.SymMethod:
		b.WriteString("MTH ")
		b.WriteString(s.QualName)
	case symbols.SymType:
		// v18.3: symbols package collapses struct/class/interface/enum
		// into the single SymType kind. The previous SymStruct/SymInterface
		// cases were undefined. Emit "TYP " for the unified type kind;
		// callers needing struct-vs-interface granularity should look at
		// ast.Module.TopLevelByKind() against the original AST, not at the
		// symbol index.
		b.WriteString("TYP ")
		b.WriteString(s.Name)
	case symbols.SymProcedure:
		b.WriteString("PRO ")
		b.WriteString(s.Name)
	default:
		b.WriteString(string(s.Kind))
		b.WriteString(" ")
		b.WriteString(s.Name)
	}
	return b.String()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func dirOf(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[:idx]
	}
	return "."
}

// ─── Skill-file bridge ────────────────────────────────────────

// SkillContext builds the compact string that replaces a .md skill file
// for one architectural layer. It combines the layer's NIT signatures
// with the call-edge summary.
func SkillContext(g *ObjectGraph, layer string) string {
	if g == nil {
		return ""
	}
	nodes := g.NodesInLayer(layer)
	if len(nodes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s layer  (%d symbols)\n", layer, len(nodes)))
	for _, n := range nodes {
		line := "  " + n.NIT
		if len(n.Effects) > 0 {
			line += "  [" + strings.Join(n.Effects, ",") + "]"
		}
		b.WriteString(line + "\n")

		// Inline callees (up to 4).
		callees := g.CalleesOf(n.ID)
		if len(callees) > 0 {
			names := make([]string, 0, len(callees))
			for _, id := range callees {
				if cn := g.NodeByID(id); cn != nil {
					names = append(names, cn.Name)
				}
			}
			if len(names) > 4 {
				names = names[:4]
			}
			if len(names) > 0 {
				b.WriteString("    calls: " + strings.Join(names, ", ") + "\n")
			}
		}
	}
	return b.String()
}

// TokenEstimate returns a rough token count for a rendered graph string
// (4 chars per token heuristic).
func TokenEstimate(rendered string) int {
	return (len(rendered) + 3) / 4
}

// ─── Ensure package-level ast import is used ─────────────────
// (ast is imported for Language type; keep the import live.)
var _ = ast.LangGo
