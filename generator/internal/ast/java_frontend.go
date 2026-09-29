// java_frontend.go — v23.12 (Phase 4): native, pure-Go, offline Java 17
// understanding frontend. Hand-rolled lexer + recursive-descent-ish structural
// parser (no external deps, no Tree-sitter), mirroring cobol_frontend.go's
// shape. Resolves package/imports/classes/interfaces/enums/records/methods and
// walks method bodies into NodeBranch/NodeLoop/NodeReturn/NodeCall/NodeTry so
// the shared CFG builder (flow.Build non-Go path) and the symbol index
// (extractCallEdges over NodeCall.Callee) work on Java exactly as they do for
// COBOL/TS/Py. Hard cases (annotations, generics with wildcards, lambdas,
// method refs, inner/anonymous classes) are pushed to NodeUnknown first — the
// IR's explicit-ignorance channel — rather than guessed at.
package ast

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// LangJava is the canonical language identifier for Java source.
// Declared here (package-level const) to mirror LangCOBOL in cobol_frontend.go;
// it is intentionally NOT in multi.go's const block.
const LangJava Language = "java"

// JavaFrontend implements Frontend for Java 17 source files.
// Pure Go; no runtime dependency, so Available() is always true.
type JavaFrontend struct{}

func (j *JavaFrontend) Language() Language   { return LangJava }
func (j *JavaFrontend) Extensions() []string { return []string{".java"} }
func (j *JavaFrontend) Available() bool      { return true } // pure Go

// Emit returns source unchanged — Java reformatting is out of scope, and the
// lossless contract is satisfied by returning the original bytes (like COBOL).
func (j *JavaFrontend) Emit(module *Module) ([]byte, error) {
	return module.RawSource, nil
}

// ─────────────────────────────────────────────────────────────
// Lexer
//
// Java's grammar is brace-delimited, so a token stream that correctly skips
// comments and string/char/text-block literals (so braces inside them don't
// throw off depth tracking) gets us most of the way. We DON'T build a full
// Java AST — we identify declaration headers and statement keywords, and track
// brace depth to delimit bodies. Anything we can't classify becomes NodeUnknown.
// ─────────────────────────────────────────────────────────────

type jTokKind int

const (
	jtIdent  jTokKind = iota // identifiers + keywords
	jtPunct                  // operators / punctuation, including { } ( ) ; < > @ ::
	jtString                 // "..." or text block """..."""
	jtChar                   // '.'
	jtNumber
)

type jToken struct {
	kind jTokKind
	text string
	line int // 1-based line where the token starts
}

