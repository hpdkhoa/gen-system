// ts_frontend.go — TASK 2 (v19): parseFuncBody replaces skipBraces at
// function/method sites, emitting NodeBranch/NodeLoop/NodeReturn/NodeCall
// children so cfg_ts_py.go can build a real multi-block CFG.
package ast

// ─────────────────────────────────────────────────────────────
// ts_frontend.go — TypeScript/TSX language frontend
//
// Fully offline, zero external dependencies. Implements a
// declaration-level recursive-descent parser that covers:
//   • import / export statements
//   • interface, type, class, enum declarations
//   • function declarations (signatures; bodies brace-skipped)
//   • const / let / var declarations
//   • abstract, async, export modifiers
//   • Generic type parameters (names captured, bodies skipped)
//
// Function/method BODIES are scanned by parseFuncBody, which:
//   - Captures raw source as a NodeUnknown{body_not_parsed} child
//     (preserving the existing presentation-layer behaviour).
//   - Also emits NodeBranch / NodeLoop / NodeReturn / NodeCall children
//     for control-flow nodes recognised in the body, so cfg_ts_py.go
//     can build a real multi-block CFG without parsing expressions.
// ─────────────────────────────────────────────────────────────

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
)

// TSFrontend implements Frontend for TypeScript and TSX.
type TSFrontend struct{}

func init() { Register(&TSFrontend{}) }

func (t *TSFrontend) Language() Language   { return LangTS }
func (t *TSFrontend) Extensions() []string { return []string{".ts", ".tsx"} }
func (t *TSFrontend) Available() bool       { return true } // pure Go, always available

// ─── Parse ───────────────────────────────────────────────────

func (t *TSFrontend) Parse(filePath string, source []byte) (*Module, error) {
	sum := sha256.Sum256(source)
	m := &Module{
		Language:    LangTS,
		FilePath:    filePath,
		PackageName: tsModuleName(filePath),
		Checksum:    fmt.Sprintf("%x", sum),
		RawSource:   source,
	}
	p := newTSParser(filePath, source)
	p.parseTopLevel(m)
	return m, nil
}

// Emit returns the raw source (TypeScript files are not reformatted —
// we preserve original bytes for unchanged regions).
func (t *TSFrontend) Emit(module *Module) ([]byte, error) {
	return module.RawSource, nil
}

// ─── Lexer ────────────────────────────────────────────────────

type tsTokenKind int

const (
	tsTOF        tsTokenKind = iota // start of file
	tsIdent                         // identifier or keyword
	tsString                        // 'x' or "x" or `x`
	tsNumber                        // numeric literal
	tsLBrace                        // {
	tsRBrace                        // }
	tsLParen                        // (
	tsRParen                        // )
	tsLAngle                        // <
	tsRAngle                        // >
	tsLBracket                      // [
	tsRBracket                      // ]
	tsComma                         // ,
	tsSemicolon                     // ;
	tsColon                         // :
	tsQuestion                      // ?
	tsEquals                        // =
	tsDot                           // .
	tsAt                            // @  (decorator)
	tsSlash                         // /
	tsAmpersand                     // &
	tsPipe                          // |
	tsEllipsis                      // ...
	tsArrow                         // =>
	tsNewline                       // \n (significant for ASI)
	tsEOF
	tsOther
)

type tsToken struct {
	kind  tsTokenKind
	text  string
	line  int
	col   int
	off   int
}

