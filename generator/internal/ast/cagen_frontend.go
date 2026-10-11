package ast

// ─────────────────────────────────────────────────────────────
// cagen_frontend.go: CA Gen (Broadcom Gen) action diagram frontend
//
// Parses CA Gen action diagrams, the procedural logic layer of
// CA Gen applications used in Tier-1 banking/insurance systems.
//
// Supported constructs (the "sufficient subset" for GenSystem):
//   PROCEDURE <name>
//   READ EACH <entity> [SORTED BY <attr>]
//   WHERE <condition>
//   WRITE <entity>
//   DELETE <entity>
//   CALL <procedure> [USING <view>]
//   SET <attr> TO <value>
//   IF <condition> ... ELSE ... END
//   FOR EACH <entity> WHERE <condition>
//   ESCAPE [action-block | procedure | read-each]
//   EXIT [STATE | PROCEDURE]
//   MOVE <value> TO <attr>
//   <entity>.<attribute> references
//
// Unknown constructs are captured as NodeUnknown with their raw
// source text so the human presentation layer can review them.
// ─────────────────────────────────────────────────────────────

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// CAGenFrontend implements Frontend for CA Gen action diagrams.
type CAGenFrontend struct{}

func init() { Register(&CAGenFrontend{}) }

func (c *CAGenFrontend) Language() Language   { return LangCAGen }
func (c *CAGenFrontend) Extensions() []string { return []string{".ad", ".cagen", ".gel"} }
func (c *CAGenFrontend) Available() bool      { return true } // pure Go

// ─── Parse ───────────────────────────────────────────────────

func (c *CAGenFrontend) Parse(filePath string, source []byte) (*Module, error) {
	sum := sha256.Sum256(source)
	m := &Module{
		Language:  LangCAGen,
		FilePath:  filePath,
		Checksum:  fmt.Sprintf("%x", sum),
		RawSource: source,
	}

	p := &caGenParser{
		filePath: filePath,
		lines:    tokenizeCAGen(source),
	}
	p.parseFile(m)

	// Derive package/module name from the first PROCEDURE name if any
	for _, n := range m.Nodes {
		if n.Kind == NodeProcedure {
			m.PackageName = n.Name
			break
		}
	}

	return m, nil
}

func (c *CAGenFrontend) Emit(module *Module) ([]byte, error) {
	return module.RawSource, nil
}

// ─── Tokenizer ───────────────────────────────────────────────

type caGenLine struct {
	lineNo  int
	raw     string
	tokens  []string // uppercase tokens (CA Gen is case-insensitive)
	rawToks []string // original-case tokens
	depth   int      // indentation depth (2 spaces per level)
}

func tokenizeCAGen(src []byte) []caGenLine {
	var lines []caGenLine
	rawLines := strings.Split(string(src), "\n")
	for i, raw := range rawLines {
		// Count leading spaces for indentation
		trimmed := strings.TrimLeft(raw, " \t")
		spaces := len(raw) - len(trimmed)
		depth := spaces / 2

		// v26.58 (D057): the line was cut at the first " * " as an inline
		// comment, which dropped the rest of a multiplication such as
		// "SET A TO B * C". A "*" starts a comment only at the start of the
		// trimmed line, so only that check remains.
		if strings.HasPrefix(trimmed, "* ") || trimmed == "*" {
			continue // comment line
		}

		rawToks := strings.Fields(trimmed)
		upper := make([]string, len(rawToks))
		for j, t := range rawToks {
			upper[j] = strings.ToUpper(t)
		}

		if len(rawToks) == 0 {
			continue
		}

		lines = append(lines, caGenLine{
			lineNo:  i + 1,
			raw:     raw,
			tokens:  upper,
			rawToks: rawToks,
			depth:   depth,
		})
	}
	return lines
}

// ─── Parser ───────────────────────────────────────────────────

type caGenParser struct {
	filePath string
	lines    []caGenLine
	pos      int
}

func (p *caGenParser) peek() (caGenLine, bool) {
	if p.pos >= len(p.lines) {
		return caGenLine{}, false
	}
	return p.lines[p.pos], true
}

func (p *caGenParser) consume() (caGenLine, bool) {
	l, ok := p.peek()
	if ok {
		p.pos++
	}
	return l, ok
}