// lexJava converts source bytes into a token stream. Comments are dropped.
// String/char/text-block literals are emitted as single tokens with their
// delimiters retained so callers can tell them apart from identifiers but the
// inner text never participates in brace/paren counting.
func lexJava(src []byte) []jToken {
	s := string(src)
	var toks []jToken
	line := 1
	i := 0
	n := len(s)

	emit := func(k jTokKind, text string, ln int) {
		toks = append(toks, jToken{kind: k, text: text, line: ln})
	}

	for i < n {
		c := s[i]

		// Newline tracking.
		if c == '\n' {
			line++
			i++
			continue
		}
		// Other whitespace.
		if c == ' ' || c == '\t' || c == '\r' || c == '\f' {
			i++
			continue
		}
		// Line comment.
		if c == '/' && i+1 < n && s[i+1] == '/' {
			for i < n && s[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment.
		if c == '/' && i+1 < n && s[i+1] == '*' {
			i += 2
			for i+1 < n && !(s[i] == '*' && s[i+1] == '/') {
				if s[i] == '\n' {
					line++
				}
				i++
			}
			i += 2 // consume */
			continue
		}
		// Text block """ ... """ (Java 13+/17). Must be checked before plain string.
		if c == '"' && i+2 < n && s[i+1] == '"' && s[i+2] == '"' {
			startLine := line
			i += 3
			for i+2 < n && !(s[i] == '"' && s[i+1] == '"' && s[i+2] == '"') {
				if s[i] == '\n' {
					line++
				}
				i++
			}
			i += 3 // consume closing """
			emit(jtString, `"""txt"""`, startLine)
			continue
		}
		// String literal.
		if c == '"' {
			startLine := line
			i++
			for i < n && s[i] != '"' {
				if s[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if s[i] == '\n' {
					line++ // tolerate (invalid Java, but don't desync)
				}
				i++
			}
			i++ // consume closing "
			emit(jtString, `"str"`, startLine)
			continue
		}
		// Char literal.
		if c == '\'' {
			startLine := line
			i++
			for i < n && s[i] != '\'' {
				if s[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				i++
			}
			i++ // consume closing '
			emit(jtChar, `'c'`, startLine)
			continue
		}
		// Identifier / keyword (Java identifiers allow letters, digits, _, $).
		if isJavaIdentStart(c) {
			start := i
			for i < n && isJavaIdentPart(s[i]) {
				i++
			}
			emit(jtIdent, s[start:i], line)
			continue
		}
		// Number (rough; we don't need exact numeric grammar).
		if c >= '0' && c <= '9' {
			start := i
			for i < n && isJavaNumberPart(s[i]) {
				i++
			}
			emit(jtNumber, s[start:i], line)
			continue
		}
		// Method reference :: as a single token.
		if c == ':' && i+1 < n && s[i+1] == ':' {
			emit(jtPunct, "::", line)
			i += 2
			continue
		}
		// Arrow -> as a single token (lambda / switch arrow).
		if c == '-' && i+1 < n && s[i+1] == '>' {
			emit(jtPunct, "->", line)
			i += 2
			continue
		}
		// Everything else: single-char punctuation/operator.
		emit(jtPunct, string(c), line)
		i++
	}
	return toks
}

func isJavaIdentStart(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isJavaIdentPart(c byte) bool {
	return isJavaIdentStart(c) || (c >= '0' && c <= '9')
}
func isJavaNumberPart(c byte) bool {
	return (c >= '0' && c <= '9') || c == '.' || c == '_' ||
		c == 'x' || c == 'X' || c == 'b' || c == 'B' ||
		c == 'e' || c == 'E' || c == 'l' || c == 'L' ||
		c == 'f' || c == 'F' || c == 'd' || c == 'D' ||
		(c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// ─────────────────────────────────────────────────────────────
// Parser
//
// Strategy: a flat scan over the token stream. We track brace depth to know
// whether we're at type-member level or inside a method body. We recognise
// declaration headers (class/interface/enum/record, methods, fields) by their
// shape, and inside method bodies we recognise statement keywords (if/for/
// while/do/switch/return/try) and method-call expressions. Anything that
// doesn't fit a recognised shape becomes a NodeUnknown carrying the raw text —
// the IR's explicit-ignorance channel — so nothing is silently dropped.
// ─────────────────────────────────────────────────────────────

type javaParser struct {
	toks     []jToken
	pos      int
	filePath string
	mod      *Module
}

// Parse parses one Java source file into a Module.
// Per the Frontend contract, only an unreadable/unrecoverable condition returns
// a non-nil error; recoverable oddities become NodeUnknown + a recoverable
// ParseError so the human-review layer can surface them.
func (j *JavaFrontend) Parse(filePath string, source []byte) (*Module, error) {
	sum := sha256.Sum256(source)
	m := &Module{
		Language:  LangJava,
		FilePath:  filePath,
		Checksum:  fmt.Sprintf("%x", sum),
		RawSource: source,
	}
	p := &javaParser{
		toks:     lexJava(source),
		filePath: filePath,
		mod:      m,
	}
	p.parseFile()
	if m.PackageName == "" {
		m.PackageName = javaBaseName(filePath)
	}
	return m, nil
}

// ─── token helpers ───────────────────────────────────────────

func (p *javaParser) eof() bool { return p.pos >= len(p.toks) }
func (p *javaParser) cur() jToken {
	if p.eof() {
		return jToken{}
	}
	return p.toks[p.pos]
}
func (p *javaParser) peek(o int) jToken {
	k := p.pos + o
	if k < 0 || k >= len(p.toks) {
		return jToken{}
	}
	return p.toks[k]
}
func (p *javaParser) advance() jToken {
	t := p.cur()
	p.pos++
	return t
}
func (p *javaParser) isPunct(s string) bool {
	t := p.cur()
	return t.kind == jtPunct && t.text == s
}
func (p *javaParser) isKeyword(s string) bool {
	t := p.cur()
	return t.kind == jtIdent && t.text == s
}

// skipBalanced consumes from an opening delimiter through its matching close,
// assuming the opener has NOT yet been consumed. Used to skip annotation
// argument lists, generic argument lists, and parameter lists wholesale.
func (p *javaParser) skipBalanced(open, close string) {
	if !p.isPunct(open) {
		return
	}
	depth := 0
	for !p.eof() {
		if p.isPunct(open) {
			depth++
		} else if p.isPunct(close) {
			depth--
			if depth == 0 {
				p.advance()
				return
			}
		}
		p.advance()
	}
}

// ─── modifiers + annotations ─────────────────────────────────

var javaModifiers = map[string]bool{
	"public": true, "private": true, "protected": true, "static": true,
	"final": true, "abstract": true, "synchronized": true, "native": true,
	"transient": true, "volatile": true, "strictfp": true, "default": true,
	"sealed": true, "non": true, // "non-sealed" lexes oddly; tolerated
}

// collectLeadingMods reads any run of annotations and modifier keywords that
// precede a declaration. Annotations are recorded by name so Spring stereotypes
// (@RestController, @Autowired, @RequestMapping, …) are captured into the
// declaration's DocComment for the human-review layer, but their argument lists
// are skipped (pushed past, not interpreted — the NodeUnknown-first discipline).
type javaMods struct {
	names       map[string]bool
	annotations []string
}

func (p *javaParser) collectLeadingMods() javaMods {
	mods := javaMods{names: map[string]bool{}}
	for !p.eof() {
		// Annotation: @Name [ ( ... ) ]
		if p.isPunct("@") {
			p.advance()
			if p.cur().kind == jtIdent {
				name := p.advance().text
				// dotted annotation name e.g. @org.junit.Test
				for p.isPunct(".") && p.peek(1).kind == jtIdent {
					p.advance()
					name += "." + p.advance().text
				}
				mods.annotations = append(mods.annotations, "@"+name)
				if p.isPunct("(") {
					p.skipBalanced("(", ")")
				}
			}
			continue
		}
		// Modifier keyword.
		if p.cur().kind == jtIdent && javaModifiers[p.cur().text] {
			mods.names[p.advance().text] = true
			continue
		}
		break
	}
	return mods
}

func (m javaMods) annoDoc() string {
	if len(m.annotations) == 0 {
		return ""
	}
	return strings.Join(m.annotations, " ")
}

// ─── file-level parse ────────────────────────────────────────

func (p *javaParser) parseFile() {
	for !p.eof() {
		switch {
		case p.isKeyword("package"):
			p.parsePackage()
		case p.isKeyword("import"):
			p.parseImport()
		case p.isPunct(";"):
			p.advance() // stray semicolon
		default:
			// A leading run of annotations/modifiers then a type decl.
			mods := p.collectLeadingMods()
			if p.atTypeKeyword() {
				p.parseTypeDecl(mods, nil)
			} else if p.eof() {
				return
			} else {
				// Unrecognised top-level construct — record and resync to the
				// next ; or } so we never spin.
				p.recordUnknownStmt(nil, "unrecognised top-level construct")
			}
		}
	}
}

func (p *javaParser) parsePackage() {
	startLine := p.cur().line
	p.advance() // 'package'
	var parts []string
	for !p.eof() && !p.isPunct(";") {
		if p.cur().kind == jtIdent {
			parts = append(parts, p.cur().text)
		}
		p.advance()
	}
	if p.isPunct(";") {
		p.advance()
	}
	p.mod.PackageName = strings.Join(parts, ".")
	p.mod.Nodes = append(p.mod.Nodes, &Node{
		ID:       "module:" + p.mod.PackageName,
		Kind:     NodeModule,
		Name:     p.mod.PackageName,
		Language: LangJava,
		Span:     Span{Start: Pos{Line: startLine}},
	})
}

func (p *javaParser) parseImport() {
	startLine := p.cur().line
	p.advance() // 'import'
	static := false
	if p.isKeyword("static") {
		static = true
		p.advance()
	}
	var parts []string
	wildcard := false
	for !p.eof() && !p.isPunct(";") {
		if p.cur().kind == jtIdent {
			parts = append(parts, p.cur().text)
		} else if p.isPunct("*") {
			wildcard = true
		}
		p.advance()
	}
	if p.isPunct(";") {
		p.advance()
	}
	path := strings.Join(parts, ".")
	reason := ""
	if static {
		reason = "static import"
	}
	p.mod.Nodes = append(p.mod.Nodes, &Node{
		ID:            "import:" + path,
		Kind:          NodeImport,
		Name:          path,
		Language:      LangJava,
		Span:          Span{Start: Pos{Line: startLine}},
		ImportPath:    path,
		ImportAll:     wildcard,
		UnknownReason: reason,
	})
}

// ─── type declarations ───────────────────────────────────────

func (p *javaParser) atTypeKeyword() bool {
	switch {
	case p.isKeyword("class"), p.isKeyword("interface"), p.isKeyword("enum"):
		return true
	case p.isKeyword("record"): // Java 16+/17 record
		return true
	case p.isKeyword("@"):
		return false
	}
	// "@interface" annotation-type declaration.
	if p.isPunct("@") && p.peek(1).kind == jtIdent && p.peek(1).text == "interface" {
		return true
	}
	return false
}

// parseTypeDecl parses class/interface/enum/record. parent is non-nil for
// nested types; we still attach nested types to the module's top-level Nodes
// (flattened) so the symbol index sees every type, but we mark inner types via
// DocComment so the human-review layer can tell. Bodies are parsed for members.
func (p *javaParser) parseTypeDecl(mods javaMods, parent *Node) {
	startLine := p.cur().line

	// Annotation-type: @interface — capture as interface, push specifics to Unknown.
	if p.isPunct("@") {
		p.advance() // @
	}

	kw := ""
	if p.cur().kind == jtIdent {
		kw = p.advance().text // class|interface|enum|record
	}
	if p.cur().kind != jtIdent {
		// Malformed; resync.
		p.recordUnknownStmt(parent, "type declaration without a name")
		return
	}
	name := p.advance().text

	kind := NodeClass
	switch kw {
	case "interface":
		kind = NodeInterface
	case "enum":
		kind = NodeEnum
	case "record":
		// Java records map to NodeClass (parity with how classes resolve);
		// the record components are captured as Fields below.
		kind = NodeClass
	}

	node := &Node{
		ID:         "type:" + name,
		Kind:       kind,
		Name:       name,
		Language:   LangJava,
		Span:       Span{Start: Pos{Line: startLine}},
		Exported:   mods.names["public"],
		Abstract:   mods.names["abstract"],
		Static:     mods.names["static"],
		DocComment: mods.annoDoc(),
	}
	if parent != nil {
		if node.DocComment != "" {
			node.DocComment += " "
		}
		node.DocComment += "(inner type of " + parent.Name + ")"
	}

	// Generic type parameters <T, U extends X>: capture names, skip the rest.
	if p.isPunct("<") {
		node.TypeParams = p.collectTypeParamNames()
	}

	// record(components): treat as Fields.
	if kw == "record" && p.isPunct("(") {
		node.Fields = append(node.Fields, p.parseRecordComponents()...)
	}

	// extends / implements / permits.
	for !p.eof() && !p.isPunct("{") && !p.isPunct(";") {
		switch {
		case p.isKeyword("extends"):
			p.advance()
			node.Extends = p.collectTypeRefName()
			// interface extends A, B, C → keep first in Extends, rest in Implements
			for p.isPunct(",") {
				p.advance()
				node.Implements = append(node.Implements, p.collectTypeRefName())
			}
		case p.isKeyword("implements"):
			p.advance()
			node.Implements = append(node.Implements, p.collectTypeRefName())
			for p.isPunct(",") {
				p.advance()
				node.Implements = append(node.Implements, p.collectTypeRefName())
			}
		case p.isKeyword("permits"):
			p.advance()
			// sealed permits-list → note, don't model.
			_ = p.collectTypeRefName()
			for p.isPunct(",") {
				p.advance()
				_ = p.collectTypeRefName()
			}
		default:
			p.advance()
		}
	}

	p.mod.Nodes = append(p.mod.Nodes, node)

	// Body.
	if p.isPunct("{") {
		p.parseTypeBody(node)
	} else if p.isPunct(";") {
		p.advance()
	}
}

// collectTypeParamNames reads <A, B extends C, ?> and returns the simple names
// (A, B). Bounded wildcards and complex bounds are not modelled — the names are
// enough for the symbol layer; the rest is intentionally not interpreted.
func (p *javaParser) collectTypeParamNames() []string {
	var names []string
	if !p.isPunct("<") {
		return names
	}
	depth := 0
	expectName := true
	for !p.eof() {
		if p.isPunct("<") {
			depth++
			p.advance()
			continue
		}
		if p.isPunct(">") {
			depth--
			p.advance()
			if depth == 0 {
				return names
			}
			continue
		}
		if depth == 1 && expectName && p.cur().kind == jtIdent {
			names = append(names, p.cur().text)
			expectName = false
		}
		if p.isPunct(",") && depth == 1 {
			expectName = true
		}
		p.advance()
	}
	return names
}

// collectTypeRefName reads a (possibly qualified, possibly generic) type name
// and returns its leading simple name, skipping any generic arguments.
func (p *javaParser) collectTypeRefName() string {
	name := ""
	if p.cur().kind == jtIdent {
		name = p.advance().text
		for p.isPunct(".") && p.peek(1).kind == jtIdent {
			p.advance()
			name += "." + p.advance().text
		}
	}
	if p.isPunct("<") {
		p.skipBalanced("<", ">")
	}
	// array dims
	for p.isPunct("[") {
		p.skipBalanced("[", "]")
	}
	return name
}

func (p *javaParser) parseRecordComponents() []Field {
	var fields []Field
	if !p.isPunct("(") {
		return fields
	}
	p.advance() // (
	for !p.eof() && !p.isPunct(")") {
		// v26.58 (D058): a round that met a token it did not expect, such as
		// the dots of "int... xs" or a stray "=", consumed nothing and the loop
		// never ended. Varargs dots are skipped as parseParamList does, and a
		// round that still consumed nothing advances one token, so the loop
		// always reaches ")" or EOF.
		start := p.pos
		// component: [annotations] Type name
		p.collectLeadingMods()
		typeName := p.collectTypeRefName()
		if p.isPunct(".") && p.peek(1).kind == jtPunct && p.peek(1).text == "." {
			for p.isPunct(".") {
				p.advance()
			}
		}
		if p.cur().kind == jtIdent {
			fname := p.advance().text
			fields = append(fields, Field{Name: fname, Type: TypeRef{Name: typeName}})
		}
		if p.isPunct(",") {
			p.advance()
		}
		if p.pos == start {
			p.advance()
		}
	}
	if p.isPunct(")") {
		p.advance()
	}
	return fields
}

// ─── type body: members ──────────────────────────────────────

// parseTypeBody consumes a { ... } body, dispatching each member to a field,
// method, nested type, or — for enums — the leading constant list. Assumes the
// opening { has NOT been consumed.
func (p *javaParser) parseTypeBody(owner *Node) {
	if !p.isPunct("{") {
		return
	}
	p.advance() // {

	// Enum constants come first, before any ; — capture them as EnumMembers.
	if owner.Kind == NodeEnum {
		p.parseEnumConstants(owner)
	}

	for !p.eof() {
		if p.isPunct("}") {
			p.advance()
			return
		}
		if p.isPunct(";") {
			p.advance()
			continue
		}

		mods := p.collectLeadingMods()

		// Nested type.
		if p.atTypeKeyword() {
			p.parseTypeDecl(mods, owner)
			continue
		}
		// Static / instance initialiser block: just skip its braces.
		if p.isPunct("{") {
			p.skipBalanced("{", "}")
			continue
		}
		// Otherwise: a field or a method. Both start with [type-params] type name.
		p.parseFieldOrMethod(owner, mods)
	}
}

func (p *javaParser) parseEnumConstants(owner *Node) {
	for !p.eof() {
		if p.cur().kind == jtIdent {
			owner.EnumMembers = append(owner.EnumMembers, EnumMember{Name: p.cur().text})
			p.advance()
			// constant args ( ... )
			if p.isPunct("(") {
				p.skipBalanced("(", ")")
			}
			// constant body { ... }
			if p.isPunct("{") {
				p.skipBalanced("{", "}")
			}
			if p.isPunct(",") {
				p.advance()
				continue
			}
		}
		// end of constant list
		if p.isPunct(";") {
			p.advance()
			return
		}
		if p.isPunct("}") || p.eof() {
			return
		}
		// anything unexpected — bail to member loop
		return
	}
}

// parseFieldOrMethod distinguishes a method (has a parameter list before { or ;)
// from a field. Constructors (name == owner.Name, no return type) are treated as
// methods named like the class.
func (p *javaParser) parseFieldOrMethod(owner *Node, mods javaMods) {
	startLine := p.cur().line

	// Optional method type parameters: <T> before the return type.
	if p.isPunct("<") {
		p.skipBalanced("<", ">")
	}

	// Read a leading type (return type or field type) and then a name. For a
	// constructor there is no separate return type — the first identifier is the
	// name and ( follows immediately.
	firstName := p.collectTypeRefName()
	secondName := ""
	if p.cur().kind == jtIdent {
		secondName = p.advance().text
	}

	var declName, retType string
	if secondName == "" {
		// Only one identifier read → constructor (name == firstName) or a
		// malformed member. If ( follows, it's a constructor.
		declName = firstName
		retType = ""
	} else {
		retType = firstName
		declName = secondName
	}

	// Method: a ( parameter list ) follows.
	if p.isPunct("(") {
		method := &Node{
			ID:         "method:" + owner.Name + "." + declName,
			Kind:       NodeMethod,
			Name:       declName,
			Language:   LangJava,
			Span:       Span{Start: Pos{Line: startLine}},
			Exported:   mods.names["public"],
			Abstract:   mods.names["abstract"],
			Static:     mods.names["static"],
			DocComment: mods.annoDoc(),
			Receiver:   &TypeRef{Name: owner.Name},
		}
		if retType != "" {
			method.Returns = []TypeRef{{Name: retType}}
		}
		method.Params = p.parseParamList()

		// throws X, Y
		if p.isKeyword("throws") {
			p.advance()
			_ = p.collectTypeRefName()
			for p.isPunct(",") {
				p.advance()
				_ = p.collectTypeRefName()
			}
		}

		// Body, abstract/interface method (;), or default.
		if p.isPunct("{") {
			p.parseMethodBody(method)
		} else if p.isPunct(";") {
			p.advance() // abstract/interface method, no body
		}
		p.mod.Nodes = append(p.mod.Nodes, method)
		return
	}

	// Field(s): retType declName [= ...] [, more] ;
	if declName != "" {
		field := &Node{
			ID:       "field:" + owner.Name + "." + declName,
			Kind:     NodeVariable,
			Name:     declName,
			Language: LangJava,
			Span:     Span{Start: Pos{Line: startLine}},
			Exported: mods.names["public"],
			Static:   mods.names["static"],
			TypeHint: TypeRef{Name: retType},
		}
		// Attach as a Field on the owner too (so type Fields reflect members).
		owner.Fields = append(owner.Fields, Field{
			Name:     declName,
			Type:     TypeRef{Name: retType},
			Exported: mods.names["public"],
		})
		p.mod.Nodes = append(p.mod.Nodes, field)
	}
	// Skip to end of the field declaration (handles initialisers, commas, arrays).
	p.skipToStatementEnd()
}

func (p *javaParser) parseParamList() []Parameter {
	var params []Parameter
	if !p.isPunct("(") {
		return params
	}
	p.advance() // (
	for !p.eof() && !p.isPunct(")") {
		// v26.58 (F4): a round that met a token it did not expect, such as the
		// "=" of "int x = 1", consumed nothing and the loop never ended. As in
		// parseRecordComponents (D058), a round that consumed nothing advances
		// one token, so the loop always reaches ")" or EOF.
		start := p.pos
		p.collectLeadingMods() // param annotations / final
		typeName := p.collectTypeRefName()
		variadic := false
		if p.isPunct(".") && p.peek(1).kind == jtPunct && p.peek(1).text == "." {
			// varargs ... — three dots lexed as separate '.'
			for p.isPunct(".") {
				p.advance()
			}
			variadic = true
		}
		if p.cur().kind == jtIdent {
			pname := p.advance().text
			params = append(params, Parameter{
				Name:     pname,
				Type:     TypeRef{Name: typeName},
				Variadic: variadic,
			})
		}
		if p.isPunct(",") {
			p.advance()
		}
		if p.pos == start {
			p.advance()
		}
	}
	if p.isPunct(")") {
		p.advance()
	}
	return params
}

// ─── method body: statement walk ─────────────────────────────
//
// We emit the structural nodes the shared (non-Go) CFG builder understands:
// NodeBranch (with Else), NodeLoop, NodeReturn, NodeTry, and NodeCall. We do
// NOT attempt full expression parsing — a statement contributes NodeCall
// children for the method invocations it textually contains (callee = the
// identifier before a '('), which is exactly what the symbol index needs to
// build the call graph. Unrecognised constructs become NodeUnknown.

// parseMethodBody assumes the opening { has NOT been consumed.
func (p *javaParser) parseMethodBody(method *Node) {
	if !p.isPunct("{") {
		return
	}
	p.advance() // {
	p.parseBlockInto(method)
}

// parseBlockInto walks statements until the matching } (which it consumes),
// appending structural children to host.
func (p *javaParser) parseBlockInto(host *Node) {
	for !p.eof() {
		if p.isPunct("}") {
			p.advance()
			return
		}
		p.parseStatement(host)
	}
}

func (p *javaParser) parseStatement(host *Node) {
	t := p.cur()

	// Nested block.
	if p.isPunct("{") {
		p.advance()
		p.parseBlockInto(host)
		return
	}
	if p.isPunct(";") {
		p.advance()
		return
	}

	if t.kind == jtIdent {
		switch t.text {
		case "if":
			p.parseIf(host)
			return
		case "for", "while":
			p.parseLoop(host, t.text, t.line)
			return
		case "do":
			p.parseDoWhile(host, t.line)
			return
		case "switch":
			p.parseSwitch(host, t.line)
			return
		case "return":
			p.parseReturn(host, t.line)
			return
		case "try":
			p.parseTry(host, t.line)
			return
		case "throw":
			// throw expr; — capture as a return-like terminator with its calls.
			line := t.line
			p.advance()
			calls := p.collectStatementCalls()
			node := &Node{Kind: NodeReturn, Name: "throw", Language: LangJava,
				Span: Span{Start: Pos{Line: line}}}
			node.Children = append(node.Children, calls...)
			host.Children = append(host.Children, node)
			return
		case "synchronized":
			// synchronized (x) { ... } — descend into the block.
			p.advance()
			if p.isPunct("(") {
				p.skipBalanced("(", ")")
			}
			if p.isPunct("{") {
				p.advance()
				p.parseBlockInto(host)
			}
			return
		case "yield", "break", "continue":
			p.skipToStatementEnd()
			return
		}
	}

	// Lambda or method reference somewhere in the statement → push to Unknown so
	// it's surfaced rather than mis-modelled, BUT still harvest calls so the
	// call graph stays useful.
	if p.statementHasLambda() {
		line := t.line
		calls := p.collectStatementCalls()
		host.Children = append(host.Children, &Node{
			Kind:          NodeUnknown,
			Language:      LangJava,
			Span:          Span{Start: Pos{Line: line}},
			RawSource:     "lambda/method-ref expression",
			UnknownReason: "lambda or method reference not modelled",
		})
		host.Children = append(host.Children, calls...)
		return
	}

	// Generic expression / local-variable / assignment statement. v24: harvest
	// its calls AND, if it produced none (e.g. a plain "this.inv = inv;" in a
	// constructor), still emit a NodeAssign carrying the statement's source line
	// so the CFG shows the real work instead of collapsing to Start→End. The
	// presentation layer reads the source line via Span, so we just need a node
	// anchored at the right line.
	stmtLine := t.line
	calls := p.collectStatementCalls()
	if len(calls) > 0 {
		host.Children = append(host.Children, calls...)
	} else if stmtLine > 0 {
		host.Children = append(host.Children, &Node{
			Kind:     NodeAssign,
			Language: LangJava,
			Span:     Span{Start: Pos{Line: stmtLine}},
		})
	}
}

// parseIf: if (cond) thenStmt [else elseStmt]. Condition calls + then-body go in
// Children; else-body goes in Else (v23.7 distinct-else contract).
func (p *javaParser) parseIf(host *Node) {
	line := p.cur().line
	p.advance() // if
	branch := &Node{Kind: NodeBranch, Name: "if", Language: LangJava,
		Span: Span{Start: Pos{Line: line}}}
	// condition
	if p.isPunct("(") {
		condCalls, condText := p.collectParenCallsWithText()
		branch.Children = append(branch.Children, condCalls...)
		branch.RawSource = condText // v24: so the CFG decision shows the real condition
	}
	// then
	p.collectBranchArm(&branch.Children)
	// else
	if p.isKeyword("else") {
		p.advance()
		p.collectBranchArm(&branch.Else)
	}
	host.Children = append(host.Children, branch)
}

// collectBranchArm parses a single arm (a block or a single statement) into the
// given child slice via a synthetic host.
func (p *javaParser) collectBranchArm(into *[]*Node) {
	tmp := &Node{}
	if p.isPunct("{") {
		p.advance()
		p.parseBlockInto(tmp)
	} else {
		p.parseStatement(tmp)
	}
	*into = append(*into, tmp.Children...)
}

func (p *javaParser) parseLoop(host *Node, kw string, line int) {
	p.advance() // for|while
	loop := &Node{Kind: NodeLoop, Name: kw, Language: LangJava,
		Span: Span{Start: Pos{Line: line}}}
	if p.isPunct("(") {
		condCalls, condText := p.collectParenCallsWithText()
		loop.Children = append(loop.Children, condCalls...)
		loop.RawSource = condText // v24: loop header shows the real condition
	}
	p.collectBranchArm(&loop.Children)
	host.Children = append(host.Children, loop)
}

func (p *javaParser) parseDoWhile(host *Node, line int) {
	p.advance() // do
	loop := &Node{Kind: NodeLoop, Name: "do", Language: LangJava,
		Span: Span{Start: Pos{Line: line}}}
	p.collectBranchArm(&loop.Children)
	if p.isKeyword("while") {
		p.advance()
		if p.isPunct("(") {
			condCalls, condText := p.collectParenCallsWithText()
			loop.Children = append(loop.Children, condCalls...)
			loop.RawSource = condText // v24
		}
	}
	p.skipToStatementEnd()
	host.Children = append(host.Children, loop)
}

// parseSwitch handles both statement switches and switch expressions, including
// arrow (->) cases (Java 14+/17). Each case body's calls are harvested; the
// whole switch is one NodeBranch with per-arm children.
func (p *javaParser) parseSwitch(host *Node, line int) {
	p.advance() // switch
	branch := &Node{Kind: NodeBranch, Name: "switch", Language: LangJava,
		Span: Span{Start: Pos{Line: line}}}
	if p.isPunct("(") {
		branch.Children = append(branch.Children, p.collectParenCalls()...)
	}
	if p.isPunct("{") {
		p.advance()
		p.parseBlockInto(branch) // case/default labels become harvested calls/stmts
	}
	host.Children = append(host.Children, branch)
}

func (p *javaParser) parseReturn(host *Node, line int) {
	p.advance() // return
	// return switch (...) { ... };  → switch EXPRESSION; emit the branch.
	if p.isKeyword("switch") {
		p.parseSwitch(host, line)
		// consume a trailing ';' if present
		if p.isPunct(";") {
			p.advance()
		}
		return
	}
	node := &Node{Kind: NodeReturn, Name: "return", Language: LangJava,
		Span: Span{Start: Pos{Line: line}}}
	node.Children = append(node.Children, p.collectStatementCalls()...)
	host.Children = append(host.Children, node)
}

// parseTry handles try / try-with-resources / catch / finally as a NodeTry whose
// Children are the try-block (and resource/catch/finally) calls.
func (p *javaParser) parseTry(host *Node, line int) {
	p.advance() // try
	node := &Node{Kind: NodeTry, Name: "try", Language: LangJava,
		Span: Span{Start: Pos{Line: line}}}
	// try-with-resources ( ... )
	if p.isPunct("(") {
		node.Children = append(node.Children, p.collectParenCalls()...)
	}
	if p.isPunct("{") {
		p.advance()
		p.parseBlockInto(node) // try body → Children (the main/normal path)
	}
	// v24: catch/finally bodies go into Else (the exception/cleanup path) so the
	// CFG builder can model try as a branch-like structure (try → merge, and
	// try → catch → merge) instead of one flat "try {" node with its body lost.
	for p.isKeyword("catch") || p.isKeyword("finally") {
		if p.isKeyword("catch") {
			p.advance()
			if p.isPunct("(") {
				p.skipBalanced("(", ")")
			}
		} else {
			p.advance() // finally
		}
		if p.isPunct("{") {
			p.advance()
			arm := &Node{Kind: NodeBranch, Name: "catch", Language: LangJava,
				Span: Span{Start: Pos{Line: p.cur().line}}}
			p.parseBlockInto(arm)
			node.Else = append(node.Else, arm.Children...)
		}
	}
	host.Children = append(host.Children, node)
}

// ─── call harvesting ─────────────────────────────────────────

// collectStatementCalls consumes tokens up to and including the statement
// terminator ';' (respecting nested () {} [] and string/char literals) and
// returns a NodeCall for each method invocation found: an identifier
// immediately followed by '('. The "new Type(" form is recorded as a call to
// the constructor's type name.
func (p *javaParser) collectStatementCalls() []*Node {
	var calls []*Node
	depthCurly := 0
	for !p.eof() {
		if p.isPunct(";") && depthCurly == 0 {
			p.advance()
			return calls
		}
		if p.isPunct("{") {
			depthCurly++
			p.advance()
			continue
		}
		if p.isPunct("}") {
			if depthCurly == 0 {
				return calls // end of enclosing block; don't consume
			}
			depthCurly--
			p.advance()
			continue
		}
		p.harvestCallAt(&calls)
		if p.eof() {
			break
		}
		p.advance()
	}
	return calls
}

// collectParenCalls consumes a balanced ( ... ) and returns the calls inside it
// (used for conditions and resource lists). Assumes ( is current.
func (p *javaParser) collectParenCalls() []*Node {
	calls, _ := p.collectParenCallsWithText()
	return calls
}

// collectParenCallsWithText is collectParenCalls plus the raw condition TEXT
// between the outer parens (v24). parseIf/parseLoop discarded the condition text,
// so the CFG's NodeBranch/NodeLoop had an empty RawSource → the renderer showed
// the literal word "decision" instead of e.g. "have < qty" or
// "int q : stock.values()". We now accumulate each token's text so the condition
// is available for block.Cond.
func (p *javaParser) collectParenCallsWithText() ([]*Node, string) {
	var calls []*Node
	if !p.isPunct("(") {
		return calls, ""
	}
	var sb []string
	depth := 0
	for !p.eof() {
		if p.isPunct("(") {
			depth++
			p.harvestCallAt(&calls)
			if depth > 1 {
				sb = append(sb, "(")
			}
			p.advance()
			continue
		}
		if p.isPunct(")") {
			depth--
			p.advance()
			if depth == 0 {
				return calls, joinCondTokens(sb)
			}
			sb = append(sb, ")")
			continue
		}
		p.harvestCallAt(&calls)
		sb = append(sb, p.cur().text)
		p.advance()
	}
	return calls, joinCondTokens(sb)
}

// harvestCallAt: if the current position is an identifier (or `new Type`)
// followed by '(', append a NodeCall. Does not advance past the name (caller's
// loop advances by one), so it's safe to call in any scan loop.
func (p *javaParser) harvestCallAt(calls *[]*Node) {
	// new Type(...)
	if p.isKeyword("new") && p.peek(1).kind == jtIdent {
		// find the type name (possibly dotted/generic) then '('
		name := p.peek(1).text
		if p.peek(2).kind == jtPunct && (p.peek(2).text == "(" || p.peek(2).text == "<") {
			*calls = append(*calls, &Node{
				Kind: NodeCall, Callee: name, Language: LangJava,
				Span: Span{Start: Pos{Line: p.cur().line}},
			})
		}
		return
	}
	if p.cur().kind == jtIdent {
		nx := p.peek(1)
		if nx.kind == jtPunct && nx.text == "(" {
			// Exclude control keywords that take ( but aren't calls.
			switch p.cur().text {
			case "if", "for", "while", "switch", "catch", "synchronized", "return":
				return
			}
			*calls = append(*calls, &Node{
				Kind: NodeCall, Callee: p.cur().text, Language: LangJava,
				Span: Span{Start: Pos{Line: p.cur().line}},
			})
		}
	}
}

// statementHasLambda peeks ahead (bounded) to see if the current statement, up
// to its terminating ; at paren/brace depth 0, contains a lambda arrow '->'.
// v24: a bare method reference '::' (e.g. Integer::sum passed as an argument to a
// normal call like stock.merge(sku, qty, Integer::sum)) does NOT count — it is a
// callable argument, not a statement-dominating lambda. Treating '::' as a lambda
// here was pushing perfectly ordinary statements to NodeUnknown and hiding their
// real call (the "stock.merge missing" bug).
func (p *javaParser) statementHasLambda() bool {
	depthP, depthC := 0, 0
	for k := p.pos; k < len(p.toks); k++ {
		t := p.toks[k]
		if t.kind == jtPunct {
			switch t.text {
			case "(":
				depthP++
			case ")":
				depthP--
			case "{":
				depthC++
			case "}":
				if depthC == 0 {
					return false
				}
				depthC--
			case ";":
				if depthP == 0 && depthC == 0 {
					return false
				}
			case "->":
				return true
			}
		}
	}
	return false
}

// ─── resync / misc helpers ───────────────────────────────────

// skipToStatementEnd advances to and consumes the next ';' at curly-depth 0,
// or stops at an unbalanced '}'. Guarantees forward progress.
func (p *javaParser) skipToStatementEnd() {
	depth := 0
	for !p.eof() {
		if p.isPunct("{") {
			depth++
		} else if p.isPunct("}") {
			if depth == 0 {
				return
			}
			depth--
		} else if p.isPunct(";") && depth == 0 {
			p.advance()
			return
		}
		p.advance()
	}
}

// recordUnknownStmt appends a NodeUnknown (to host if non-nil, else the module)
// and resyncs to the next ; or }. Guarantees forward progress so the parser can
// never spin on an unrecognised construct.
func (p *javaParser) recordUnknownStmt(host *Node, reason string) {
	line := p.cur().line
	n := &Node{
		Kind:          NodeUnknown,
		Language:      LangJava,
		Span:          Span{Start: Pos{Line: line}},
		UnknownReason: reason,
		RawSource:     p.cur().text,
	}
	if host != nil {
		host.Children = append(host.Children, n)
	} else {
		p.mod.Nodes = append(p.mod.Nodes, n)
	}
	p.mod.ParseErrors = append(p.mod.ParseErrors, ParseError{
		Message: reason, Span: Span{Start: Pos{Line: line}}, Recoverable: true,
	})
	// Forward progress: consume at least one token, then resync.
	if !p.eof() {
		p.advance()
	}
	p.skipToStatementEnd()
}

func javaBaseName(path string) string {
	b := path
	if i := strings.LastIndexAny(b, "/\\"); i >= 0 {
		b = b[i+1:]
	}
	b = strings.TrimSuffix(b, ".java")
	return b
}
