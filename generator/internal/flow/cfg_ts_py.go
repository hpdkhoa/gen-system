// flow/cfg_ts_py.go: multi-block CFG builder for TS, Python, and CA Gen
// v25.1: extractStructuralNodes/hasStructuralNodes now recognise NodeTry and
// preserve Else (catch/finally, branch else-arms) with children extracted, so
// try/catch survives extraction and reaches the processNode try-split. Prior to
// this a non-Go try flattened its body into the parent block and lost the
// wrapper, rendering as a lone "try {" node → End (the golden-IR CFG symptom).
// Added for open item 4: TS/Python full CFG.
// The TS and Python frontends already emit NodeCall, NodeBranch, NodeLoop,
// and NodeReturn nodes as children of functions, but they sit
// wrapped inside a NodeUnknown{reason:"body_not_parsed"} child.
// This file extracts those structural nodes and feeds them through the
// same cfgBuilder used for Go, producing real multi-block CFGs instead
// of the previous single-block stub.
//
// CA Gen bodies were already wired through buildProcedureCFG but lacked
// the ESCAPE/EXIT handling. This file fixes that too.
//
// Nothing in this file redeclares any symbol from flow.go.
package flow

import (
	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

// buildStructuralCFG builds a CFG for non-Go languages by extracting
// structural nodes (calls, branches, loops, returns) from a function's
// children. For TS/Python the structural nodes live inside a single
// NodeUnknown{reason:"body_not_parsed"} wrapper; this unwraps them.
func buildStructuralCFG(id string, n *ast.Node) *CFG {
	cfg := &CFG{FuncID: id}
	b := &cfgBuilder{cfg: cfg}
	b.entry = b.newBlock("entry")
	b.current = b.entry

	// Collect structural child nodes, unwrapping the opaque-body wrapper if present.
	stmts := extractStructuralNodes(n.Children)

	for _, stmt := range stmts {
		b.processNode(stmt)
	}

	if len(cfg.Exits) == 0 && b.current != nil {
		b.current.Label += "+exit"
		cfg.Exits = []int{b.current.ID}
	}
	return cfg
}

// extractStructuralNodes recursively flattens children, unwrapping any
// NodeUnknown{reason:"body_not_parsed"} wrappers produced by the TS and
// Python frontends. It collects only structurally meaningful nodes
// (calls, branches, loops, returns, defers, escapes).
func extractStructuralNodes(children []*ast.Node) []*ast.Node {
	var out []*ast.Node
	for _, c := range children {
		if c == nil {
			continue
		}
		if c.Kind == ast.NodeUnknown && c.UnknownReason == "body_not_parsed" {
			// The raw source is opaque, but the frontend may have also populated
			// child structural nodes; recurse into them.
			out = append(out, extractStructuralNodes(c.Children)...)
			continue
		}
		switch c.Kind {
		case ast.NodeCall, ast.NodeReturn, ast.NodeBranch,
			ast.NodeLoop, ast.NodeDefer, ast.NodeEscape, ast.NodeAssign,
			ast.NodeTry:
			// v23.41: NodeAssign kept so COBOL data statements (SUBTRACT/MOVE/…)
			// appear as CFG process steps instead of vanishing.
			// v25.1: NodeTry kept so try/catch survives extraction and reaches the
			// processNode NodeTry case (flow.go) that builds the try-body + catch +
			// merge split. Before this, NodeTry hit the default arm below, which
			// flattened its children into the parent block and dropped the wrapper.
			// So a Java/TS/Python try rendered as a lone "try {" node straight to
			// End, with the body lost (the reported CFG symptom).
			// Copy with children recursively extracted so nested control flow works.
			// Else (branch else-arm; try catch/finally) is extracted too. A shallow
			// struct copy carries Else by value but leaves its nodes un-extracted,
			// which would strip nested control flow inside an else/catch body.
			node := shallowCopyWithChildren(c, extractStructuralNodes(c.Children))
			node.Else = extractStructuralNodes(c.Else)
			out = append(out, node)
		default:
			// Other kinds (assign, unknown): recurse for nested structural nodes.
			nested := extractStructuralNodes(c.Children)
			out = append(out, nested...)
		}
	}
	return out
}

// shallowCopyWithChildren returns a shallow copy of n with Children replaced.
func shallowCopyWithChildren(n *ast.Node, children []*ast.Node) *ast.Node {
	cp := *n
	cp.Children = children
	return &cp
}

// hasStructuralNodes returns true if children contain any control-flow nodes,
// meaning a structural CFG will have more than one block.
func hasStructuralNodes(children []*ast.Node) bool {
	for _, c := range children {
		if c == nil {
			continue
		}
		if c.Kind == ast.NodeUnknown && c.UnknownReason == "body_not_parsed" {
			if hasStructuralNodes(c.Children) {
				return true
			}
			continue
		}
		switch c.Kind {
		case ast.NodeBranch, ast.NodeLoop, ast.NodeReturn, ast.NodeEscape,
			ast.NodeTry:
			// v25.1: NodeTry counts as structural so a method whose only control
			// flow is a try/catch still routes to buildStructuralCFG (and thus the
			// try-split) instead of collapsing to a single opaque block.
			return true
		}
		if hasStructuralNodes(c.Children) || hasStructuralNodes(c.Else) {
			return true
		}
	}
	return false
}