func (p *caGenParser) parseFile(m *Module) {
	for {
		l, ok := p.peek()
		if !ok {
			break
		}
		if len(l.tokens) == 0 {
			p.consume()
			continue
		}

		switch l.tokens[0] {
		case "PROCEDURE":
			n := p.parseProcedure()
			m.Nodes = append(m.Nodes, n)
		case "ACTION", "BLOCK":
			n := p.parseActionBlock()
			m.Nodes = append(m.Nodes, n)
		default:
			// Treat top-level non-PROCEDURE lines as unknown
			p.consume()
			m.Nodes = append(m.Nodes, &Node{
				Kind:          NodeUnknown,
				Language:      LangCAGen,
				Span:          Span{Start: Pos{Line: l.lineNo, Column: 1}},
				RawSource:     l.raw,
				UnknownReason: "unexpected_top_level: " + l.tokens[0],
			})
		}
	}
}

// ─── PROCEDURE ───────────────────────────────────────────────

func (p *caGenParser) parseProcedure() *Node {
	l, _ := p.consume()
	name := "UNKNOWN_PROCEDURE"
	if len(l.rawToks) >= 2 {
		name = l.rawToks[1]
	}

	n := &Node{
		ID:       "procedure:" + name,
		Kind:     NodeProcedure,
		Name:     name,
		Language: LangCAGen,
		Span:     Span{Start: Pos{Line: l.lineNo, Column: 1}},
	}

	// Parse the procedure body until we hit another PROCEDURE or EOF
	procDepth := l.depth
	for {
		next, ok := p.peek()
		if !ok {
			break
		}
		if next.depth <= procDepth && len(next.tokens) > 0 && next.tokens[0] == "PROCEDURE" {
			break
		}
		child := p.parseStatement()
		if child != nil {
			n.Children = append(n.Children, child)
		}
	}

	n.Span.End = Pos{Line: p.lastLine()}
	return n
}

// ─── ACTION BLOCK ─────────────────────────────────────────────

func (p *caGenParser) parseActionBlock() *Node {
	l, _ := p.consume()
	name := ""
	if len(l.rawToks) >= 2 {
		name = l.rawToks[1]
	}
	n := &Node{
		ID:       "action:" + name,
		Kind:     NodeProcedure,
		Name:     name,
		Language: LangCAGen,
		Span:     Span{Start: Pos{Line: l.lineNo, Column: 1}},
	}
	baseDepth := l.depth
	for {
		next, ok := p.peek()
		if !ok || (next.depth <= baseDepth && len(next.tokens) > 0 && next.tokens[0] == "END") {
			break
		}
		child := p.parseStatement()
		if child != nil {
			n.Children = append(n.Children, child)
		}
	}
	return n
}

// ─── Statements ───────────────────────────────────────────────

func (p *caGenParser) parseStatement() *Node {
	l, ok := p.peek()
	if !ok || len(l.tokens) == 0 {
		p.consume()
		return nil
	}

	switch l.tokens[0] {
	case "READ":
		return p.parseReadEach()
	case "FOR":
		if len(l.tokens) >= 2 && l.tokens[1] == "EACH" {
			return p.parseForEach()
		}
	case "WRITE":
		return p.parseWrite()
	case "DELETE":
		return p.parseDelete()
	case "CALL":
		return p.parseCall()
	case "SET", "MOVE":
		return p.parseAssign()
	case "IF":
		return p.parseIf()
	case "ELSE":
		return p.parseElse()
	case "END":
		p.consume()
		return nil
	case "ESCAPE":
		return p.parseEscape()
	case "EXIT":
		return p.parseExit()
	case "WHERE":
		return p.parseWhere()
	}

	// Unknown statement: capture raw and continue
	p.consume()
	return &Node{
		Kind:          NodeUnknown,
		Language:      LangCAGen,
		Span:          Span{Start: Pos{Line: l.lineNo, Column: 1}},
		RawSource:     l.raw,
		UnknownReason: "unrecognized_statement: " + l.tokens[0],
	}
}