// tsLoopCond extracts a loop's condition text from the token stream: the tokens
// between the '(' following the loop keyword at index kw and its matching ')'.
// For `for (let i=0; i<n; i++)` it returns the whole header; for `while (x > 0)`
// it returns "x > 0". Returns "" if no parenthesised header is found (e.g.
// `do { } while` handled by its own while, or a malformed slice). v23.49.
func tsLoopCond(toks []tsToken, kw int) string {
	// find the first '(' at or shortly after the keyword
	i := kw + 1
	for i < len(toks) && toks[i].kind != tsLParen {
		// stop if we hit a block or another statement before a '('
		if toks[i].kind == tsLBrace || toks[i].kind == tsSemicolon {
			return ""
		}
		i++
	}
	if i >= len(toks) || toks[i].kind != tsLParen {
		return ""
	}
	depth := 0
	var parts []string
	for ; i < len(toks); i++ {
		switch toks[i].kind {
		case tsLParen:
			depth++
			if depth == 1 {
				continue // don't include the outer '('
			}
		case tsRParen:
			depth--
			if depth == 0 {
				return strings.TrimSpace(strings.Join(parts, " "))
			}
		}
		parts = append(parts, toks[i].text)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

type tsLexer struct {
	src  []byte
	pos  int
	line int
	col  int
}

func newTSLexer(src []byte) *tsLexer {
	return &tsLexer{src: src, pos: 0, line: 1, col: 1}
}

func (l *tsLexer) peek() byte {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *tsLexer) peekAt(off int) byte {
	i := l.pos + off
	if i >= len(l.src) {
		return 0
	}
	return l.src[i]
}

func (l *tsLexer) advance() byte {
	if l.pos >= len(l.src) {
		return 0
	}
	ch := l.src[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

func (l *tsLexer) skipLineComment() {
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
}

func (l *tsLexer) skipBlockComment() {
	for l.pos < len(l.src) {
		if l.src[l.pos] == '*' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/' {
			l.pos += 2
			l.col += 2
			return
		}
		l.advance()
	}
}

func (l *tsLexer) readString(delim byte) string {
	var sb strings.Builder
	sb.WriteByte(delim)
	for l.pos < len(l.src) {
		ch := l.advance()
		sb.WriteByte(ch)
		if ch == '\\' && l.pos < len(l.src) {
			sb.WriteByte(l.advance())
			continue
		}
		if ch == delim {
			break
		}
	}
	return sb.String()
}

func (l *tsLexer) readTemplate() string {
	var sb strings.Builder
	sb.WriteByte('`')
	depth := 0
	for l.pos < len(l.src) {
		ch := l.advance()
		sb.WriteByte(ch)
		if ch == '\\' && l.pos < len(l.src) {
			sb.WriteByte(l.advance())
			continue
		}
		if ch == '$' && l.peek() == '{' {
			depth++
		}
		if ch == '{' && depth > 0 {
			// skip; depth tracking is approximate
		}
		if ch == '}' && depth > 0 {
			depth--
		}
		if ch == '`' && depth == 0 {
			break
		}
	}
	return sb.String()
}

func (l *tsLexer) next() tsToken {
	for {
		if l.pos >= len(l.src) {
			return tsToken{kind: tsEOF, line: l.line, col: l.col, off: l.pos}
		}
		ch := l.peek()

		// Whitespace (not newline)
		if ch == ' ' || ch == '\t' || ch == '\r' {
			l.advance()
			continue
		}
		// Newline
		if ch == '\n' {
			line, col, off := l.line, l.col, l.pos
			l.advance()
			return tsToken{kind: tsNewline, text: "\n", line: line, col: col, off: off}
		}

		startLine, startCol, startOff := l.line, l.col, l.pos

		// Comments
		if ch == '/' {
			if l.peekAt(1) == '/' {
				l.pos += 2
				l.skipLineComment()
				continue
			}
			if l.peekAt(1) == '*' {
				l.pos += 2
				l.skipBlockComment()
				continue
			}
			l.advance()
			return tsToken{kind: tsSlash, text: "/", line: startLine, col: startCol, off: startOff}
		}

		// Strings
		if ch == '\'' || ch == '"' {
			l.advance()
			// v18 (D1#6): readString(delim) already prepends the opening
			// delimiter to its strings.Builder and consumes through the
			// closing delimiter, so its return value is the full quoted
			// literal. Concatenating string(ch) on top double-quoted every
			// string, which made stripQuotes leave a leading apostrophe and
			// broke every import path / class extends / type alias literal.
			s := l.readString(ch)
			return tsToken{kind: tsString, text: s, line: startLine, col: startCol, off: startOff}
		}
		if ch == '`' {
			l.advance()
			s := l.readTemplate()
			return tsToken{kind: tsString, text: s, line: startLine, col: startCol, off: startOff}
		}

		// Ellipsis
		if ch == '.' && l.peekAt(1) == '.' && l.peekAt(2) == '.' {
			l.pos += 3; l.col += 3
			return tsToken{kind: tsEllipsis, text: "...", line: startLine, col: startCol, off: startOff}
		}

		// Arrow
		if ch == '=' && l.peekAt(1) == '>' {
			l.pos += 2; l.col += 2
			return tsToken{kind: tsArrow, text: "=>", line: startLine, col: startCol, off: startOff}
		}

		// Single-char punctuation
		punct := map[byte]tsTokenKind{
			'{': tsLBrace, '}': tsRBrace, '(': tsLParen, ')': tsRParen,
			'<': tsLAngle, '>': tsRAngle, '[': tsLBracket, ']': tsRBracket,
			',': tsComma, ';': tsSemicolon, ':': tsColon, '?': tsQuestion,
			'=': tsEquals, '.': tsDot, '@': tsAt, '&': tsAmpersand, '|': tsPipe,
		}
		if kind, ok := punct[ch]; ok {
			l.advance()
			return tsToken{kind: kind, text: string(ch), line: startLine, col: startCol, off: startOff}
		}

		// Numbers
		if ch >= '0' && ch <= '9' {
			var sb strings.Builder
			for l.pos < len(l.src) && (l.src[l.pos] >= '0' && l.src[l.pos] <= '9' || l.src[l.pos] == '.' || l.src[l.pos] == '_') {
				sb.WriteByte(l.advance())
			}
			return tsToken{kind: tsNumber, text: sb.String(), line: startLine, col: startCol, off: startOff}
		}

		// Identifiers and keywords
		if isIdentStart(ch) {
			var sb strings.Builder
			for l.pos < len(l.src) && isIdentContinue(l.src[l.pos]) {
				sb.WriteByte(l.advance())
			}
			return tsToken{kind: tsIdent, text: sb.String(), line: startLine, col: startCol, off: startOff}
		}

		l.advance()
		return tsToken{kind: tsOther, text: string(ch), line: startLine, col: startCol, off: startOff}
	}
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' || ch == '$'
}
func isIdentContinue(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9')
}

// ─── Parser ───────────────────────────────────────────────────

type tsParser struct {
	filePath string
	src      []byte
	lex      *tsLexer
	buf      []tsToken // lookahead buffer
}

func newTSParser(filePath string, src []byte) *tsParser {
	return &tsParser{filePath: filePath, src: src, lex: newTSLexer(src)}
}

func (p *tsParser) peek() tsToken {
	for {
		if len(p.buf) == 0 {
			tok := p.lex.next()
			if tok.kind == tsNewline {
				continue // skip newlines in lookahead
			}
			p.buf = append(p.buf, tok)
		}
		return p.buf[0]
	}
}

func (p *tsParser) consume() tsToken {
	_ = p.peek()
	tok := p.buf[0]
	p.buf = p.buf[1:]
	return tok
}

func (p *tsParser) expect(kind tsTokenKind) tsToken {
	tok := p.peek()
	if tok.kind == kind {
		return p.consume()
	}
	return tok // best-effort
}

func (p *tsParser) skipUntil(kinds ...tsTokenKind) {
	set := make(map[tsTokenKind]bool, len(kinds))
	for _, k := range kinds {
		set[k] = true
	}
	for {
		tok := p.peek()
		if tok.kind == tsEOF || set[tok.kind] {
			return
		}
		p.consume()
	}
}

// skipUntilStmtEnd advances tokens until the next semicolon, EOF, the
// closing brace of the enclosing block, or a newline at brace depth 0.
//
// Added in v18 (D1#8, D1#9). The plain skipUntil(tsSemicolon, tsEOF)
// used by parseTypeAlias, parseVarDecl and the class field-initialiser
// would run all the way to EOF for newline-terminated statements
// (TypeScript without semicolons), swallowing every subsequent
// declaration in the file or class body. A separate helper was needed
// because skipUntil goes through peek(), and peek() unconditionally
// skips tsNewline tokens to make lookahead robust against blank lines.
//
// Semantics: if there is already a token buffered by an earlier peek(),
// the current statement has effectively ended — that buffered token
// belongs to whatever comes next (and the newline that separates them
// has already been silently dropped by peek). Do not touch the buffer
// in that case; just return. Only when the buffer is empty do we drop
// down to the raw lexer to consume tokens up to a real terminator,
// honouring newlines at brace depth 0.
//
// Fixed in v18.1: the earlier version drained any buffered ident
// (e.g. the 'export' keyword of the next declaration) and then ran the
// raw loop, which caused TestTSFrontend_ParseTypeAlias to see only one
// alias instead of two.
func (p *tsParser) skipUntilStmtEnd() {
	if len(p.buf) > 0 {
		// Buffered: the statement is already over. Even if the buffered
		// token is a semicolon/rbrace we want to leave it for the caller
		// to inspect (matching the post-loop check on tsSemicolon).
		return
	}
	depth := 0
	for {
		tok := p.lex.next()
		if tok.kind == tsEOF {
			p.buf = append(p.buf, tok)
			return
		}
		if tok.kind == tsNewline && depth == 0 {
			// Newline at depth 0 is a statement terminator. Do not buffer
			// it (peek() would discard it on the next call anyway).
			return
		}
		if tok.kind == tsNewline {
			continue
		}
		if depth == 0 && (tok.kind == tsSemicolon || tok.kind == tsRBrace) {
			// Leave the terminator buffered for the caller to inspect.
			p.buf = append(p.buf, tok)
			return
		}
		if tok.kind == tsLBrace || tok.kind == tsLParen || tok.kind == tsLBracket {
			depth++
		} else if tok.kind == tsRBrace || tok.kind == tsRParen || tok.kind == tsRBracket {
			if depth > 0 {
				depth--
			}
		}
	}
}

// skipBraces skips everything from '{' to the matching '}'.
func (p *tsParser) skipBraces() string {
	if p.peek().kind != tsLBrace {
		return ""
	}
	start := p.peek().off
	depth := 0
	var sb strings.Builder
	for {
		tok := p.consume()
		sb.WriteString(tok.text)
		if tok.kind == tsLBrace {
			depth++
		} else if tok.kind == tsRBrace {
			depth--
			if depth == 0 {
				break
			}
		} else if tok.kind == tsEOF {
			break
		}
	}
	_ = start
	return sb.String()
}

// parseFuncBody is the replacement for skipBraces at function/method body
// sites. It does the same depth-balanced brace scan but also emits
// structural IR nodes (NodeBranch, NodeLoop, NodeReturn, NodeCall) as it
// encounters control-flow keywords and call expressions in the raw text.
//
// Returns:
//   - raw: the brace-delimited body text (same as skipBraces)
//   - structural: zero or more IR nodes describing control flow inside the body
//
// The caller assembles Children via bodyChildren(raw, structural).
func (p *tsParser) parseFuncBody() (raw string, structural []*Node) {
	if p.peek().kind != tsLBrace {
		return "", nil
	}

	// Phase 1: capture raw text + token stream.
	var sb strings.Builder
	depth := 0
	var bodyTokens []tsToken

	for {
		tok := p.consume()
		sb.WriteString(tok.text)
		bodyTokens = append(bodyTokens, tok)
		if tok.kind == tsLBrace {
			depth++
		} else if tok.kind == tsRBrace {
			depth--
			if depth == 0 {
				break
			}
		} else if tok.kind == tsEOF {
			break
		}
	}
	raw = sb.String()

	// Phase 2: structural scan over the captured tokens.
	// Recognised patterns (at any brace depth inside the body):
	//   if / switch / try  → NodeBranch
	//   for / while / do   → NodeLoop
	//   return / throw     → NodeReturn
	//   ident(             → NodeCall (simple call expression)
	for i, tok := range bodyTokens {
		if tok.kind != tsIdent {
			continue
		}
		switch tok.text {
		case "if", "switch", "try":
			structural = append(structural, &Node{
				Kind:     NodeBranch,
				Name:     tok.text,
				Language: LangTS,
				Span:     Span{Start: Pos{Line: tok.line, Column: tok.col}},
			})
		case "for", "while", "do":
			// v23.49: pull the condition text from the parentheses after the
			// keyword (for (…) / while (…)) so the CFG loop decision shows it.
			structural = append(structural, &Node{
				Kind:      NodeLoop,
				Name:      tok.text,
				Language:  LangTS,
				RawSource: tsLoopCond(bodyTokens, i),
				Span:      Span{Start: Pos{Line: tok.line, Column: tok.col}},
			})
		case "return", "throw":
			structural = append(structural, &Node{
				Kind:     NodeReturn,
				Name:     tok.text,
				Language: LangTS,
				Span:     Span{Start: Pos{Line: tok.line, Column: tok.col}},
			})
		default:
			// ident( pattern → call; skip control-structure keywords.
			if i+1 < len(bodyTokens) && bodyTokens[i+1].kind == tsLParen {
				switch tok.text {
				case "if", "for", "while", "switch", "catch":
					// not a call
				default:
					structural = append(structural, &Node{
						Kind:     NodeCall,
						Callee:   tok.text,
						Language: LangTS,
						Span:     Span{Start: Pos{Line: tok.line, Column: tok.col}},
					})
				}
			}
		}
	}
	return raw, structural
}

// bodyChildren assembles the standard Children slice for a function/method node.
// raw is the body source; structural are the IR nodes from parseFuncBody.
// The opaque NodeUnknown is always first so cfg_ts_py.extractStructuralNodes
// continues to find and unwrap it.
func bodyChildren(raw string, structural []*Node) []*Node {
	opaque := &Node{
		Kind:          NodeUnknown,
		UnknownReason: "body_not_parsed",
		RawSource:     raw,
	}
	return append([]*Node{opaque}, structural...)
}

// skipAngles skips <...> with nesting.
func (p *tsParser) skipAngles() {
	if p.peek().kind != tsLAngle {
		return
	}
	depth := 0
	for {
		tok := p.consume()
		if tok.kind == tsLAngle {
			depth++
		} else if tok.kind == tsRAngle {
			depth--
			if depth == 0 {
				return
			}
		} else if tok.kind == tsEOF || tok.kind == tsLBrace {
			p.buf = append([]tsToken{tok}, p.buf...)
			return
		}
	}
}

func (p *tsParser) makeSpan(line, col, off int) Span {
	return Span{Start: Pos{Line: line, Column: col, Offset: off}}
}

// ─── Top-Level ────────────────────────────────────────────────

func (p *tsParser) parseTopLevel(m *Module) {
	for {
		tok := p.peek()
		if tok.kind == tsEOF {
			break
		}

		// Collect modifiers
		exported := false
		abstract := false
		async := false
		declare := false

		// Handle decorator @decorator
		if tok.kind == tsAt {
			p.consume()
			p.skipUntil(tsNewline, tsIdent, tsLBrace, tsEOF)
			continue
		}

		if tok.kind == tsIdent {
			switch tok.text {
			case "export":
				exported = true
				p.consume()
				tok = p.peek()
				if tok.kind == tsIdent && tok.text == "default" {
					p.consume()
					// export default — skip to semicolon/newline
					p.skipUntil(tsSemicolon, tsEOF)
					if p.peek().kind == tsSemicolon {
						p.consume()
					}
					continue
				}
				if tok.kind == tsIdent && tok.text == "type" {
					// might be "export type { Foo }" or "export type Foo = ..."
					// peek further
					next := p.peek()
					if next.kind == tsLBrace {
						// re-export block: export type { X, Y }
						p.skipBraces()
						p.skipUntil(tsSemicolon, tsEOF)
						p.consume()
						continue
					}
					// fall through to type alias
					tok = p.peek()
				}
				if tok.kind == tsLBrace {
					// export { X, Y as Z } from '...'
					n := p.parseExportBlock(exported, tok.line, tok.col, tok.off)
					m.Nodes = append(m.Nodes, n)
					continue
				}
				if tok.kind == tsIdent && tok.text == "abstract" {
					abstract = true
					p.consume()
					tok = p.peek()
				}
				if tok.kind == tsIdent && tok.text == "async" {
					async = true
					p.consume()
					tok = p.peek()
				}
				if tok.kind == tsIdent && tok.text == "declare" {
					declare = true
					p.consume()
					tok = p.peek()
				}
			case "declare":
				declare = true
				p.consume()
				tok = p.peek()
			case "abstract":
				abstract = true
				p.consume()
				tok = p.peek()
			case "async":
				async = true
				p.consume()
				tok = p.peek()
			}

			_ = declare

			switch tok.text {
			case "import":
				n := p.parseImport(tok.line, tok.col, tok.off)
				m.Nodes = append(m.Nodes, n)
			case "interface":
				n := p.parseInterface(exported, tok.line, tok.col, tok.off)
				m.Nodes = append(m.Nodes, n)
			case "type":
				n := p.parseTypeAlias(exported, tok.line, tok.col, tok.off)
				m.Nodes = append(m.Nodes, n)
			case "class":
				n := p.parseClass(exported, abstract, tok.line, tok.col, tok.off)
				m.Nodes = append(m.Nodes, n)
			case "function":
				n := p.parseFunction(exported, async, tok.line, tok.col, tok.off)
				m.Nodes = append(m.Nodes, n)
			case "enum":
				n := p.parseEnum(exported, tok.line, tok.col, tok.off)
				m.Nodes = append(m.Nodes, n)
			case "const", "let", "var":
				nodes := p.parseVarDecl(exported, tok.line, tok.col, tok.off)
				m.Nodes = append(m.Nodes, nodes...)
			default:
				// Unknown statement — mark and skip to next safe point
				m.Nodes = append(m.Nodes, &Node{
					Kind:          NodeUnknown,
					Language:      LangTS,
					Span:          p.makeSpan(tok.line, tok.col, tok.off),
					RawSource:     tok.text,
					UnknownReason: "unrecognized_top_level_statement",
				})
				p.consume()
				p.skipUntilStmtEnd() // v18 (D1#8): was skipUntil(.., tsNewline, ..) — dead
				if p.peek().kind == tsSemicolon {
					p.consume()
				}
			}
		} else {
			p.consume() // skip stray tokens
		}
	}
}

// ─── Import ───────────────────────────────────────────────────

func (p *tsParser) parseImport(line, col, off int) *Node {
	p.consume() // consume 'import'
	tok := p.peek()

	n := &Node{
		Kind:     NodeImport,
		Language: LangTS,
		Span:     p.makeSpan(line, col, off),
	}

	// import type { ... } — treat identically to import { ... }
	if tok.kind == tsIdent && tok.text == "type" {
		p.consume()
		tok = p.peek()
	}

	// v18 (D1#7): the lexer emits '*' as tsOther (it's not in the punct
	// map and not an ident-start), so the original code's check
	//     case tsIdent: if tok.text == "*" { ... }
	// was unreachable for namespace imports. Handle the star branch up
	// front before dispatching on token kind.
	if tok.text == "*" {
		n.ImportAll = true
		p.consume()
		if p.peek().text == "as" {
			p.consume()
			n.ImportAlias = p.consume().text
		}
		if p.peek().text == "from" {
			p.consume()
		}
		if p.peek().kind == tsString {
			n.ImportPath = stripQuotes(p.consume().text)
		}
		n.ID = "import:" + n.ImportPath
		if n.Name == "" {
			n.Name = lastSegment(n.ImportPath)
		}
		p.skipUntil(tsSemicolon, tsEOF)
		if p.peek().kind == tsSemicolon {
			p.consume()
		}
		return n
	}

	switch tok.kind {
	case tsString:
		// import 'side-effect-module'
		n.ImportPath = stripQuotes(p.consume().text)
		n.ID = "import:" + n.ImportPath
	case tsLBrace:
		// import { X, Y as Z } from '...'
		n.ImportNames = p.parseImportNames()
		p.skipUntil(tsIdent, tsEOF) // skip to 'from'
		if p.peek().text == "from" {
			p.consume()
		}
		if p.peek().kind == tsString {
			n.ImportPath = stripQuotes(p.consume().text)
		}
		n.ID = "import:" + n.ImportPath
		n.Name = lastSegment(n.ImportPath)
	case tsIdent:
		// import DefaultExport from '...'
		name := p.consume().text
		n.ImportAlias = name
		n.Name = name
		// may be followed by , { ... }
		if p.peek().kind == tsComma {
			p.consume()
			if p.peek().kind == tsLBrace {
				n.ImportNames = p.parseImportNames()
			}
		}
		if p.peek().text == "from" {
			p.consume()
		}
		if p.peek().kind == tsString {
			n.ImportPath = stripQuotes(p.consume().text)
		}
		n.ID = "import:" + n.ImportPath
		if n.Name == "" {
			n.Name = lastSegment(n.ImportPath)
		}
	}

	p.skipUntil(tsSemicolon, tsEOF)
	if p.peek().kind == tsSemicolon {
		p.consume()
	}
	return n
}

func (p *tsParser) parseImportNames() []string {
	p.consume() // consume '{'
	var names []string
	for {
		tok := p.peek()
		if tok.kind == tsRBrace || tok.kind == tsEOF {
			p.consume()
			break
		}
		if tok.kind == tsComma {
			p.consume()
			continue
		}
		if tok.kind == tsIdent {
			name := p.consume().text
			if p.peek().text == "as" {
				p.consume()
				alias := p.consume().text
				names = append(names, alias)
			} else {
				names = append(names, name)
			}
		} else {
			p.consume()
		}
	}
	return names
}

// ─── Export block ─────────────────────────────────────────────

func (p *tsParser) parseExportBlock(exported bool, line, col, off int) *Node {
	body := p.skipBraces()
	n := &Node{
		Kind:      NodeExport,
		Language:  LangTS,
		Span:      p.makeSpan(line, col, off),
		Exported:  exported,
		RawSource: body,
	}
	if p.peek().text == "from" {
		p.consume()
		if p.peek().kind == tsString {
			n.ImportPath = stripQuotes(p.consume().text)
		}
	}
	n.ID = "export:" + n.ImportPath
	p.skipUntil(tsSemicolon, tsEOF)
	if p.peek().kind == tsSemicolon {
		p.consume()
	}
	return n
}

// ─── Interface ────────────────────────────────────────────────

func (p *tsParser) parseInterface(exported bool, line, col, off int) *Node {
	p.consume() // consume 'interface'
	name := p.consume().text
	n := &Node{
		ID:       "interface:" + name,
		Kind:     NodeInterface,
		Name:     name,
		Language: LangTS,
		Span:     p.makeSpan(line, col, off),
		Exported: exported,
	}
	// Generic params
	if p.peek().kind == tsLAngle {
		n.TypeParams = p.parseTypeParams()
	}
	// extends
	if p.peek().kind == tsIdent && p.peek().text == "extends" {
		p.consume()
		for {
			tok := p.peek()
			if tok.kind == tsLBrace || tok.kind == tsEOF {
				break
			}
			if tok.kind == tsIdent {
				n.Implements = append(n.Implements, p.consume().text)
			} else if tok.kind == tsComma {
				p.consume()
			} else {
				p.consume()
			}
		}
	}
	// Body
	if p.peek().kind == tsLBrace {
		n.Fields = p.parseInterfaceBody()
	}
	return n
}

func (p *tsParser) parseInterfaceBody() []Field {
	p.consume() // '{'
	var fields []Field
	for {
		tok := p.peek()
		if tok.kind == tsRBrace || tok.kind == tsEOF {
			p.consume()
			break
		}
		if tok.kind == tsSemicolon || tok.kind == tsComma {
			p.consume()
			continue
		}
		// optional readonly modifier
		if tok.kind == tsIdent && tok.text == "readonly" {
			p.consume()
			tok = p.peek()
		}
		// method or property
		if tok.kind == tsIdent || tok.kind == tsString {
			name := p.consume().text
			optional := false
			if p.peek().kind == tsQuestion {
				optional = true
				p.consume()
			}
			var typ TypeRef
			if p.peek().kind == tsColon {
				p.consume()
				typ = p.parseTypeExpr()
			} else if p.peek().kind == tsLParen {
				// method signature — skip params and return type
				p.skipParens()
				if p.peek().kind == tsColon {
					p.consume()
					typ = p.parseTypeExpr()
				}
				typ.IsFunc = true
				if typ.Name == "" {
					typ.Name = "func"
				}
			}
			fields = append(fields, Field{
				Name:     name,
				Type:     typ,
				Optional: optional,
				Exported: isExported(name),
			})
		} else {
			p.consume()
		}
	}
	return fields
}

// ─── Type alias ───────────────────────────────────────────────

func (p *tsParser) parseTypeAlias(exported bool, line, col, off int) *Node {
	p.consume() // consume 'type'
	name := p.consume().text
	n := &Node{
		ID:       "type:" + name,
		Kind:     NodeTypeAlias,
		Name:     name,
		Language: LangTS,
		Span:     p.makeSpan(line, col, off),
		Exported: exported,
	}
	if p.peek().kind == tsLAngle {
		n.TypeParams = p.parseTypeParams()
	}
	if p.peek().kind == tsEquals {
		p.consume()
		n.AliasTarget = p.parseTypeExpr()
	}
	p.skipUntilStmtEnd() // v18 (D1#8): newline terminates a TS statement without semicolon
	if p.peek().kind == tsSemicolon {
		p.consume()
	}
	return n
}

// ─── Class ────────────────────────────────────────────────────

func (p *tsParser) parseClass(exported, abstract bool, line, col, off int) *Node {
	p.consume() // consume 'class'
	name := ""
	if p.peek().kind == tsIdent {
		name = p.consume().text
	}
	n := &Node{
		ID:       "class:" + name,
		Kind:     NodeClass,
		Name:     name,
		Language: LangTS,
		Span:     p.makeSpan(line, col, off),
		Exported: exported,
		Abstract: abstract,
	}
	if p.peek().kind == tsLAngle {
		n.TypeParams = p.parseTypeParams()
	}
	for {
		tok := p.peek()
		if tok.kind == tsLBrace || tok.kind == tsEOF {
			break
		}
		if tok.kind == tsIdent {
			switch tok.text {
			case "extends":
				p.consume()
				if p.peek().kind == tsIdent {
					n.Extends = p.consume().text
					if p.peek().kind == tsLAngle {
						p.skipAngles()
					}
				}
			case "implements":
				p.consume()
				for {
					t2 := p.peek()
					if t2.kind == tsLBrace || t2.kind == tsEOF {
						break
					}
					if t2.kind == tsIdent {
						n.Implements = append(n.Implements, p.consume().text)
						if p.peek().kind == tsLAngle {
							p.skipAngles()
						}
					} else if t2.kind == tsComma {
						p.consume()
					} else {
						break
					}
				}
			default:
				p.consume()
			}
		} else {
			p.consume()
		}
	}
	if p.peek().kind == tsLBrace {
		n.Fields, n.Children = p.parseClassBody(name)
	}
	return n
}

func (p *tsParser) parseClassBody(className string) ([]Field, []*Node) {
	p.consume() // '{'
	var fields []Field
	var methods []*Node
	for {
		tok := p.peek()
		if tok.kind == tsRBrace || tok.kind == tsEOF {
			p.consume()
			break
		}
		if tok.kind == tsSemicolon {
			p.consume()
			continue
		}
		// modifiers
		static, async, abstract2, priv := false, false, false, false
		for tok.kind == tsIdent {
			switch tok.text {
			case "static":
				static = true; p.consume(); tok = p.peek()
			case "async":
				async = true; p.consume(); tok = p.peek()
			case "abstract":
				abstract2 = true; p.consume(); tok = p.peek()
			case "private", "protected":
				priv = true; p.consume(); tok = p.peek()
			case "public", "readonly", "override", "declare":
				p.consume(); tok = p.peek()
			default:
				goto memberName
			}
		}
	memberName:
		if tok.kind == tsIdent || tok.kind == tsString {
			name := p.consume().text

			// Constructor
			if name == "constructor" {
				paramStr := p.readParamList()
				bodyRaw2, bodyStructural2 := p.parseFuncBody()
				methods = append(methods, &Node{
					ID:       "method:" + className + ".constructor",
					Kind:     NodeMethod,
					Name:     "constructor",
					Language: LangTS,
					Span:     p.makeSpan(tok.line, tok.col, tok.off),
					Static:   static,
					Async:    async,
					Abstract: abstract2,
					Children: bodyChildren(paramStr+bodyRaw2, bodyStructural2),
				})
				continue
			}

			// Method or property?
			if p.peek().kind == tsLParen || p.peek().kind == tsLAngle {
				if p.peek().kind == tsLAngle {
					p.skipAngles()
				}
				params := p.parseParamList()
				var returns []TypeRef
				if p.peek().kind == tsColon {
					p.consume()
					returns = []TypeRef{p.parseTypeExpr()}
				}
				var bodyRaw string
				var bodyStructural []*Node
				if p.peek().kind == tsLBrace {
					bodyRaw, bodyStructural = p.parseFuncBody()
				} else {
					p.skipUntil(tsSemicolon, tsEOF)
					if p.peek().kind == tsSemicolon {
						p.consume()
					}
				}
				methods = append(methods, &Node{
					ID:       "method:" + className + "." + name,
					Kind:     NodeMethod,
					Name:     name,
					Language: LangTS,
					Span:     p.makeSpan(tok.line, tok.col, tok.off),
					Static:   static,
					Async:    async,
					Abstract: abstract2,
					Exported: isExported(name) && !priv,
					Params:   params,
					Returns:  returns,
					Children: bodyChildren(bodyRaw, bodyStructural),
				})
			} else {
				// Property declaration
				optional := false
				if p.peek().kind == tsQuestion {
					optional = true; p.consume()
				}
				if p.peek().kind == tsOther && p.peek().text == "!" {
					p.consume()
				}
				var typ TypeRef
				if p.peek().kind == tsColon {
					p.consume()
					typ = p.parseTypeExpr()
				}
				if p.peek().kind == tsEquals {
					p.consume()
					// v18 (D1#8): the previous skipUntil(.., tsNewline, ..) call
					// was dead because peek() swallows tsNewline before it can
					// match — that ran to EOF and consumed every subsequent
					// method in the class body, so tests expecting >= 2 method
					// children always saw 0. skipUntilStmtEnd reads raw tokens
					// from the lexer and honours newlines at depth 0.
					p.skipUntilStmtEnd()
				}
				if p.peek().kind == tsSemicolon {
					p.consume()
				}
				fields = append(fields, Field{
					Name:     name,
					Type:     typ,
					Optional: optional,
					Exported: isExported(name) && !priv,
				})
			}
		} else {
			p.skipBraces()
			p.consume()
		}
	}
	return fields, methods
}

// ─── Function ─────────────────────────────────────────────────

func (p *tsParser) parseFunction(exported, async bool, line, col, off int) *Node {
	p.consume() // consume 'function'
	name := ""
	if p.peek().kind == tsIdent {
		name = p.consume().text
	}
	n := &Node{
		ID:       "func:" + name,
		Kind:     NodeFunction,
		Name:     name,
		Language: LangTS,
		Span:     p.makeSpan(line, col, off),
		Exported: exported,
		Async:    async,
	}
	if p.peek().kind == tsLAngle {
		n.TypeParams = p.parseTypeParams()
	}
	n.Params = p.parseParamList()
	if p.peek().kind == tsColon {
		p.consume()
		n.Returns = []TypeRef{p.parseTypeExpr()}
	}
	var bodyRaw string
	var bodyStructural []*Node
	if p.peek().kind == tsLBrace {
		bodyRaw, bodyStructural = p.parseFuncBody()
	}
	n.Children = bodyChildren(bodyRaw, bodyStructural)
	return n
}

// ─── Enum ─────────────────────────────────────────────────────

func (p *tsParser) parseEnum(exported bool, line, col, off int) *Node {
	p.consume() // consume 'enum'
	name := p.consume().text
	n := &Node{
		ID:       "enum:" + name,
		Kind:     NodeEnum,
		Name:     name,
		Language: LangTS,
		Span:     p.makeSpan(line, col, off),
		Exported: exported,
	}
	if p.peek().kind != tsLBrace {
		return n
	}
	p.consume() // '{'
	for {
		tok := p.peek()
		if tok.kind == tsRBrace || tok.kind == tsEOF {
			p.consume()
			break
		}
		if tok.kind == tsComma || tok.kind == tsSemicolon {
			p.consume()
			continue
		}
		if tok.kind == tsIdent || tok.kind == tsString {
			memberName := p.consume().text
			val := ""
			if p.peek().kind == tsEquals {
				p.consume()
				valTok := p.consume()
				val = valTok.text
			}
			n.EnumMembers = append(n.EnumMembers, EnumMember{Name: memberName, Value: val})
		} else {
			p.consume()
		}
	}
	return n
}

// ─── Var/Const/Let ────────────────────────────────────────────

func (p *tsParser) parseVarDecl(exported bool, line, col, off int) []*Node {
	_ = p.consume() // consume 'const'/'let'/'var'
	var nodes []*Node
	for {
		tok := p.peek()
		if tok.kind == tsEOF || tok.kind == tsSemicolon {
			if tok.kind == tsSemicolon {
				p.consume()
			}
			break
		}
		if tok.kind == tsLBrace || tok.kind == tsLBracket {
			// destructuring — mark as unknown
			nodes = append(nodes, &Node{
				Kind:          NodeUnknown,
				Language:      LangTS,
				Span:          p.makeSpan(tok.line, tok.col, tok.off),
				UnknownReason: "destructuring_declaration",
			})
			p.skipBraces()
			p.skipUntil(tsSemicolon, tsEOF)
			if p.peek().kind == tsSemicolon {
				p.consume()
			}
			break
		}
		if tok.kind != tsIdent {
			p.consume()
			continue
		}
		name := p.consume().text
		optional := false
		if p.peek().kind == tsQuestion {
			optional = true; p.consume()
		}
		var typ TypeRef
		if p.peek().kind == tsColon {
			p.consume()
			typ = p.parseTypeExpr()
		}
		val := ""
		if p.peek().kind == tsEquals {
			p.consume()
			// v18 (D1#9): the previous loop only terminated at comma,
			// semicolon, EOF, or unmatched ')]}'. For TypeScript without
			// trailing semicolons, two adjacent `export const` declarations
			// merged into one — the second declaration was consumed as
			// part of the first value literal, so we always saw 1 of 2.
			// Drain p.buf first, then read raw tokens straight from the
			// lexer so we can honour newlines at depth 0 without changing
			// peek()'s newline-skipping behaviour everywhere else.
			var sb strings.Builder
			depth := 0
			done := false
			// 1) Drain any tokens already buffered by peek().
			for !done && len(p.buf) > 0 {
				t2 := p.buf[0]
				if t2.kind == tsEOF {
					done = true
					break
				}
				if (t2.kind == tsComma || t2.kind == tsSemicolon) && depth == 0 {
					done = true
					break
				}
				if t2.kind == tsLBrace || t2.kind == tsLParen || t2.kind == tsLBracket {
					depth++
				} else if t2.kind == tsRBrace || t2.kind == tsRParen || t2.kind == tsRBracket {
					if depth == 0 {
						done = true
						break
					}
					depth--
				}
				sb.WriteString(t2.text)
				p.buf = p.buf[1:]
			}
			// 2) Pull raw tokens from the lexer, newline-aware.
			for !done {
				t2 := p.lex.next()
				if t2.kind == tsEOF {
					// Push back so the outer loop sees it.
					p.buf = append(p.buf, t2)
					break
				}
				if t2.kind == tsNewline && depth == 0 {
					// Newline at depth 0 ends the statement; do not
					// buffer (peek() would discard it anyway).
					break
				}
				if t2.kind == tsNewline {
					continue
				}
				if (t2.kind == tsComma || t2.kind == tsSemicolon) && depth == 0 {
					// Leave the terminator for the caller.
					p.buf = append(p.buf, t2)
					break
				}
				if t2.kind == tsLBrace || t2.kind == tsLParen || t2.kind == tsLBracket {
					depth++
				} else if t2.kind == tsRBrace || t2.kind == tsRParen || t2.kind == tsRBracket {
					if depth == 0 {
						p.buf = append(p.buf, t2)
						break
					}
					depth--
				}
				sb.WriteString(t2.text)
			}
			val = sb.String()
		}
		_ = optional
		nodes = append(nodes, &Node{
			ID:           "var:" + name,
			Kind:         NodeVariable,
			Name:         name,
			Language:     LangTS,
			Span:         p.makeSpan(tok.line, tok.col, tok.off),
			Exported:     exported,
			TypeHint:     typ,
			ValueLiteral: val,
		})
		if p.peek().kind == tsComma {
			p.consume()
			continue
		}
		break
	}
	if p.peek().kind == tsSemicolon {
		p.consume()
	}
	return nodes
}

// ─── Type Expression Parser ───────────────────────────────────

// parseTypeExpr reads a type expression until a natural stopping point.
// Returns TypeRef.Name = "?" (UnknownType) for complex union/intersection
// types that we don't fully model.
func (p *tsParser) parseTypeExpr() TypeRef {
	parts := p.parseUnionType()
	if len(parts) == 1 {
		return parts[0]
	}
	// Union or intersection — record the names joined for human readability
	names := make([]string, len(parts))
	for i, pt := range parts {
		names[i] = pt.String()
	}
	return TypeRef{Name: strings.Join(names, " | ")}
}

func (p *tsParser) parseUnionType() []TypeRef {
	var parts []TypeRef
	parts = append(parts, p.parseSingleType())
	for p.peek().kind == tsPipe || p.peek().kind == tsAmpersand {
		p.consume()
		parts = append(parts, p.parseSingleType())
	}
	return parts
}

func (p *tsParser) parseSingleType() TypeRef {
	tok := p.peek()

	// Array type shorthand: type[]
	// Parenthesized: (type)
	// Function type: (params) => returnType
	// Tuple: [type, type]

	if tok.kind == tsLParen {
		p.skipParens()
		if p.peek().kind == tsArrow {
			p.consume()
			p.parseTypeExpr()
		}
		return TypeRef{Name: "func", IsFunc: true}
	}

	if tok.kind == tsLBracket {
		p.skipBrackets()
		return TypeRef{Name: "tuple"}
	}

	if tok.kind == tsLBrace {
		p.skipBraces()
		return TypeRef{Name: "object"}
	}

	if tok.kind == tsIdent {
		name := p.consume().text
		pkg := ""
		if p.peek().kind == tsDot {
			p.consume()
			member := p.consume().text
			pkg = name
			name = member
		}
		ref := TypeRef{Name: name, Package: pkg}
		if p.peek().kind == tsLAngle {
			p.skipAngles()
		}
		for p.peek().kind == tsLBracket && p.peekAt(1).kind == tsRBracket {
			p.consume(); p.consume()
			ref.IsSlice = true
		}
		return ref
	}

	if tok.kind == tsString {
		p.consume()
		return TypeRef{Name: tok.text} // string literal type
	}

	// null, undefined, etc.
	if tok.kind == tsIdent {
		return TypeRef{Name: p.consume().text}
	}

	return UnknownType
}

func (p *tsParser) peekAt(n int) tsToken {
	for len(p.buf) <= n {
		tok := p.lex.next()
		if tok.kind == tsNewline {
			continue
		}
		p.buf = append(p.buf, tok)
	}
	return p.buf[n]
}

func (p *tsParser) parseTypeParams() []string {
	p.consume() // '<'
	var names []string
	depth := 1
	for depth > 0 {
		tok := p.consume()
		if tok.kind == tsEOF {
			break
		}
		if tok.kind == tsLAngle {
			depth++
		} else if tok.kind == tsRAngle {
			depth--
			if depth == 0 {
				break
			}
		} else if tok.kind == tsIdent && depth == 1 {
			names = append(names, tok.text)
		}
	}
	return names
}

func (p *tsParser) parseParamList() []Parameter {
	p.skipParens()
	return nil // params recorded as unknown — body_not_parsed covers them
}

func (p *tsParser) readParamList() string {
	if p.peek().kind != tsLParen {
		return ""
	}
	var sb strings.Builder
	depth := 0
	for {
		tok := p.consume()
		sb.WriteString(tok.text)
		if tok.kind == tsLParen {
			depth++
		} else if tok.kind == tsRParen {
			depth--
			if depth == 0 {
				break
			}
		} else if tok.kind == tsEOF {
			break
		}
	}
	return sb.String()
}

func (p *tsParser) skipParens() {
	if p.peek().kind != tsLParen {
		return
	}
	depth := 0
	for {
		tok := p.consume()
		if tok.kind == tsLParen {
			depth++
		} else if tok.kind == tsRParen {
			depth--
			if depth == 0 {
				return
			}
		} else if tok.kind == tsEOF {
			return
		}
	}
}

func (p *tsParser) skipBrackets() {
	if p.peek().kind != tsLBracket {
		return
	}
	depth := 0
	for {
		tok := p.consume()
		if tok.kind == tsLBracket {
			depth++
		} else if tok.kind == tsRBracket {
			depth--
			if depth == 0 {
				return
			}
		} else if tok.kind == tsEOF {
			return
		}
	}
}

// ─── Helpers ──────────────────────────────────────────────────

func stripQuotes(s string) string {
	s = strings.TrimPrefix(s, "'")
	s = strings.TrimPrefix(s, "\"")
	s = strings.TrimPrefix(s, "`")
	s = strings.TrimSuffix(s, "'")
	s = strings.TrimSuffix(s, "\"")
	s = strings.TrimSuffix(s, "`")
	return s
}

func lastSegment(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func tsModuleName(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".tsx")
	base = strings.TrimSuffix(base, ".ts")
	return base
}
