// v23.39: BasicBlock.Cond carries the branch condition (set from NodeBranch.RawSource) so the CFG view can render a real decision node.
// flow.go — v23.8: buildGoCFG now prunes unreachable blocks (pruneUnreachable)
// so an if/else where every branch returns no longer leaves an orphan merge
// block (0 preds) in the graph; IDs are remapped densely so CFG.Block(id) stays
// valid. v23.7: NodeBranch with a non-empty Else now emits a distinct
// else-block (pred->else->merge) instead of folding else into the then-block.
// flow.go — Control-flow graph, dataflow analysis, effect tagging
// Patched for open item 4: TS/Python full CFG.
// Change: buildFuncCFG now calls buildStructuralCFG (defined in cfg_ts_py.go)
// for non-Go languages that have extractable structural nodes, instead of
// always falling back to a single opaque block.
package flow

import (
	"fmt"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// ─── CFG Types ────────────────────────────────────────────────

// BasicBlock is a maximal sequence of statements with no branches.
type BasicBlock struct {
	ID    int
	Stmts []*ast.Node // statement-level nodes in this block
	Succs []int       // successor block IDs (0..n)
	Preds []int       // predecessor block IDs
	// Label describes the block for human presentation.
	Label string
	// Cond is the branch condition text for a block that ends in an if/switch
	// (v23.39). Empty for straight-line blocks. Lets the CFG view show the real
	// test (e.g. "CUST-BALANCE < 0") on the decision node instead of a bare "IF".
	Cond string
}

// CFG is the control-flow graph for one function/procedure.
type CFG struct {
	FuncID string // Symbol.ID of the function
	Blocks []*BasicBlock
	Entry  int   // ID of the entry block
	Exits  []int // IDs of exit blocks (return/escape)
}

// Block returns the block with the given ID, or nil.
func (c *CFG) Block(id int) *BasicBlock {
	if id < 0 || id >= len(c.Blocks) {
		return nil
	}
	return c.Blocks[id]
}

// CFGSet holds all CFGs for one module, keyed by symbolID.
type CFGSet map[string]*CFG

// ─── Builder ──────────────────────────────────────────────────

// Build produces a CFGSet from a parsed Module.
// For Go functions it builds a real multi-block CFG.
// For TS/Python/CAGen functions it uses buildStructuralCFG which
// extracts branch/loop/return nodes already emitted by those frontends,
// producing a multi-block CFG when structural nodes are present, or
// a single-block CFG when the body is fully opaque.
func Build(m *ast.Module, idx *symbols.Index) CFGSet {
	cfgs := make(CFGSet)
	for _, n := range m.Nodes {
		switch n.Kind {
		case ast.NodeFunction, ast.NodeMethod:
			symKind := symbols.SymFunc
			if n.Kind == ast.NodeMethod {
				symKind = symbols.SymMethod
				recvName := ""
				if n.Receiver != nil {
					recvName = n.Receiver.Name
				}
				id := symbols.SymbolID(m.FilePath, symKind, recvName+"."+n.Name)
				cfgs[id] = buildFuncCFG(id, n, m.Language)
			} else {
				id := symbols.SymbolID(m.FilePath, symKind, n.Name)
				cfgs[id] = buildFuncCFG(id, n, m.Language)
			}
		case ast.NodeProcedure:
			id := symbols.SymbolID(m.FilePath, symbols.SymProcedure, n.Name)
			cfgs[id] = buildProcedureCFG(id, n)
		}
	}
	return cfgs
}

// buildFuncCFG dispatches to the correct CFG builder based on language.
//
// Go → buildGoCFG (full expression-level CFG from native go/ast children).
// TS/Python → buildStructuralCFG when structural nodes exist in children;
//
//	falls back to single-block when the body is completely opaque.
//
// CA Gen functions (not procedures) → buildStructuralCFG.
func buildFuncCFG(id string, n *ast.Node, lang ast.Language) *CFG {
	// Go always uses the native multi-block builder.
	if lang == ast.LangGo && len(n.Children) > 0 {
		return buildGoCFG(id, n.Children)
	}

	// TS, Python, CA Gen: if there are structural nodes in the children
	// (possibly wrapped in an opaque body), build a real CFG.
	// cfg_ts_py.go provides buildStructuralCFG and hasStructuralNodes.
	if lang != ast.LangGo && hasStructuralNodes(n.Children) {
		return buildStructuralCFG(id, n)
	}

	// Fully opaque body — single block (covers empty Go functions too).
	cfg := &CFG{FuncID: id}
	entry := &BasicBlock{ID: 0, Label: "entry+body+exit"}
	for _, c := range n.Children {
		entry.Stmts = append(entry.Stmts, c)
	}
	cfg.Blocks = []*BasicBlock{entry}
	cfg.Entry = 0
	cfg.Exits = []int{0}
	return cfg
}

// buildGoCFG creates a simplified CFG from Go function body nodes.
// We model: calls, returns, branches, loops.
func buildGoCFG(id string, stmts []*ast.Node) *CFG {
	cfg := &CFG{FuncID: id}
	b := &cfgBuilder{cfg: cfg, deadBlocks: true}
	b.entry = b.newBlock("entry")
	b.current = b.entry

	for _, stmt := range stmts {
		b.processNode(stmt)
	}

	// v23.8 — prune unreachable blocks before finalizing exits. When every
	// branch of an if/else returns, the NodeBranch case still allocates a merge
	// block that nothing flows into (0 predecessors); it is correct that the
	// join is unreachable, but leaving the empty orphan in the CFG clutters the
	// graph view. Drop unreachable blocks (reachable set from Entry) and remap
	// IDs to a dense range, rewriting Entry/Exits/Succs/Preds consistently.
	pruneUnreachable(cfg)

	// Ensure there is an exit block. After pruning, b.current may have been an
	// orphan that was removed, so derive the fallback exit from the surviving
	// blocks rather than the (possibly dropped) builder cursor.
	if len(cfg.Exits) == 0 && len(cfg.Blocks) > 0 {
		last := cfg.Blocks[len(cfg.Blocks)-1]
		last.Label += "+exit"
		cfg.Exits = []int{last.ID}
	}

	return cfg
}

// pruneUnreachable removes blocks not reachable from cfg.Entry and renumbers
// the survivors to a dense 0..n-1 range (preserving order), rewriting Entry,
// Exits, and every block's ID/Succs/Preds. Blocks are kept index-aligned so
// CFG.Block(id) (which indexes Blocks by position) stays valid. Entry is always
// retained. Idempotent and safe on already-dense CFGs.
func pruneUnreachable(cfg *CFG) {
	if cfg == nil || len(cfg.Blocks) == 0 {
		return
	}
	byID := make(map[int]*BasicBlock, len(cfg.Blocks))
	for _, blk := range cfg.Blocks {
		byID[blk.ID] = blk
	}
	// BFS over successors from Entry.
	//
	// v26.58 (D103): a block with statements and no predecessors is dead code
	// (statements after a return). It is a root of the walk too, so it and the
	// blocks it flows into survive; AnalyzeDataFlow reports it as dead_code.
	// Only EMPTY orphans, such as the join of an if/else whose arms both
	// return, are dropped, which is what v23.8 set out to do.
	reachable := map[int]bool{}
	queue := []int{cfg.Entry}
	for _, blk := range cfg.Blocks {
		if blk.ID != cfg.Entry && len(blk.Preds) == 0 && len(blk.Stmts) > 0 {
			queue = append(queue, blk.ID)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if reachable[cur] {
			continue
		}
		reachable[cur] = true
		if blk := byID[cur]; blk != nil {
			for _, s := range blk.Succs {
				if !reachable[s] {
					queue = append(queue, s)
				}
			}
		}
	}
	if len(reachable) == len(cfg.Blocks) {
		return // nothing to prune
	}

	// Keep survivors in original order; map old ID -> new dense ID.
	remap := map[int]int{}
	var survivors []*BasicBlock
	for _, blk := range cfg.Blocks {
		if reachable[blk.ID] {
			remap[blk.ID] = len(survivors)
			survivors = append(survivors, blk)
		}
	}
	// Rewrite each survivor's ID and its edge lists (dropping any edge that
	// pointed to or from a pruned block).
	remapEdges := func(ids []int) []int {
		out := make([]int, 0, len(ids))
		for _, id := range ids {
			if n, ok := remap[id]; ok {
				out = append(out, n)
			}
		}
		return out
	}
	for _, blk := range survivors {
		blk.Succs = remapEdges(blk.Succs)
		blk.Preds = remapEdges(blk.Preds)
		blk.ID = remap[blk.ID]
	}
	cfg.Blocks = survivors
	if n, ok := remap[cfg.Entry]; ok {
		cfg.Entry = n
	} else {
		cfg.Entry = 0
	}
	var exits []int
	for _, e := range cfg.Exits {
		if n, ok := remap[e]; ok {
			exits = append(exits, n)
		}
	}
	cfg.Exits = exits
}

type cfgBuilder struct {
	cfg     *CFG
	current *BasicBlock
	entry   *BasicBlock
	nextID  int
	// deadBlocks makes processNode put statements after a return into a block
	// with no predecessors instead of dropping them (v26.58, D103). Set only
	// by buildGoCFG, where the node order is the source order.
	deadBlocks bool
}

func (b *cfgBuilder) newBlock(label string) *BasicBlock {
	block := &BasicBlock{ID: b.nextID, Label: label}
	b.nextID++
	b.cfg.Blocks = append(b.cfg.Blocks, block)
	return block
}

func (b *cfgBuilder) edge(from, to *BasicBlock) {
	if from == nil || to == nil {
		return
	}
	from.Succs = appendUnique(from.Succs, to.ID)
	to.Preds = appendUnique(to.Preds, from.ID)
}

func (b *cfgBuilder) processNode(n *ast.Node) {
	if n == nil {
		return
	}
	if b.current == nil {
		// v26.58 (D103): after a return the cursor is nil, and every following
		// statement was dropped, so AnalyzeDataFlow never saw dead code. In the
		// Go builder the statement now opens a block with no predecessors.
		// pruneUnreachable keeps such a block because it holds statements, so
		// the flow view shows the dead code and the dead_code check finds it.
		// The structural TS/Python builder keeps the old drop: those frontends
		// emit a return before the calls of its own expression, so a node after
		// a return there is not dead code.
		if !b.deadBlocks {
			return
		}
		b.current = b.newBlock("unreachable")
	}
	switch n.Kind {
	case ast.NodeCall:
		b.current.Stmts = append(b.current.Stmts, n)

	case ast.NodeReturn:
		b.current.Stmts = append(b.current.Stmts, n)
		b.cfg.Exits = appendUnique(b.cfg.Exits, b.current.ID)
		// Statements after return are unreachable
		b.current = nil

	case ast.NodeBranch:
		// v23.41 — EVALUATE (COBOL) is a MULTI-WAY branch: its children are WHEN
		// nodes, each a NodeBranch whose RawSource is the case value. Build one
		// decision block that fans out to a case-block per WHEN, all rejoining at
		// a shared merge — instead of nesting each WHEN as a fake if (which
		// produced the "decision / decision / decision" cascade with no labels).
		//
		// v25.9 — Go's `switch`, `type-switch` and `select` are the same shape and
		// now emit the same IR (a case-node per clause carrying its value in
		// RawSource), so they take this path too. Previously they flattened every
		// case body into Children with no Else, and the if/else path below turned
		// them into a cascade of fake decisions whose conditions were borrowed
		// from the `if`s inside the case bodies.
		if isMultiWayBranch(n) {
			pred := b.current
			// Evaluate any non-arm children (e.g. calls inside a Go switch tag)
			// in the DECIDING block first, before any case block exists. Doing
			// this in the fan-out loop would append them to whichever case block
			// happened to be current — order-dependent and wrong.
			for _, ch := range n.Children {
				if ch != nil && ch.Kind != ast.NodeBranch {
					b.processNode(ch)
				}
			}
			pred = b.current // processNode may have advanced the block
			if pred != nil && n.RawSource != "" && pred.Cond == "" {
				pred.Cond = multiWayLabel(n)
			}
			mergeBlock := b.newBlock("merge")
			for _, when := range n.Children {
				if when == nil || when.Kind != ast.NodeBranch {
					continue // already evaluated in the deciding block above
				}
				caseBlock := b.newBlock("case")
				// carry the case value so the edge/label and block can show it
				caseBlock.Cond = when.RawSource
				b.edge(pred, caseBlock)
				b.current = caseBlock
				for _, child := range when.Children {
					b.processNode(child)
				}
				if b.current != nil {
					b.edge(b.current, mergeBlock)
				}
			}
			// v26.59 (N03): a Go switch or type switch with no default clause
			// runs no case when no value matches and goes on after the switch,
			// so the deciding block flows into the merge. Without this edge the
			// code after a switch whose cases all return was reported as
			// dead_code. A select with no default blocks until a case runs, so
			// it gets no such edge.
			if n.Language == ast.LangGo && (n.Name == "switch" || n.Name == "type-switch") && !hasDefaultCase(n) {
				b.edge(pred, mergeBlock)
			}
			b.current = mergeBlock
			return
		}

		// if/switch: then-block, optional else-block, merge-block
		pred := b.current
		// v23.39 — record the condition text on the deciding block so the CFG
		// view can render a real decision node. RawSource carries it (set by the
		// Go/COBOL frontends); the branch node itself is also kept as a stmt.
		if pred != nil && n.RawSource != "" && pred.Cond == "" {
			pred.Cond = n.RawSource
		}
		thenBlock := b.newBlock("then")
		mergeBlock := b.newBlock("merge")
		b.edge(pred, thenBlock)

		b.current = thenBlock
		for _, child := range n.Children {
			b.processNode(child)
		}
		if b.current != nil {
			b.edge(b.current, mergeBlock)
		}

		// v23.7 — if there is an explicit else body, give it its OWN block
		// (pred → else → merge) instead of a bare pred → merge fall-through.
		if len(n.Else) > 0 {
			elseBlock := b.newBlock("else")
			b.edge(pred, elseBlock)
			b.current = elseBlock
			for _, child := range n.Else {
				b.processNode(child)
			}
			if b.current != nil {
				b.edge(b.current, mergeBlock)
			}
		} else {
			// no else: the false path falls straight through to merge
			b.edge(pred, mergeBlock)
		}
		b.current = mergeBlock

	case ast.NodeTry:
		// v24: model try/catch as a branch-like split so the body is real flow,
		// not a lone "try {" node. The try body is the normal path; catch/finally
		// (carried in n.Else by the Java frontend) is the exception path; both
		// converge on a merge block. try-with-resources / harvested calls in
		// Children that precede the body are processed as part of the try body.
		pred := b.current
		if pred != nil && pred.Cond == "" {
			pred.Cond = "try" // labels the split so the CFG shows a decision
		}
		tryBlock := b.newBlock("try_body")
		mergeBlock := b.newBlock("try_merge")
		b.edge(pred, tryBlock)
		b.current = tryBlock
		for _, child := range n.Children {
			b.processNode(child)
		}
		if b.current != nil {
			b.edge(b.current, mergeBlock)
		}
		if len(n.Else) > 0 {
			catchBlock := b.newBlock("catch")
			b.edge(pred, catchBlock) // exception path from the try
			b.current = catchBlock
			for _, child := range n.Else {
				b.processNode(child)
			}
			if b.current != nil {
				b.edge(b.current, mergeBlock)
			}
		} else {
			b.edge(pred, mergeBlock)
		}
		b.current = mergeBlock

	case ast.NodeLoop:
		// for/while: loop-header, body, merge
		pred := b.current
		headerBlock := b.newBlock("loop_header")
		// v23.43 — the header is the decision block; show the loop condition
		// (e.g. "until WS-FEE-INDEX >= TXN-FEE-COUNT") instead of a bare label.
		if n.RawSource != "" {
			headerBlock.Cond = n.RawSource
		}
		bodyBlock := b.newBlock("loop_body")
		mergeBlock := b.newBlock("loop_exit")
		b.edge(pred, headerBlock)
		b.edge(headerBlock, bodyBlock)
		b.edge(headerBlock, mergeBlock) // loop may not execute

		b.current = bodyBlock
		for _, child := range n.Children {
			b.processNode(child)
		}
		if b.current != nil {
			b.edge(b.current, headerBlock) // back edge
		}
		b.current = mergeBlock

	case ast.NodeDefer:
		// Defer: record but don't change flow structure
		b.current.Stmts = append(b.current.Stmts, n)

	default:
		b.current.Stmts = append(b.current.Stmts, n)
		for _, child := range n.Children {
			b.processNode(child)
		}
	}
}

// ─── CA Gen CFG ───────────────────────────────────────────────

func buildProcedureCFG(id string, n *ast.Node) *CFG {
	cfg := &CFG{FuncID: id}
	b := &cfgBuilder{cfg: cfg}
	b.entry = b.newBlock("entry")
	b.current = b.entry

	for _, child := range n.Children {
		b.processCAGenNode(child)
	}

	if len(cfg.Exits) == 0 && b.current != nil {
		b.current.Label += "+exit"
		cfg.Exits = []int{b.current.ID}
	}

	// v26.59 (N04): an IF whose THEN and ELSE both end in EXIT or ESCAPE
	// leaves an if_merge block that nothing reaches and that holds nothing.
	// This builder never pruned, so AnalyzeDataFlow reported the empty block
	// as dead_code. pruneUnreachable drops only empty orphans, as in the Go
	// builder; a block with statements after such an IF stays and is still
	// reported. It runs after the exit fallback: an orphan exists only when
	// some arm exited, and then cfg.Exits is not empty, so the fallback never
	// names a block that pruning removes.
	pruneUnreachable(cfg)

	return cfg
}

func (b *cfgBuilder) processCAGenNode(n *ast.Node) {
	if n == nil || b.current == nil {
		return
	}
	switch n.Kind {
	case ast.NodeLoop: // READ EACH, FOR EACH
		pred := b.current
		headerBlock := b.newBlock("read_each_header")
		bodyBlock := b.newBlock("read_each_body")
		mergeBlock := b.newBlock("read_each_exit")
		b.edge(pred, headerBlock)
		b.edge(headerBlock, bodyBlock)
		b.edge(headerBlock, mergeBlock)
		b.current = bodyBlock
		for _, child := range n.Children {
			b.processCAGenNode(child)
		}
		if b.current != nil {
			b.edge(b.current, headerBlock)
		}
		b.current = mergeBlock

	case ast.NodeBranch: // IF / WHERE
		pred := b.current
		thenBlock := b.newBlock("if_true")
		mergeBlock := b.newBlock("if_merge")
		b.edge(pred, thenBlock)
		b.current = thenBlock
		for _, child := range n.Children {
			b.processCAGenNode(child)
		}
		if b.current != nil {
			b.edge(b.current, mergeBlock)
		}
		// v26.57 (D014): an ELSE body gets its own block (pred → else → merge),
		// as processNode does. Every COBOL SECTION is built here, and its ELSE
		// body used to have no block at all.
		if len(n.Else) > 0 {
			elseBlock := b.newBlock("if_false")
			b.edge(pred, elseBlock)
			b.current = elseBlock
			for _, child := range n.Else {
				b.processCAGenNode(child)
			}
			if b.current != nil {
				b.edge(b.current, mergeBlock)
			}
		} else {
			b.edge(pred, mergeBlock)
		}
		b.current = mergeBlock

	case ast.NodeReturn, ast.NodeEscape: // EXIT / ESCAPE
		b.current.Stmts = append(b.current.Stmts, n)
		b.cfg.Exits = appendUnique(b.cfg.Exits, b.current.ID)
		b.current = nil

	default:
		b.current.Stmts = append(b.current.Stmts, n)
		for _, child := range n.Children {
			b.processCAGenNode(child)
		}
	}
}

// ─── Dataflow Analysis ────────────────────────────────────────

// DataFlowIssue describes a potential data-flow problem.
type DataFlowIssue struct {
	Kind    string // "use_before_def", "dead_code", "unread_var"
	Message string
	Span    ast.Span
}

// AnalyzeDataFlow performs reaching-definitions analysis over a CFG.
// Returns any issues found.
func AnalyzeDataFlow(cfg *CFG, table *symbols.Table) []DataFlowIssue {
	if cfg == nil || len(cfg.Blocks) == 0 {
		return nil
	}
	var issues []DataFlowIssue

	// Dead code: blocks with no predecessors (except entry)
	for _, b := range cfg.Blocks {
		if b.ID == cfg.Entry {
			continue
		}
		if len(b.Preds) == 0 {
			issues = append(issues, DataFlowIssue{
				Kind:    "dead_code",
				Message: fmt.Sprintf("block %q (%d) is unreachable", b.Label, b.ID),
			})
		}
	}

	return issues
}

// ─── Effect Tagging ───────────────────────────────────────────

// EffectKind describes the kind of side effect a function has.
type EffectKind string

const (
	EffectDB      EffectKind = "db"      // touches a database
	EffectFS      EffectKind = "fs"      // touches the filesystem
	EffectNetwork EffectKind = "network" // makes network calls
	EffectPure    EffectKind = "pure"    // no side effects
	EffectUnknown EffectKind = "unknown" // cannot determine
)

// FuncEffects holds the inferred effects for one function.
type FuncEffects struct {
	SymbolID string
	Effects  map[EffectKind]bool
	// DBWrite is true when a DB callee writes (see dbWritePatterns).
	//
	// v26.58 (D100): EffectDB is set for any database access, reads included,
	// so the verify pass reported every Get and List of a PG store as a read
	// that writes. The write is a separate field, not a new EffectKind, so the
	// Effects map that the Studio, belief and objgraph list stays as it was.
	DBWrite bool
}

// dbPatterns: package/function names that indicate DB access.
var dbPatterns = []string{
	"sql.", "db.", "gorm.", "sqlx.", "pgx.", "bun.", "ent.",
	"DB.", "Tx.", "Exec", "Query", "QueryRow", "Prepare",
	"WRITE", "READ", "DELETE", "INSERT", "UPDATE", // CA Gen
}

// dbWritePatterns: the subset of DB callees that write. Go writes go through
// Exec (Exec, ExecContext, NamedExec); CA Gen names the verb. READ and Query
// only read. v26.58 (D100).
var dbWritePatterns = []string{"Exec", "WRITE", "INSERT", "UPDATE", "DELETE"}

var fsPatterns = []string{
	"os.", "ioutil.", "afero.", "filepath.",
	"Create", "Open", "WriteFile", "ReadFile", "Remove", "Rename",
}

var netPatterns = []string{
	"http.", "grpc.", "net.", "rpc.", "websocket.",
	"Get", "Post", "Do", "Dial", "Connect",
}

// InferEffects examines every function body in the module and tags
// its side effects based on callee names.
func InferEffects(m *ast.Module, cfgSet CFGSet) map[string]*FuncEffects {
	out := make(map[string]*FuncEffects)

	for _, n := range m.Nodes {
		switch n.Kind {
		case ast.NodeFunction:
			id := symbols.SymbolID(m.FilePath, symbols.SymFunc, n.Name)
			out[id] = inferNodeEffects(id, n.Children, m.Language)
		case ast.NodeMethod:
			recvName := ""
			if n.Receiver != nil {
				recvName = n.Receiver.Name
			}
			id := symbols.SymbolID(m.FilePath, symbols.SymMethod, recvName+"."+n.Name)
			out[id] = inferNodeEffects(id, n.Children, m.Language)
		case ast.NodeProcedure:
			id := symbols.SymbolID(m.FilePath, symbols.SymProcedure, n.Name)
			out[id] = inferNodeEffects(id, n.Children, m.Language)
		}
	}
	return out
}

func inferNodeEffects(id string, children []*ast.Node, lang ast.Language) *FuncEffects {
	efx := &FuncEffects{
		SymbolID: id,
		Effects:  make(map[EffectKind]bool),
	}

	var callees []string
	var walk func(nodes []*ast.Node)
	walk = func(nodes []*ast.Node) {
		for _, n := range nodes {
			if n == nil {
				continue
			}
			if n.Kind == ast.NodeCall && n.Callee != "" {
				callees = append(callees, n.Callee)
			}
			// v18.4 (R2): the CA Gen frontend emits WRITE/READ as
			// NodeAssign with the verb in Name (e.g. "WRITE ORDER"),
			// not NodeCall. DELETE comes through as NodeCall. Without
			// this branch the verb-prefix check below missed every WRITE
			// and READ in CA Gen procedures, leaving the effect set as
			// EffectPure for any procedure that only did IO.
			if n.Kind == ast.NodeAssign && lang == ast.LangCAGen && n.Name != "" {
				callees = append(callees, n.Name)
			}
			walk(n.Children)
			// v26.57 (D014): the Go, Java and COBOL frontends put else and
			// catch bodies in n.Else. Walking Children alone tagged a function
			// pure when its only database call sat in an else branch.
			walk(n.Else)
		}
	}
	walk(children)

	// CA Gen procedures: WRITE/READ/DELETE are DB operations.
	if lang == ast.LangCAGen {
		for _, c := range callees {
			cu := strings.ToUpper(c)
			if strings.HasPrefix(cu, "WRITE") || strings.HasPrefix(cu, "READ") ||
				strings.HasPrefix(cu, "DELETE") || strings.HasPrefix(cu, "INSERT") ||
				strings.HasPrefix(cu, "UPDATE") {
				efx.Effects[EffectDB] = true
				if !strings.HasPrefix(cu, "READ") {
					efx.DBWrite = true // v26.58 (D100)
				}
			}
		}
	}

	for _, callee := range callees {
		if matchesAny(callee, dbPatterns) {
			efx.Effects[EffectDB] = true
			if matchesAny(callee, dbWritePatterns) {
				efx.DBWrite = true // v26.58 (D100)
			}
		}
		if matchesAny(callee, fsPatterns) {
			efx.Effects[EffectFS] = true
		}
		if matchesAny(callee, netPatterns) {
			efx.Effects[EffectNetwork] = true
		}
	}

	if !efx.Effects[EffectDB] && !efx.Effects[EffectFS] && !efx.Effects[EffectNetwork] {
		if len(callees) == 0 {
			efx.Effects[EffectPure] = true
		} else {
			efx.Effects[EffectUnknown] = true
		}
	}

	return efx
}

// isMultiWayBranch reports whether a NodeBranch models a MULTI-WAY construct
// (COBOL EVALUATE, Go switch / type-switch / select) rather than a two-way
// if/else. Such a node's case arms are child NodeBranch nodes, each carrying its
// case value in RawSource. Requires at least one NodeBranch child — a `switch {}`
// with no cases degenerates to the if/else path harmlessly.
//
// NOTE: the arms are matched by KIND, not by name. COBOL emits "WHEN" *and*
// "WHEN_OTHER"; Go emits "case". Matching a single literal name would silently
// regress an EVALUATE whose only arm is WHEN OTHER. v25.9.
func isMultiWayBranch(n *ast.Node) bool {
	switch n.Name {
	case "EVALUATE", "switch", "type-switch", "select", "match":
		// v26.6: Python 3.10 match/case fans out like a switch/EVALUATE — a parent
		// branch whose Children are per-case NodeBranch arms. Recognising it here
		// gives match the same multi-way lane-column layout (v26.1) for free.
	default:
		return false
	}
	for _, ch := range n.Children {
		if ch != nil && ch.Kind == ast.NodeBranch {
			return true
		}
	}
	return false
}

// hasDefaultCase reports whether a Go switch or type switch has a default
// clause. The Go frontend (caseClauses) labels that clause "default" in its
// RawSource; a case value can never be the keyword itself. v26.59 (N03).
func hasDefaultCase(n *ast.Node) bool {
	for _, ch := range n.Children {
		if ch != nil && ch.Kind == ast.NodeBranch && ch.RawSource == "default" {
			return true
		}
	}
	return false
}

// multiWayLabel renders the deciding block's condition text for a multi-way
// branch: "EVALUATE <subject>" for COBOL, "switch <tag>" for Go. v25.9.
func multiWayLabel(n *ast.Node) string {
	switch n.Name {
	case "EVALUATE":
		return "EVALUATE " + n.RawSource
	case "switch":
		return "switch " + n.RawSource
	case "match":
		return "match " + n.RawSource // v26.6: Python match subject
	default:
		return n.Name
	}
}

func matchesAny(callee string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(callee, p) {
			return true
		}
	}
	return false
}

// ─── Helpers ─────────────────────────────────────────────────

func appendUnique(s []int, v int) []int {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