func (p *caGenParser) parseReadEach() *Node {
	l, _ := p.consume()
	// READ EACH <entity> [SORTED BY <attr>] / READ <entity>
	entity := ""
	var sortAttr string
	toks := l.tokens

	if len(toks) >= 3 && toks[1] == "EACH" {
		entity = l.rawToks[2]
		// SORTED BY ...
		for i := 3; i < len(toks)-1; i++ {
			if toks[i] == "SORTED" && toks[i+1] == "BY" && i+2 < len(toks) {
				sortAttr = l.rawToks[i+2]
				break
			}
		}
	} else if len(toks) >= 2 {
		entity = l.rawToks[1]
	}

	n := &Node{
		ID:       fmt.Sprintf("read:%s@%d", entity, l.lineNo),
		Kind:     NodeLoop, // READ EACH is effectively a loop
		Name:     "READ EACH " + entity,
		Language: LangCAGen,
		Span:     Span{Start: Pos{Line: l.lineNo, Column: 1}},
	}
	if sortAttr != "" {
		n.Children = append(n.Children, &Node{
			Kind: NodeVariable, Name: "SORTED_BY", ValueLiteral: sortAttr,
		})
	}
	// Parse WHERE and nested body
	readDepth := l.depth
	for {
		next, ok := p.peek()
		if !ok || (next.depth <= readDepth && len(next.tokens) > 0 &&
			(next.tokens[0] == "END" || next.tokens[0] == "PROCEDURE" || next.tokens[0] == "READ")) {
			break
		}
		child := p.parseStatement()
		if child != nil {
			n.Children = append(n.Children, child)
		}
	}
	// Record the entity being read as a "call" node for dependency tracking
	n.Callee = entity
	return n
}

func (p *caGenParser) parseForEach() *Node {
	l, _ := p.consume()
	entity := ""
	if len(l.rawToks) >= 3 {
		entity = l.rawToks[2]
	}
	n := &Node{
		ID:       fmt.Sprintf("foreach:%s@%d", entity, l.lineNo),
		Kind:     NodeLoop,
		Name:     "FOR EACH " + entity,
		Language: LangCAGen,
		Span:     Span{Start: Pos{Line: l.lineNo, Column: 1}},
		Callee:   entity,
	}
	baseDepth := l.depth
	for {
		next, ok := p.peek()
		if !ok || (next.depth <= baseDepth && len(next.tokens) > 0 && next.tokens[0] == "END") {
			if ok && next.tokens[0] == "END" {
				p.consume()
			}
			break
		}
		child := p.parseStatement()
		if child != nil {
			n.Children = append(n.Children, child)
		}
	}
	return n
}

func (p *caGenParser) parseWrite() *Node {
	l, _ := p.consume()
	entity := ""
	if len(l.rawToks) >= 2 {
		entity = l.rawToks[1]
	}
	return &Node{
		ID:        fmt.Sprintf("write:%s@%d", entity, l.lineNo),
		Kind:      NodeAssign,
		Name:      "WRITE " + entity,
		Language:  LangCAGen,
		Span:      Span{Start: Pos{Line: l.lineNo, Column: 1}},
		Callee:    entity,
		RawSource: l.raw,
	}
}

func (p *caGenParser) parseDelete() *Node {
	l, _ := p.consume()
	entity := ""
	if len(l.rawToks) >= 2 {
		entity = l.rawToks[1]
	}
	return &Node{
		ID:        fmt.Sprintf("delete:%s@%d", entity, l.lineNo),
		Kind:      NodeCall,
		Name:      "DELETE " + entity,
		Language:  LangCAGen,
		Span:      Span{Start: Pos{Line: l.lineNo, Column: 1}},
		Callee:    entity,
		RawSource: l.raw,
	}
}

func (p *caGenParser) parseCall() *Node {
	l, _ := p.consume()
	callee := ""
	var args []string
	toks := l.tokens
	if len(toks) >= 2 {
		callee = l.rawToks[1]
	}
	for i := 2; i < len(toks); i++ {
		if toks[i] == "USING" && i+1 < len(toks) {
			args = append(args, l.rawToks[i+1:]...)
			break
		}
	}
	return &Node{
		ID:        fmt.Sprintf("call:%s@%d", callee, l.lineNo),
		Kind:      NodeCall,
		Name:      "CALL " + callee,
		Language:  LangCAGen,
		Span:      Span{Start: Pos{Line: l.lineNo, Column: 1}},
		Callee:    callee,
		CallArgs:  args,
		RawSource: l.raw,
	}
}

func (p *caGenParser) parseAssign() *Node {
	l, _ := p.consume()
	// SET <attr> TO <value>  or  MOVE <value> TO <attr>
	raw := l.raw
	var target, value string
	toks := l.tokens
	verb := toks[0]
	if verb == "SET" && len(toks) >= 4 && toks[2] == "TO" {
		target = l.rawToks[1]
		value = strings.Join(l.rawToks[3:], " ")
	} else if verb == "MOVE" && len(toks) >= 4 && toks[2] == "TO" {
		value = l.rawToks[1]
		target = l.rawToks[3]
	}

	// Detect entity.attribute pattern
	attrRef := parseCAGenAttrRef(target)

	n := &Node{
		ID:           fmt.Sprintf("assign:%s@%d", target, l.lineNo),
		Kind:         NodeAssign,
		Name:         verb + " " + target,
		Language:     LangCAGen,
		Span:         Span{Start: Pos{Line: l.lineNo, Column: 1}},
		ValueLiteral: value,
		RawSource:    raw,
	}
	if attrRef != "" {
		n.TypeHint = TypeRef{Name: attrRef}
	}
	return n
}

func (p *caGenParser) parseIf() *Node {
	l, _ := p.consume()
	condition := ""
	if len(l.rawToks) >= 2 {
		condition = strings.Join(l.rawToks[1:], " ")
	}
	n := &Node{
		ID:        fmt.Sprintf("if@%d", l.lineNo),
		Kind:      NodeBranch,
		Name:      "IF",
		Language:  LangCAGen,
		Span:      Span{Start: Pos{Line: l.lineNo, Column: 1}},
		RawSource: condition,
	}
	ifDepth := l.depth
	for {
		next, ok := p.peek()
		if !ok {
			break
		}
		if next.depth <= ifDepth && len(next.tokens) > 0 {
			if next.tokens[0] == "ELSE" || next.tokens[0] == "END" {
				break
			}
		}
		child := p.parseStatement()
		if child != nil {
			n.Children = append(n.Children, child)
		}
	}
	return n
}

func (p *caGenParser) parseElse() *Node {
	l, _ := p.consume()
	n := &Node{
		ID:       fmt.Sprintf("else@%d", l.lineNo),
		Kind:     NodeBranch,
		Name:     "ELSE",
		Language: LangCAGen,
		Span:     Span{Start: Pos{Line: l.lineNo, Column: 1}},
	}
	elseDepth := l.depth
	for {
		next, ok := p.peek()
		if !ok {
			break
		}
		if next.depth <= elseDepth && len(next.tokens) > 0 && next.tokens[0] == "END" {
			p.consume()
			break
		}
		child := p.parseStatement()
		if child != nil {
			n.Children = append(n.Children, child)
		}
	}
	return n
}

func (p *caGenParser) parseEscape() *Node {
	l, _ := p.consume()
	target := ""
	if len(l.rawToks) >= 2 {
		target = strings.Join(l.rawToks[1:], " ")
	}
	return &Node{
		ID:        fmt.Sprintf("escape@%d", l.lineNo),
		Kind:      NodeEscape,
		Name:      "ESCAPE",
		Language:  LangCAGen,
		Span:      Span{Start: Pos{Line: l.lineNo, Column: 1}},
		RawSource: target,
		Callee:    target,
	}
}

func (p *caGenParser) parseExit() *Node {
	l, _ := p.consume()
	target := "PROCEDURE"
	if len(l.rawToks) >= 2 {
		target = l.rawToks[1]
	}
	return &Node{
		ID:        fmt.Sprintf("exit@%d", l.lineNo),
		Kind:      NodeReturn,
		Name:      "EXIT " + target,
		Language:  LangCAGen,
		Span:      Span{Start: Pos{Line: l.lineNo, Column: 1}},
		RawSource: l.raw,
	}
}

func (p *caGenParser) parseWhere() *Node {
	l, _ := p.consume()
	condition := ""
	if len(l.rawToks) >= 2 {
		condition = strings.Join(l.rawToks[1:], " ")
	}
	return &Node{
		ID:        fmt.Sprintf("where@%d", l.lineNo),
		Kind:      NodeBranch,
		Name:      "WHERE",
		Language:  LangCAGen,
		Span:      Span{Start: Pos{Line: l.lineNo, Column: 1}},
		RawSource: condition,
	}
}

// ─── Helpers ──────────────────────────────────────────────────

// parseCAGenAttrRef detects "entity.attribute" patterns and returns
// the fully qualified reference, e.g. "ORDER.STATUS".
func parseCAGenAttrRef(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, ".") {
		return s
	}
	return ""
}

func (p *caGenParser) lastLine() int {
	if p.pos > 0 && p.pos <= len(p.lines) {
		return p.lines[p.pos-1].lineNo
	}
	return 0
}
