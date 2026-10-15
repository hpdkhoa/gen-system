// v23.39: COBOL IF NodeBranch now preserves the condition text in RawSource for the flow-chart decision node.
// cobol_frontend.go (v23.33, COBOL runtime catalog): emits a NodeCall for each
// FUNCTION <name> intrinsic reference so intrinsic usage is visible in the call
// graph and resolvable against the synthetic COBOL runtime catalog. Prior:
// cobol_frontend.go (v23.9, Phase 6): fixed two pre-existing test failures.
// Single-line EXEC CICS/SQL no longer swallows the next statement (parseCICSBlock
// /parseSQLBlock guard on END-EXEC already on the start line), and COPY REPLACING
// now does pseudo-text substring substitution (applyReplacing) so PREFIX-RECORD
// becomes WS-CUSTOMER-RECORD.
// cobol_frontend.go: COBOL/CICS language frontend (v19, feature-complete)
//
// v19.6 hotfix: Node has no Path or Tag fields. SELECT entries now use
// ImportPath for the ASSIGN target; Node-level COBOL metadata (FD marker,
// section name, FILE-CONTROL clauses) is encoded into RawSource which is
// the canonical opaque-payload field on the unified Node IR. Field.Tag
// remains the right home for field-level COBOL metadata (PIC, USAGE,
// OCCURS, REDEFINES, VALUE).
//
// Pure-Go hand-rolled parser; no external dependencies. Handles COBOL
// divisions, sections, paragraphs, and CICS EXEC blocks. Designed for
// real banking codebases.
//
// v19 (this patch) makes the frontend feature-complete enough to ungate
// the registration. Coverage added:
//   - EVALUATE / WHEN / WHEN OTHER       → NodeBranch + per-WHEN children
//   - COPY <book> [REPLACING ...]        → inlines copybook source
//   - REDEFINES <field>                  → field-level Tag metadata
//   - OCCURS n [DEPENDING ON <name>]     → field-level Tag metadata
//   - OCCURS n TIMES (and 0 TO n TIMES)  → cardinality captured
//   - FILE-CONTROL / SELECT / ASSIGN     → file binding NodeImport entries
//   - FD <file>                          → file descriptor NodeStruct
//   - USAGE: COMP-3 (packed decimal),
//     COMP / BINARY / COMP-1/2/4/5, DISPLAY → field Tag metadata
//   - CICS verbs: READ, WRITE, REWRITE,
//     DELETE, LINK, XCTL, SEND,
//     RECEIVE, RETURN, ABEND,
//     SYNCPOINT, ASSIGN, WRITEQ,
//     READQ, DELETEQ, READNEXT,
//     STARTBR, ENDBR, RESETBR
//   - PIC clauses now recognise editing chars (Z, $, etc.) and the
//     full PIC string up to the next clause keyword.
//
// Copybook resolution: CobolFrontend exposes a CopybookPath slice and a
// CopybookExtensions slice. ParseWithBooks resolves COPY books against
// these. The default Parse() leaves COPY directives unresolved (emits
// NodeUnknown{copy_unresolved}) so the engine can still parse a file in
// isolation.
package ast

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LangCOBOL is the canonical language identifier for COBOL/CICS source.
const LangCOBOL Language = "cobol"

// CobolFrontend implements Frontend for COBOL and COBOL/CICS source files.
type CobolFrontend struct {
	// CopybookPath is an ordered list of directories searched when
	// resolving COPY <name> directives. If empty, COPY directives are
	// left unresolved (NodeUnknown{copy_unresolved} is emitted).
	CopybookPath []string

	// CopybookExtensions are the extensions tried for each COPY name.
	// Defaults to {".cpy", ".cbl", ".cob"} when empty.
	CopybookExtensions []string
}

func (c *CobolFrontend) Language() Language { return LangCOBOL }
func (c *CobolFrontend) Extensions() []string {
	return []string{".cob", ".cbl", ".cpy", ".cobol"}
}
func (c *CobolFrontend) Available() bool { return true } // pure Go

// ─── Parse ────────────────────────────────────────────────────

// Parse parses a COBOL source file. COPY directives are resolved if
// CopybookPath is non-empty; otherwise they are recorded as unresolved.
func (c *CobolFrontend) Parse(filePath string, source []byte) (*Module, error) {
	sum := sha256.Sum256(source)
	m := &Module{
		Language:  LangCOBOL,
		FilePath:  filePath,
		Checksum:  fmt.Sprintf("%x", sum),
		RawSource: source,
	}

	p := &cobolParser{
		filePath:           filePath,
		lines:              tokenizeCOBOL(source),
		copybookPath:       c.CopybookPath,
		copybookExtensions: c.copybookExtensionsOrDefault(),
		visitedBooks:       map[string]bool{},
	}
	// Resolve COPY directives once before structural parsing. This keeps
	// the line numbering simple (copybook lines are inlined as if they
	// had appeared at the COPY site).
	p.lines = p.expandCopybooks(p.lines)
	programs := 0
	for i := range p.lines {
		if strings.HasPrefix(p.lines[i].upper, "PROGRAM-ID") {
			programs++
		}
	}
	p.multiProgram = programs > 1

	p.parseFile(m)

	// Set PackageName to the PROGRAM-ID if found.
	for _, n := range m.Nodes {
		if n.Kind == NodeModule && n.Name != "" {
			m.PackageName = n.Name
			break
		}
	}
	if m.PackageName == "" {
		m.PackageName = cobolBaseName(filePath)
	}

	return m, nil
}

// Emit returns source unchanged. COBOL reformatting is out of scope.
func (c *CobolFrontend) Emit(module *Module) ([]byte, error) {
	return module.RawSource, nil
}

func (c *CobolFrontend) copybookExtensionsOrDefault() []string {
	if len(c.CopybookExtensions) > 0 {
		return c.CopybookExtensions
	}
	return []string{".cpy", ".cbl", ".cob"}
}

// ─── Tokeniser ────────────────────────────────────────────────

// cobolLine is one logical COBOL line after column stripping.
type cobolLine struct {
	lineNo int
	raw    string   // original text (preserved for RawSource fields)
	text   string   // content area (cols 7 to 72 for fixed format, whole line for free format)
	upper  string   // upper-cased content for keyword matching
	tokens []string // whitespace-split tokens (upper-cased)
}

// tokenizeCOBOL splits source into logical lines, strips sequence numbers
// and indicator column for fixed-format COBOL, skips comment lines, and
// merges continuation lines.
func tokenizeCOBOL(src []byte) []cobolLine {
	rawLines := strings.Split(string(src), "\n")
	var out []cobolLine
	for i, raw := range rawLines {
		lineNo := i + 1
		line := strings.TrimRight(raw, "\r")

		text := line
		isContinuation := false
		if len(line) >= 7 {
			seq := strings.TrimSpace(line[:6])
			if isNumericOrBlank(seq) {
				indicator := ' '
				if len(line) > 6 {
					indicator = rune(line[6])
				}
				if indicator == '*' || indicator == '/' || indicator == 'D' {
					continue
				}
				if indicator == '-' {
					isContinuation = true
				}
				end := len(line)
				if end > 72 {
					end = 72
				}
				if len(line) > 7 {
					text = strings.TrimSpace(line[7:end])
				} else {
					text = ""
				}
			}
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, "*") || strings.HasPrefix(text, "//") {
			continue
		}
		if idx := strings.Index(text, " *> "); idx >= 0 {
			text = strings.TrimSpace(text[:idx])
		}

		if isContinuation && len(out) > 0 {
			// Merge into previous logical line.
			prev := &out[len(out)-1]
			prev.text += " " + text
			prev.upper = strings.ToUpper(prev.text)
			prev.tokens = strings.Fields(prev.upper)
			prev.raw += "\n" + raw
			continue
		}

		upper := strings.ToUpper(text)
		toks := strings.Fields(upper)
		if len(toks) == 0 {
			continue
		}
		out = append(out, cobolLine{
			lineNo: lineNo,
			raw:    raw,
			text:   text,
			upper:  upper,
			tokens: toks,
		})
	}
	return out
}

func isNumericOrBlank(s string) bool {
	if s == "" {
		return true
	}
	for _, c := range s {
		if c != ' ' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// ─── Parser ───────────────────────────────────────────────────

type cobolParser struct {
	filePath string
	lines    []cobolLine
	pos      int

	// copybook resolution
	copybookPath       []string
	copybookExtensions []string
	visitedBooks       map[string]bool // cycle guard

	// pending NodeUnknown copy_unresolved nodes (added to module post-parse)
	pendingUnresolvedCopies []*Node

	// v26.69: one file may hold several programs (IDENTIFICATION DIVISION ...
	// END PROGRAM, one after another). program is the NodeModule being parsed;
	// multiProgram is true when the file has more than one PROGRAM-ID, and then
	// paragraphs carry their program's name so equal names stay apart.
	program      *Node
	multiProgram bool
}

func (p *cobolParser) peek() *cobolLine {
	if p.pos >= len(p.lines) {
		return nil
	}
	return &p.lines[p.pos]
}

func (p *cobolParser) consume() *cobolLine {
	if p.pos >= len(p.lines) {
		return nil
	}
	l := &p.lines[p.pos]
	p.pos++
	return l
}

func (p *cobolParser) firstToken() string {
	l := p.peek()
	if l == nil || len(l.tokens) == 0 {
		return ""
	}
	return l.tokens[0]
}

// ─── Copybook expansion ──────────────────────────────────────

// expandCopybooks walks the line stream once and replaces any
//
//	COPY <name> [REPLACING ...].
//
// directive with the tokenised contents of the resolved copybook.
// When CopybookPath is empty the COPY line is left in place and a
// NodeUnknown{copy_unresolved} entry is added to pendingUnresolvedCopies.
func (p *cobolParser) expandCopybooks(in []cobolLine) []cobolLine {
	out := make([]cobolLine, 0, len(in))
	for i := 0; i < len(in); i++ {
		l := in[i]
		if len(l.tokens) >= 2 && l.tokens[0] == "COPY" {
			bookName := strings.TrimSuffix(l.tokens[1], ".")
			bookName = strings.Trim(bookName, `"'`)

			content, resolvedPath, ok := p.findCopybook(bookName)
			if !ok {
				p.pendingUnresolvedCopies = append(p.pendingUnresolvedCopies, &Node{
					Kind:          NodeUnknown,
					Language:      LangCOBOL,
					UnknownReason: "copy_unresolved:" + bookName,
					Span:          Span{Start: Pos{Line: l.lineNo}},
				})
				out = append(out, l) // keep the COPY line for visibility
				continue
			}

			// Cycle guard: avoid infinite loops on self-referential books.
			if p.visitedBooks[resolvedPath] {
				p.pendingUnresolvedCopies = append(p.pendingUnresolvedCopies, &Node{
					Kind:          NodeUnknown,
					Language:      LangCOBOL,
					UnknownReason: "copy_cycle:" + bookName,
					Span:          Span{Start: Pos{Line: l.lineNo}},
				})
				continue
			}
			p.visitedBooks[resolvedPath] = true

			// Parse REPLACING clauses if present.
			replacements := parseCopyReplacing(l.tokens)

			// Tokenise the copybook with line numbers continuing from the
			// current source line (so spans point near the COPY site).
			bookLines := tokenizeCOBOL(content)
			// Recursively expand nested COPYs in the copybook.
			bookLines = p.expandCopybooks(bookLines)
			// v26.58 (D056): the mark stayed set after this expansion, so a
			// second COPY of the same book (e.g. with another REPLACING) was
			// dropped as copy_cycle. A cycle only exists while the book is on
			// the current expansion path, so clear the mark once it returns.
			delete(p.visitedBooks, resolvedPath)
			// Apply REPLACING substitutions.
			for j := range bookLines {
				bookLines[j].lineNo = l.lineNo
				applyReplacing(&bookLines[j], replacements)
			}
			out = append(out, bookLines...)
		} else {
			out = append(out, l)
		}
	}
	return out
}

// findCopybook searches CopybookPath for a file matching bookName with
// one of the configured extensions. Returns (content, resolvedPath, ok).
func (p *cobolParser) findCopybook(bookName string) ([]byte, string, bool) {
	if len(p.copybookPath) == 0 {
		return nil, "", false
	}
	candidates := []string{bookName} // exact name first (in case it has extension)
	for _, ext := range p.copybookExtensions {
		candidates = append(candidates, bookName+ext, strings.ToLower(bookName)+ext)
	}
	for _, dir := range p.copybookPath {
		for _, name := range candidates {
			path := filepath.Join(dir, name)
			if data, err := os.ReadFile(path); err == nil {
				return data, path, true
			}
		}
	}
	return nil, "", false
}

// copyReplacement is one ==pattern== BY ==replacement== rule.
type copyReplacement struct {
	from string
	to   string
}

// parseCopyReplacing extracts REPLACING ==from== BY ==to== pairs from
// a COPY directive's token stream. Returns empty if no REPLACING clause.
func parseCopyReplacing(tokens []string) []copyReplacement {
	var out []copyReplacement
	i := 0
	for i < len(tokens) {
		if tokens[i] == "REPLACING" {
			i++
			break
		}
		i++
	}
	for i < len(tokens) {
		from := strings.Trim(tokens[i], "=.,")
		if from == "" {
			i++
			continue
		}
		// expect BY between from and to
		j := i + 1
		for j < len(tokens) && tokens[j] != "BY" {
			j++
		}
		if j >= len(tokens)-1 {
			break
		}
		to := strings.Trim(tokens[j+1], "=.,")
		out = append(out, copyReplacement{from: from, to: to})
		i = j + 2
	}
	return out
}

// applyReplacing substitutes the REPLACING patterns in a line's tokens.
//
// v23.9 (Phase 6): COBOL pseudo-text replacing (`==PREFIX== BY ==WS-CUSTOMER==`)
// is substring substitution within the copybook text, not whole-token equality.
// A copybook field `PREFIX-RECORD` must become `WS-CUSTOMER-RECORD`. The previous
// `tok == r.from` test only matched a token that equalled the pattern exactly, so
// hyphenated names like `PREFIX-RECORD`/`PREFIX-ID` were never rewritten and the
// REPLACING clause appeared to do nothing. We now apply strings.ReplaceAll per
// token. Longer patterns are applied first so overlapping rules are deterministic.
func applyReplacing(l *cobolLine, repls []copyReplacement) {
	if len(repls) == 0 {
		return
	}
	ordered := make([]copyReplacement, len(repls))
	copy(ordered, repls)
	sort.SliceStable(ordered, func(i, j int) bool {
		return len(ordered[i].from) > len(ordered[j].from)
	})
	for i, tok := range l.tokens {
		for _, r := range ordered {
			if r.from == "" {
				continue
			}
			tok = strings.ReplaceAll(tok, r.from, r.to)
		}
		l.tokens[i] = tok
	}
	l.upper = strings.Join(l.tokens, " ")
	l.text = l.upper
}

// ─── Top-level parser ─────────────────────────────────────────

// parseFile is the top-level entry: scans for IDENTIFICATION, ENVIRONMENT,
// DATA, and PROCEDURE divisions and emits nodes accordingly.
func (p *cobolParser) parseFile(m *Module) {
	for p.pos < len(p.lines) {
		l := p.peek()
		if l == nil {
			break
		}
		tok0 := l.tokens[0]
		switch {
		case tok0 == "IDENTIFICATION" || tok0 == "ID":
			p.parseIdentificationDivision(m)
		case tok0 == "ENVIRONMENT":
			p.parseEnvironmentDivision(m)
		case tok0 == "DATA":
			p.parseDataDivision(m)
		case tok0 == "PROCEDURE":
			p.parseProcedureDivision(m)
		case isEndProgram(l):
			p.endProgram(l.lineNo)
			p.consume()
		default:
			p.consume()
		}
	}
	p.endProgram(p.lastLine())
	// Attach any unresolved/cycled COPY records to the module.
	m.Nodes = append(m.Nodes, p.pendingUnresolvedCopies...)
}

// endProgram closes the open program's span at line, if a program is open.
func (p *cobolParser) endProgram(line int) {
	if p.program != nil {
		if line > p.program.Span.End.Line {
			p.program.Span.End = Pos{Line: line}
		}
		p.program = nil
	}
}

// programName is the name paragraphs and sections carry: the PROGRAM-ID in a
// file with several programs, else "".
func (p *cobolParser) programName() string {
	if p.multiProgram && p.program != nil {
		return p.program.Name
	}
	return ""
}

// parseIdentificationDivision extracts PROGRAM-ID.
func (p *cobolParser) parseIdentificationDivision(m *Module) {
	startLine := p.peek().lineNo
	p.consume() // IDENTIFICATION DIVISION
	for p.pos < len(p.lines) {
		l := p.peek()
		if l == nil {
			break
		}
		if isDivision(l) {
			break
		}
		if l.tokens[0] == "PROGRAM-ID" || (len(l.tokens) >= 1 && strings.HasPrefix(l.upper, "PROGRAM-ID")) {
			name := ""
			if len(l.tokens) >= 2 {
				raw := l.tokens[1]
				raw = strings.TrimSuffix(raw, ".")
				name = raw
			} else if p.pos+1 < len(p.lines) {
				next := p.lines[p.pos+1]
				if len(next.tokens) > 0 {
					name = strings.TrimSuffix(next.tokens[0], ".")
				}
			}
			n := &Node{
				ID:       "module:" + name,
				Kind:     NodeModule,
				Name:     name,
				Language: LangCOBOL,
				Span:     Span{Start: Pos{Line: startLine}, End: Pos{Line: l.lineNo}},
				Exported: true,
			}
			if p.multiProgram {
				n.Program = name
			}
			p.endProgram(startLine - 1)
			p.program = n
			m.Nodes = append(m.Nodes, n)
		}
		p.consume()
	}
}

// ─── Environment Division: FILE-CONTROL / SELECT ──────────────

// parseEnvironmentDivision handles FILE-CONTROL paragraph specifically.
// SELECT ... ASSIGN TO ... entries become NodeImport with Path=assignment.
func (p *cobolParser) parseEnvironmentDivision(m *Module) {
	p.consume() // ENVIRONMENT DIVISION.
	for p.pos < len(p.lines) {
		l := p.peek()
		if l == nil || isDivision(l) {
			break
		}
		// CONFIGURATION SECTION / INPUT-OUTPUT SECTION: skip header line, recurse.
		if isSection(l) {
			p.consume()
			continue
		}
		// FILE-CONTROL paragraph header
		if l.tokens[0] == "FILE-CONTROL" || l.tokens[0] == "FILE-CONTROL." {
			p.consume()
			p.parseFileControl(m)
			continue
		}
		p.consume()
	}
}

// parseFileControl reads SELECT ... ASSIGN TO ... statements.
//
//	SELECT CUSTOMER-FILE ASSIGN TO DD-CUST
//	    ORGANIZATION IS INDEXED
//	    ACCESS MODE IS DYNAMIC
//	    RECORD KEY IS CUST-ID
//	    FILE STATUS IS WS-CUST-STATUS.
func (p *cobolParser) parseFileControl(m *Module) {
	for p.pos < len(p.lines) {
		l := p.peek()
		if l == nil || isDivision(l) {
			break
		}
		// New section starts → exit
		if isSection(l) || l.tokens[0] == "DATA" {
			break
		}
		if l.tokens[0] == "SELECT" && len(l.tokens) >= 2 {
			fileName := strings.TrimSuffix(l.tokens[1], ".")
			assignTo := ""
			organization := ""
			accessMode := ""
			recordKey := ""

			// Scan tokens of this SELECT statement (may continue across lines
			// until next "SELECT" or section/division boundary).
			combined := l.upper
			startLine := l.lineNo
			p.consume()
			for p.pos < len(p.lines) {
				next := p.peek()
				if next == nil || isDivision(next) || isSection(next) {
					break
				}
				if next.tokens[0] == "SELECT" || next.tokens[0] == "FILE-CONTROL" {
					break
				}
				combined += " " + next.upper
				if strings.HasSuffix(strings.TrimSpace(next.upper), ".") {
					p.consume()
					break
				}
				p.consume()
			}

			toks := strings.Fields(combined)
			for i, t := range toks {
				switch t {
				case "ASSIGN":
					// "ASSIGN TO <name>" or "ASSIGN <name>"
					if i+2 < len(toks) && toks[i+1] == "TO" {
						assignTo = strings.TrimSuffix(toks[i+2], ".")
					} else if i+1 < len(toks) {
						assignTo = strings.TrimSuffix(toks[i+1], ".")
					}
				case "ORGANIZATION":
					if i+2 < len(toks) && toks[i+1] == "IS" {
						organization = strings.TrimSuffix(toks[i+2], ".")
					} else if i+1 < len(toks) {
						organization = strings.TrimSuffix(toks[i+1], ".")
					}
				case "ACCESS":
					if i+3 < len(toks) && toks[i+1] == "MODE" && toks[i+2] == "IS" {
						accessMode = strings.TrimSuffix(toks[i+3], ".")
					}
				case "RECORD":
					if i+3 < len(toks) && toks[i+1] == "KEY" && toks[i+2] == "IS" {
						recordKey = strings.TrimSuffix(toks[i+3], ".")
					}
				}
			}

			tag := encodeTag(map[string]string{
				"assign":       assignTo,
				"organization": organization,
				"access":       accessMode,
				"record_key":   recordKey,
			})

			m.Nodes = append(m.Nodes, &Node{
				ID:         "file:" + fileName,
				Kind:       NodeImport,
				Name:       fileName,
				ImportPath: assignTo,
				Language:   LangCOBOL,
				Span:       Span{Start: Pos{Line: startLine}},
				Exported:   true,
				RawSource:  tag,
			})
			continue
		}
		p.consume()
	}
}

// ─── Data Division ───────────────────────────────────────────

// parseDataDivision extracts 01-level record definitions and FD entries.
func (p *cobolParser) parseDataDivision(m *Module) {
	p.consume() // DATA DIVISION.
	currentSection := ""
	for p.pos < len(p.lines) {
		l := p.peek()
		if l == nil || isDivision(l) {
			break
		}
		if isSection(l) {
			currentSection = strings.TrimSuffix(l.tokens[0], ".")
			p.consume()
			continue
		}
		// FD <file-name>: file descriptor entry.
		if l.tokens[0] == "FD" && len(l.tokens) >= 2 {
			p.parseFD(m)
			continue
		}
		// 01-level group item.
		if len(l.tokens) >= 2 && l.tokens[0] == "01" {
			p.parse01Level(m, currentSection)
			continue
		}
		p.consume()
	}
}

// parseFD reads an FD entry (File Descriptor in FILE SECTION).
//
//	FD CUSTOMER-FILE
//	    LABEL RECORDS ARE STANDARD
//	    BLOCK CONTAINS 0 RECORDS.
//	01 CUSTOMER-RECORD.
//	   05 CUST-ID PIC 9(6).
//
// The FD entry binds to the next 01 record. Here we emit a NodeStruct for
// the FD itself with RawSource containing "fd=true" so consumers can
// distinguish it from a plain 01-level group.
func (p *cobolParser) parseFD(m *Module) {
	l := p.consume()
	if len(l.tokens) < 2 {
		return
	}
	fileName := strings.TrimSuffix(l.tokens[1], ".")
	n := &Node{
		ID:        "fd:" + fileName,
		Kind:      NodeStruct,
		Name:      fileName,
		Language:  LangCOBOL,
		Span:      Span{Start: Pos{Line: l.lineNo}},
		Exported:  true,
		RawSource: "fd=true",
	}
	// Skip clauses on this FD until we hit the next 01 or section.
	for p.pos < len(p.lines) {
		next := p.peek()
		if next == nil {
			break
		}
		if len(next.tokens) >= 1 && next.tokens[0] == "01" {
			break
		}
		if isDivision(next) || isSection(next) {
			break
		}
		p.consume()
	}
	n.Span.End = Pos{Line: p.lastLine()}
	m.Nodes = append(m.Nodes, n)
}

// parse01Level reads an 01-level group item as NodeStruct with full
// child-field metadata: PIC, USAGE, REDEFINES, OCCURS, VALUE.
func (p *cobolParser) parse01Level(m *Module, sectionName string) {
	l := p.consume()
	if len(l.tokens) < 2 {
		return
	}
	name := strings.TrimSuffix(l.tokens[1], ".")
	n := &Node{
		ID:       "struct:" + name,
		Kind:     NodeStruct,
		Name:     name,
		Language: LangCOBOL,
		Span:     Span{Start: Pos{Line: l.lineNo}},
		Exported: isExportedCOBOL(name),
	}
	if sectionName != "" {
		n.RawSource = "section=" + sectionName
	}

	// Collect subordinate levels (02-49, 66 for RENAMES, 88 for condition names).
	for p.pos < len(p.lines) {
		sub := p.peek()
		if sub == nil || isDivision(sub) || isSection(sub) {
			break
		}
		if len(sub.tokens) < 1 {
			p.consume()
			continue
		}
		lvl := parseLevel(sub.tokens[0])
		if lvl == 1 || (lvl == 77 && p.pos != 0) {
			break
		}
		// 88-level condition names are folded into the parent as
		// fields of type "cond" so the consumer can see them.
		if lvl == 88 && len(sub.tokens) >= 2 {
			condName := strings.TrimSuffix(sub.tokens[1], ".")
			n.Fields = append(n.Fields, Field{
				Name:     condName,
				Type:     TypeRef{Name: "cond"},
				Exported: isExportedCOBOL(condName),
				Tag:      "level=88",
				Line:     sub.lineNo,
			})
			p.consume()
			continue
		}
		if lvl >= 2 && lvl <= 49 && len(sub.tokens) >= 2 {
			fieldName := strings.TrimSuffix(sub.tokens[1], ".")
			// A copybook line carries the line of its COPY statement, so a
			// copybook field points at the COPY in the program.
			fieldLine := sub.lineNo

			// COBOL field clauses can span multiple lines until a "."
			// terminator is encountered. Coalesce so OCCURS/PIC/USAGE/etc.
			// at any line of the clause are visible to the extractors.
			clauseText := sub.upper
			endLine := sub.lineNo
			endsClause := strings.HasSuffix(strings.TrimRight(sub.text, " \t"), ".")
			p.consume()
			for !endsClause && p.pos < len(p.lines) {
				next := p.peek()
				if next == nil {
					break
				}
				// If next line is itself a level number, this clause ended without a period.
				if len(next.tokens) > 0 && parseLevel(next.tokens[0]) > 0 {
					break
				}
				if isDivision(next) || isSection(next) {
					break
				}
				clauseText += " " + next.upper
				endLine = next.lineNo
				endsClause = strings.HasSuffix(strings.TrimRight(next.text, " \t"), ".")
				p.consume()
			}
			_ = endLine

			pic := extractPIC(clauseText)
			usage := extractUsage(clauseText)
			occurs, occursDep := extractOccurs(clauseText)
			redefines := extractRedefines(clauseText)
			value := extractValue(clauseText)

			tagMap := map[string]string{
				"level": fmt.Sprintf("%02d", lvl),
			}
			if usage != "" {
				tagMap["usage"] = usage
			}
			if occurs > 0 {
				tagMap["occurs"] = fmt.Sprintf("%d", occurs)
			}
			if occursDep != "" {
				tagMap["occurs_depending_on"] = occursDep
			}
			if redefines != "" {
				tagMap["redefines"] = redefines
			}
			if value != "" {
				tagMap["value"] = value
			}

			n.Fields = append(n.Fields, Field{
				Name:     fieldName,
				Type:     TypeRef{Name: pic},
				Exported: isExportedCOBOL(fieldName),
				Tag:      encodeTag(tagMap),
				Line:     fieldLine,
			})
			continue
		}
		p.consume()
	}
	n.Span.End = Pos{Line: p.lastLine()}
	m.Nodes = append(m.Nodes, n)
}

// ─── Procedure Division ──────────────────────────────────────

// parseProcedureDivision parses sections and paragraphs as NodeFunction/NodeProcedure,
// and EXEC CICS / EVALUATE / IF blocks as appropriate children.
func (p *cobolParser) parseProcedureDivision(m *Module) {
	p.consume() // PROCEDURE DIVISION [USING ...].

	var current *Node // current paragraph/section node

	// v23.46: IF body nesting. COBOL previously emitted a flat NodeBranch with
	// the then-body as siblings, so the CFG built empty then-blocks and pushed
	// the real statements into the merge/exit (the "no End on branch / condition
	// on wrong block" bugs). We now track an IF scope stack: statements between
	// IF [THEN] and its matching END-IF (or scope-terminating period) become
	// children of the branch (or its Else after an ELSE). `tgt()` is the current
	// append target: the innermost open IF branch, else the paragraph.
	type ifScope struct {
		branch    *Node // the NodeBranch (nil for a loop scope)
		loop      *Node // v24: the NodeLoop for an inline PERFORM UNTIL … END-PERFORM
		inElse    bool  // statements now go into Else, not Children
		endsAtDot bool  // period-terminated (no END-IF) → pop on next period
		eval      *Node // v26.58 (D064): the EVALUATE root for an EVALUATE scope
		when      *Node // v26.58 (D064): the open WHEN of that EVALUATE, if any
	}
	var scopes []*ifScope
	// innermostEval returns the index of the innermost open EVALUATE scope, or -1.
	innermostEval := func() int {
		for i := len(scopes) - 1; i >= 0; i-- {
			if scopes[i].eval != nil {
				return i
			}
		}
		return -1
	}
	// add() appends a child to the right place: an open loop's Children, an open
	// IF's Else list when in the else arm, otherwise the branch Children; falls
	// back to the paragraph/module when no scope is open (mirrors appendChild).
	add := func(child *Node) {
		if len(scopes) > 0 {
			s := scopes[len(scopes)-1]
			if s.eval != nil {
				host := s.when
				if host == nil {
					host = s.eval
				}
				host.Children = append(host.Children, child)
				return
			}
			if s.loop != nil {
				s.loop.Children = append(s.loop.Children, child)
				return
			}
			if s.inElse {
				s.branch.Else = append(s.branch.Else, child)
			} else {
				s.branch.Children = append(s.branch.Children, child)
			}
			return
		}
		// v26.69: a statement before the first paragraph belongs to the program
		// itself. It used to land at file level with no owner, so its calls
		// had no caller.
		if current == nil && p.program != nil {
			p.program.Children = append(p.program.Children, child)
			return
		}
		appendChild(current, m, child)
	}
	popDotScopes := func() {
		// a scope-terminating period closes every period-style IF still open
		for len(scopes) > 0 && scopes[len(scopes)-1].endsAtDot {
			scopes = scopes[:len(scopes)-1]
		}
	}
	// v26.58 (D062): only some branches checked for a closing period, so an IF
	// stayed open past "PERFORM P UNTIL C.", "END-IF." or "END-EXEC.". A period
	// ends the sentence after any statement, so every branch that consumes a
	// statement calls this on the last line it consumed.
	endSentence := func() {
		if p.pos > 0 && strings.HasSuffix(strings.TrimRight(p.lines[p.pos-1].raw, " \t\r\n"), ".") {
			popDotScopes()
		}
	}
	clearScopes := func() { scopes = scopes[:0] }

	flush := func() {
		// an EVALUATE left open (no END-EVALUATE) ends with its paragraph
		for _, s := range scopes {
			if s.eval != nil {
				s.eval.Span.End = Pos{Line: p.lastLine()}
			}
		}
		clearScopes()
		if current != nil {
			current.Span.End = Pos{Line: p.lastLine()}
			m.Nodes = append(m.Nodes, current)
			current = nil
		}
	}

	for p.pos < len(p.lines) {
		l := p.peek()
		if l == nil {
			break
		}
		if isDivision(l) {
			flush()
			break
		}
		// v26.69: END PROGRAM closes the paragraph and the program; the next
		// program of the file starts with its own IDENTIFICATION DIVISION.
		if isEndProgram(l) {
			flush()
			p.endProgram(l.lineNo)
			p.consume()
			break
		}

		// SECTION header
		if isSection(l) {
			flush()
			name := strings.TrimSuffix(l.tokens[0], ".")
			current = &Node{
				ID:       "procedure:" + name,
				Kind:     NodeProcedure,
				Name:     name,
				Language: LangCOBOL,
				Span:     Span{Start: Pos{Line: l.lineNo}},
				Exported: isExportedCOBOL(name),
				Program:  p.programName(),
			}
			p.consume()
			continue
		}
		// Paragraph header
		if isParagraphHeader(l) {
			flush()
			name := strings.TrimSuffix(l.tokens[0], ".")
			current = &Node{
				ID:       "function:" + name,
				Kind:     NodeFunction,
				Name:     name,
				Language: LangCOBOL,
				Span:     Span{Start: Pos{Line: l.lineNo}},
				Exported: isExportedCOBOL(name),
				Program:  p.programName(),
			}
			p.consume()
			continue
		}

		// EVALUATE block → NodeBranch with one NodeBranch child per WHEN clause
		// (named "WHEN" or "WHEN_OTHER").
		// v26.58 (D061): parseEvaluate attached the block to the paragraph, so an
		// EVALUATE inside an open IF or inline PERFORM became a sibling after it.
		// add() puts it in the innermost open scope.
		// v26.58 (D064): parseEvaluate ended at the first END-EVALUATE, even a
		// nested one, and kept only CALL, PERFORM, EXEC CICS and STOP under a
		// WHEN, so MOVE, IF, EXEC SQL and nested EVALUATE left no node or the
		// wrong one. EVALUATE is now a scope on the same stack as IF and loops:
		// the statements under a WHEN go through this loop like a paragraph body,
		// and each END-EVALUATE closes its own EVALUATE.
		if l.tokens[0] == "EVALUATE" {
			// v23.41: the selector (e.g. "TRUE", "WS-CODE") labels the decision.
			root := &Node{
				Kind:      NodeBranch,
				Name:      "EVALUATE",
				Language:  LangCOBOL,
				RawSource: strings.Join(l.tokens[1:], " "),
				Span:      Span{Start: Pos{Line: l.lineNo}},
			}
			add(root)
			scopes = append(scopes, &ifScope{eval: root})
			p.consume()
			continue
		}
		// WHEN → a new arm of the innermost EVALUATE. It closes any scope still
		// open inside the previous arm.
		if l.tokens[0] == "WHEN" {
			if i := innermostEval(); i >= 0 {
				scopes = scopes[:i+1]
				s := scopes[i]
				name := "WHEN"
				// v23.41: the WHEN value (e.g. "TXN-DEBIT") becomes the decision text.
				whenVal := strings.Join(l.tokens[1:], " ")
				if len(l.tokens) >= 2 && l.tokens[1] == "OTHER" {
					name = "WHEN_OTHER"
					whenVal = "OTHER"
				}
				s.when = &Node{
					Kind:      NodeBranch,
					Name:      name,
					Language:  LangCOBOL,
					RawSource: whenVal,
					Span:      Span{Start: Pos{Line: l.lineNo}},
				}
				s.eval.Children = append(s.eval.Children, s.when)
				p.consume()
				continue
			}
		}
		// END-EVALUATE → close the innermost EVALUATE and anything open in it.
		if l.tokens[0] == "END-EVALUATE" || l.tokens[0] == "END-EVALUATE." {
			if i := innermostEval(); i >= 0 {
				scopes[i].eval.Span.End = Pos{Line: l.lineNo}
				scopes = scopes[:i]
			}
			p.consume()
			endSentence()
			continue
		}

		// CALL <name> → NodeCall
		if l.tokens[0] == "CALL" && len(l.tokens) >= 2 {
			callee := strings.TrimRight(l.tokens[1], ".")
			callee = strings.Trim(callee, `"'`)
			add(&Node{
				Kind:     NodeCall,
				Callee:   callee,
				Language: LangCOBOL,
				Span:     Span{Start: Pos{Line: l.lineNo}},
			})
			if strings.HasSuffix(strings.TrimSpace(l.raw), ".") {
				popDotScopes()
			}
			p.consume()
			continue
		}

		// FUNCTION <name>(...) → NodeCall (v23.33). Intrinsic-function uses
		// appear anywhere in a statement (COMPUTE, MOVE, IF, …), not as their
		// own verb, so we scan all the tokens of the line, not only the
		// first. Each "FUNCTION" token is followed by the intrinsic name. This
		// makes intrinsic usage visible in the call graph and resolvable
		// against the synthetic COBOL runtime catalog. This code does not
		// consume the statement. It still falls through to whatever verb owns the line.
		for ti := 0; ti+1 < len(l.tokens); ti++ {
			// v26.69: "(FUNCTION RANDOM" and "-(FUNCTION SIN(X))" open with an
			// operator or a parenthesis, so the token was not "FUNCTION".
			if strings.TrimLeft(l.tokens[ti], "(+-*/") == "FUNCTION" {
				fn := strings.TrimRight(l.tokens[ti+1], ".")
				fn = strings.Trim(fn, `"'`)
				// The argument list often tokenises onto the name, e.g.
				// "NUMVAL(WS-RAW)" or "MAX(WS-AMT,". Keep only the identifier
				// up to the first '(' so it resolves to the intrinsic name.
				if lp := strings.IndexByte(fn, '('); lp >= 0 {
					fn = fn[:lp]
				}
				// v26.69: "(FUNCTION RANDOM)" kept the closing parenthesis.
				fn = strings.TrimRight(fn, ",)")
				if fn != "" {
					add(&Node{
						Kind:     NodeCall,
						Callee:   fn,
						Language: LangCOBOL,
						Span:     Span{Start: Pos{Line: l.lineNo}},
					})
				}
			}
		}

		// EXEC CICS / EXEC SQL block → NodeCall (v23.47: via add() so it nests in
		// an open IF's then/else body instead of collapsing onto the paragraph).
		if l.tokens[0] == "EXEC" && len(l.tokens) >= 2 {
			switch l.tokens[1] {
			case "CICS":
				add(p.parseCICSBlock(m, l.lineNo))
				endSentence()
				continue
			case "SQL":
				add(p.parseSQLBlock(m, l.lineNo))
				endSentence()
				continue
			}
		}

		// PERFORM → NodeCall (paragraph invocation) and/or NodeLoop
		if l.tokens[0] == "PERFORM" && len(l.tokens) >= 2 {
			target := strings.TrimSuffix(l.tokens[1], ".")
			isLoop := false
			for _, tok := range l.tokens {
				if tok == "UNTIL" || tok == "VARYING" || tok == "TIMES" {
					isLoop = true
					break
				}
			}
			// v23.47: build the loop node with the performed paragraph as its
			// body, so the CFG shows the action inside the loop instead of an
			// empty "until …" diamond. `PERFORM <para> UNTIL <cond>` = loop over a
			// call; `PERFORM UNTIL <cond> … END-PERFORM` (inline body) keeps the
			// scope-stack path below via the loop's own scope (future work).
			// v26.69: "PERFORM FUNCTION MIN(A, B) TIMES" took FUNCTION as the
			// paragraph; an intrinsic is the loop count.
			isParaCall := target != "UNTIL" && target != "VARYING" &&
				target != "THROUGH" && target != "THRU" && target != "TIMES" && target != "FUNCTION"
			// v26.58 (D063): "PERFORM 5 TIMES" (or "PERFORM WS-N TIMES") took the
			// count as a paragraph name, pushed no loop scope, and left the body
			// outside the loop; its END-PERFORM then closed the enclosing IF. With
			// TIMES as the third token there is no paragraph: it is an inline loop.
			if len(l.tokens) >= 3 && strings.TrimSuffix(l.tokens[2], ".") == "TIMES" {
				isParaCall = false
			}
			// v26.58 (F6): "PERFORM WITH TEST AFTER UNTIL C" took WITH as the
			// paragraph, so the inline body landed outside the loop. WITH TEST
			// BEFORE or AFTER (WITH is optional) is a phrase of the loop: with it
			// right after PERFORM there is no paragraph, it is an inline loop.
			// After a paragraph name the phrase is already harmless.
			ti := 1
			if strings.TrimSuffix(l.tokens[ti], ".") == "WITH" && len(l.tokens) > 2 {
				ti = 2
			}
			if l.tokens[ti] == "TEST" && ti+1 < len(l.tokens) {
				if w := strings.TrimSuffix(l.tokens[ti+1], "."); w == "BEFORE" || w == "AFTER" {
					isParaCall = false
				}
			}
			if isLoop {
				loopCond := ""
				for i, tok := range l.tokens {
					if tok == "UNTIL" && i+1 < len(l.tokens) {
						loopCond = "until " + strings.Join(l.tokens[i+1:], " ")
						break
					}
					if tok == "VARYING" && i+1 < len(l.tokens) {
						loopCond = "varying " + strings.Join(l.tokens[i+1:], " ")
						break
					}
					if tok == "TIMES" && i > 0 {
						loopCond = l.tokens[i-1] + " times"
						break
					}
				}
				loop := &Node{
					Kind:      NodeLoop,
					Language:  LangCOBOL,
					RawSource: strings.TrimSuffix(loopCond, "."),
					Span:      Span{Start: Pos{Line: l.lineNo}},
				}
				// PERFORM <para> UNTIL <cond>: the call is the loop body.
				if isParaCall {
					loop.Children = append(loop.Children, &Node{
						Kind:     NodeCall,
						Callee:   target,
						Language: LangCOBOL,
						Span:     Span{Start: Pos{Line: l.lineNo}},
					})
					add(loop)
				} else {
					// v24: inline `PERFORM UNTIL <cond> … END-PERFORM` (no paragraph
					// call). Add the loop, then push it as an open scope so the
					// statements up to END-PERFORM nest as its body.
					add(loop)
					scopes = append(scopes, &ifScope{loop: loop})
				}
				p.consume()
				endSentence()
				continue
			} else if isParaCall {
				// plain PERFORM <para> (no loop) → a call
				add(&Node{
					Kind:     NodeCall,
					Callee:   target,
					Language: LangCOBOL,
					Span:     Span{Start: Pos{Line: l.lineNo}},
				})
			}
		}

		// STOP RUN / GOBACK / EXIT PROGRAM → NodeReturn
		if l.tokens[0] == "STOP" || l.tokens[0] == "GOBACK" ||
			(l.tokens[0] == "EXIT" && len(l.tokens) >= 2 && l.tokens[1] == "PROGRAM") {
			add(&Node{
				Kind:     NodeReturn,
				Language: LangCOBOL,
				Span:     Span{Start: Pos{Line: l.lineNo}},
			})
		}

		// IF → NodeBranch. v23.39: condition text in RawSource. v23.46: push an
		// IF scope so the then-body nests as children (not flat siblings). The
		// scope is closed by END-IF, or by a scope-terminating period for the
		// period-style IF. We attach the branch to the current target first, then
		// push it, so nested IFs stack correctly.
		if l.tokens[0] == "IF" {
			// v26.58 (D062): "IF A = 1 MOVE 1 TO X." kept the statement in the
			// condition text and gave it no node. Split the line into the IF and
			// its statement parts first, so each part is parsed on its own.
			p.splitInlineIF()
			l = p.peek()
			cond := strings.Join(l.tokens[1:], " ")
			cond = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(cond), "THEN"))
			// strip a trailing period (or the comma of "IF A = 1,") from the condition
			cond = strings.TrimSpace(strings.TrimSuffix(cond, "."))
			cond = strings.TrimSpace(strings.TrimSuffix(cond, ","))
			br := &Node{
				Kind:      NodeBranch,
				Language:  LangCOBOL,
				Name:      "IF",
				RawSource: cond,
				Span:      Span{Start: Pos{Line: l.lineNo}},
			}
			add(br) // attach to enclosing scope/paragraph before pushing
			// v26.58 (D062): endsAtDot was set only when the IF line itself ended
			// with a period, so "IF A = 1," followed by "MOVE 2 TO Y." stayed open
			// to the paragraph end. COBOL closes an IF at END-IF or at the first
			// period, whichever comes first, so every IF scope closes at a period.
			scopes = append(scopes, &ifScope{branch: br, endsAtDot: true})
			p.consume()
			continue
		}

		// ELSE → switch the innermost IF scope to its Else arm.
		if l.tokens[0] == "ELSE" || l.tokens[0] == "ELSE." {
			// v26.58 (F5): "ELSE CALL 'SUB-ELSE'" consumed the whole line, so the
			// statement after ELSE left no node and an END-IF on the line did not
			// close the IF. Split the line as the one line IF split does: ELSE,
			// the statement and each END-IF become their own lines, and the loop
			// parses the statement as the first one of the else arm.
			p.splitElseLine()
			if len(scopes) > 0 {
				scopes[len(scopes)-1].inElse = true
			}
			p.consume()
			continue
		}

		// END-IF → pop the innermost IF scope.
		if l.tokens[0] == "END-IF" || l.tokens[0] == "END-IF." {
			if len(scopes) > 0 {
				scopes = scopes[:len(scopes)-1]
			}
			p.consume()
			endSentence()
			continue
		}

		// v24: END-PERFORM → pop the innermost inline-PERFORM loop scope, so the
		// statements between `PERFORM UNTIL …` and END-PERFORM nested as the loop
		// body and control returns to the enclosing scope.
		if l.tokens[0] == "END-PERFORM" || l.tokens[0] == "END-PERFORM." {
			for len(scopes) > 0 {
				top := scopes[len(scopes)-1]
				scopes = scopes[:len(scopes)-1]
				if top.loop != nil {
					break // popped the loop scope (and any stray inner scopes)
				}
			}
			p.consume()
			endSentence()
			continue
		}

		// v23.41: data / arithmetic statements (SUBTRACT, ADD, MOVE, COMPUTE, …)
		// previously produced no node, so they were invisible in the CFG (the
		// reported "missing SUBTRACT"). Emit a NodeAssign carrying the full
		// statement text in RawSource so it renders as a process step. Guarded to
		// the verbs below and only when the line wasn't already consumed as a
		// CALL/PERFORM/IF/EXEC/return above (those set tokens[0] to their verb, so
		// a simple first-token check is sufficient and non-overlapping).
		if cobolDataVerbs[l.tokens[0]] || cobolFileVerbs[l.tokens[0]] {
			add(&Node{
				Kind:      NodeAssign,
				Language:  LangCOBOL,
				Name:      l.tokens[0],
				RawSource: strings.Join(l.tokens, " "),
				Span:      Span{Start: Pos{Line: l.lineNo}},
			})
			// A period after a statement closes any period-style IF scopes.
			if strings.HasSuffix(strings.TrimRight(l.raw, " \t\r\n"), ".") {
				popDotScopes()
			}
		}

		// v23.46: a statement line ending in a period closes any period-style IF
		// scopes still open (covers PERFORM and other fall-through statements that
		// didn't `continue` above). END-IF-style scopes are unaffected.
		if len(scopes) > 0 && strings.HasSuffix(strings.TrimRight(l.raw, " \t\r\n"), ".") {
			popDotScopes()
		}

		p.consume()
	}
	flush()
}

// cobolStatementVerbs are words that start a statement, beside cobolDataVerbs.
// splitInlineIF uses both to find where the condition of a one-line IF ends.
var cobolStatementVerbs = map[string]bool{
	"CALL": true, "PERFORM": true, "EXEC": true, "EVALUATE": true, "IF": true,
	"GO": true, "GOBACK": true, "STOP": true, "EXIT": true, "CONTINUE": true,
	"NEXT": true, "OPEN": true, "CLOSE": true, "READ": true, "WRITE": true,
	"REWRITE": true, "DELETE": true,
}

// splitInlineIF splits the IF line at p.pos when it carries statements, as in
// "IF A = 1 MOVE 1 TO X." or "IF A = 1 THEN MOVE 1 TO X ELSE MOVE 2 TO X END-IF".
// The IF keeps the condition. Each statement, ELSE and END-IF becomes its own
// line with the same line number, so the main loop parses it like any other
// line (a nested IF in a part is split again when the loop reaches it). The
// condition ends at THEN or at the first statement verb outside a literal.
func (p *cobolParser) splitInlineIF() {
	l := p.lines[p.pos]
	var cuts []int // token indexes where a new line starts
	cut := func(i int) {
		if i < len(l.tokens) && (len(cuts) == 0 || cuts[len(cuts)-1] < i) {
			cuts = append(cuts, i)
		}
	}
	quote := byte(0)
	for i := 1; i < len(l.tokens); i++ {
		tok := l.tokens[i]
		if quote == 0 {
			switch {
			case len(cuts) == 0 && tok == "THEN":
				cut(i + 1)
			case len(cuts) == 0 && i >= 2 && (cobolStatementVerbs[tok] || cobolDataVerbs[tok]):
				cut(i)
			case len(cuts) > 0 && (tok == "ELSE" || tok == "END-IF" || tok == "END-IF."):
				cut(i)
				cut(i + 1)
			}
		}
		for j := 0; j < len(tok); j++ {
			c := tok[j]
			if quote == 0 && (c == '\'' || c == '"') {
				quote = c
			} else if c == quote {
				quote = 0
			}
		}
	}
	p.splitLineAt(cuts)
}

// splitElseLine splits the ELSE line at p.pos when it carries a statement, as
// in "ELSE CALL 'SUB-ELSE'" or "ELSE MOVE 2 TO X END-IF". ELSE, the statement
// and each END-IF outside a literal become their own lines with the same line
// number. A nested IF after ELSE is split again when the loop reaches it.
// v26.58 (F5).
func (p *cobolParser) splitElseLine() {
	l := p.lines[p.pos]
	if len(l.tokens) < 2 || l.tokens[0] != "ELSE" {
		return
	}
	cuts := []int{1}
	quote := byte(0)
	for i := 1; i < len(l.tokens); i++ {
		tok := l.tokens[i]
		if quote == 0 && i >= 2 && (tok == "END-IF" || tok == "END-IF.") {
			if cuts[len(cuts)-1] < i {
				cuts = append(cuts, i)
			}
			if i+1 < len(l.tokens) {
				cuts = append(cuts, i+1)
			}
		}
		for j := 0; j < len(tok); j++ {
			c := tok[j]
			if quote == 0 && (c == '\'' || c == '"') {
				quote = c
			} else if c == quote {
				quote = 0
			}
		}
	}
	p.splitLineAt(cuts)
}

// splitLineAt replaces the line at p.pos with its parts, cut before each token
// index in cuts (ascending, each above 0 and below the token count). Each part
// keeps the line number. Shared by splitInlineIF and splitElseLine.
func (p *cobolParser) splitLineAt(cuts []int) {
	if len(cuts) == 0 {
		return
	}
	l := p.lines[p.pos]
	words := strings.Fields(l.text)
	if len(words) != len(l.tokens) {
		words = l.tokens
	}
	bounds := append(append([]int{0}, cuts...), len(l.tokens))
	parts := make([]cobolLine, 0, len(bounds)-1)
	for k := 0; k+1 < len(bounds); k++ {
		lo, hi := bounds[k], bounds[k+1]
		text := strings.Join(words[lo:hi], " ")
		toks := append([]string(nil), l.tokens[lo:hi]...)
		parts = append(parts, cobolLine{
			lineNo: l.lineNo,
			raw:    text,
			text:   text,
			upper:  strings.Join(toks, " "),
			tokens: toks,
		})
	}
	rest := append(parts, p.lines[p.pos+1:]...)
	p.lines = append(p.lines[:p.pos], rest...)
}

// cobolDataVerbs are COBOL statements that mutate data (not control flow, not
// calls). v23.41: emitted as NodeAssign so they show as CFG process steps.
var cobolDataVerbs = map[string]bool{
	"MOVE": true, "ADD": true, "SUBTRACT": true, "MULTIPLY": true,
	"DIVIDE": true, "COMPUTE": true, "DISPLAY": true, "ACCEPT": true,
	"INITIALIZE": true, "SET": true, "STRING": true, "UNSTRING": true,
	"INSPECT": true, "ADDRESS": false,
}

// cobolFileVerbs are the file I/O statements. v26.66: they produced no node,
// so a paragraph that only read or wrote files showed no step for it in the
// CFG and no effect. They are emitted like the data verbs, as NodeAssign with
// the verb in Name; flow.InferEffects reads them as file effects.
var cobolFileVerbs = map[string]bool{
	"OPEN": true, "CLOSE": true, "READ": true, "WRITE": true,
	"REWRITE": true, "DELETE": true, "START": true,
}

// appendChild attaches a child node to current; if current is nil it goes
// to the module nodes directly so we never lose information.
func appendChild(current *Node, m *Module, child *Node) {
	if current != nil {
		current.Children = append(current.Children, child)
	} else {
		m.Nodes = append(m.Nodes, child)
	}
}

// parseCICSBlock parses EXEC CICS … END-EXEC as a single NodeCall and returns
// it (v23.47) so the caller can attach it to the correct scope (an open IF's
// then/else body, not always the paragraph; this was the "IF/ELSE collapse" bug).
func (p *cobolParser) parseCICSBlock(m *Module, startLineNo int) *Node {
	startLine := p.consume()
	command := ""
	if len(startLine.tokens) >= 3 {
		command = startLine.tokens[2]
	}
	var rawParts []string
	rawParts = append(rawParts, startLine.raw)

	// v23.9 (Phase 6): single-line `EXEC CICS <verb> ... END-EXEC` is complete
	// on the start line. Previously the continuation loop below always ran and
	// consumed the following line while hunting for END-EXEC, swallowing every
	// other CICS statement (WRITE kept, REWRITE eaten, DELETE kept, LINK eaten…).
	// Only scan continuation lines when END-EXEC is not already on the start line.
	if !strings.Contains(startLine.upper, "END-EXEC") {
		for p.pos < len(p.lines) {
			l := p.peek()
			if l == nil {
				break
			}
			rawParts = append(rawParts, l.raw)
			if strings.Contains(l.upper, "END-EXEC") {
				p.consume()
				break
			}
			if command == "" && len(l.tokens) > 0 && isCICSVerb(l.tokens[0]) {
				command = l.tokens[0]
			}
			p.consume()
		}
	}

	return &Node{
		Kind:      NodeCall,
		Callee:    "CICS." + command,
		Language:  LangCOBOL,
		Span:      Span{Start: Pos{Line: startLineNo}, End: Pos{Line: p.lastLine()}},
		RawSource: strings.Join(rawParts, "\n"),
	}
}

// parseSQLBlock parses EXEC SQL … END-EXEC as a single NodeCall with
// Callee="SQL.<verb>" and returns it (v23.47) for scope-correct attachment.
func (p *cobolParser) parseSQLBlock(m *Module, startLineNo int) *Node {
	startLine := p.consume()
	verb := ""
	if len(startLine.tokens) >= 3 {
		verb = startLine.tokens[2]
	}
	var rawParts []string
	rawParts = append(rawParts, startLine.raw)

	// v23.9 (Phase 6): same single-line guard as parseCICSBlock. A one-line
	// `EXEC SQL ... END-EXEC` must not consume the following statement.
	if !strings.Contains(startLine.upper, "END-EXEC") {
		for p.pos < len(p.lines) {
			l := p.peek()
			if l == nil {
				break
			}
			rawParts = append(rawParts, l.raw)
			if strings.Contains(l.upper, "END-EXEC") {
				p.consume()
				break
			}
			if verb == "" && len(l.tokens) > 0 && isSQLVerb(l.tokens[0]) {
				verb = l.tokens[0]
			}
			p.consume()
		}
	}
	return &Node{
		Kind:      NodeCall,
		Callee:    "SQL." + verb,
		Language:  LangCOBOL,
		Span:      Span{Start: Pos{Line: startLineNo}, End: Pos{Line: p.lastLine()}},
		RawSource: strings.Join(rawParts, "\n"),
	}
}

func (p *cobolParser) lastLine() int {
	if p.pos > 0 && p.pos <= len(p.lines) {
		return p.lines[p.pos-1].lineNo
	}
	return 0
}

// ─── Helpers ──────────────────────────────────────────────────

// isEndProgram reports whether the line is "END PROGRAM name."
func isEndProgram(l *cobolLine) bool {
	return len(l.tokens) >= 2 && l.tokens[0] == "END" && strings.TrimSuffix(l.tokens[1], ".") == "PROGRAM"
}

// isDivision returns true if the line starts a top-level COBOL division.
func isDivision(l *cobolLine) bool {
	if len(l.tokens) < 2 {
		return false
	}
	div := strings.TrimSuffix(l.tokens[1], ".")
	return div == "DIVISION"
}

// isSection returns true if the line is a SECTION header.
func isSection(l *cobolLine) bool {
	for _, tok := range l.tokens {
		if strings.TrimSuffix(tok, ".") == "SECTION" {
			return true
		}
	}
	return false
}

// paragraphReserved lists the words that can stand alone on a line with a
// period but never name a paragraph: verbs without operands and every
// explicit scope terminator of the COBOL standard.
//
// v26.57 (D008): END-PERFORM, CONTINUE and the other END- terminators were
// missing, so "END-PERFORM." opened a false paragraph named END-PERFORM and
// every later statement landed in it. The list is explicit on purpose: a real
// paragraph may start with END- (END-OF-JOB-PARA).
var paragraphReserved = map[string]bool{
	"END": true, "STOP": true, "GOBACK": true, "EXIT": true, "CONTINUE": true,
	"MOVE": true, "ADD": true, "SUBTRACT": true, "MULTIPLY": true,
	"DIVIDE": true, "COMPUTE": true, "DISPLAY": true, "ACCEPT": true,
	"EVALUATE": true, "WHEN": true, "ELSE": true, "PERFORM": true,
	"FILE-CONTROL": true,
	"END-ACCEPT":   true, "END-ADD": true, "END-CALL": true, "END-COMPUTE": true,
	"END-DELETE": true, "END-DISPLAY": true, "END-DIVIDE": true,
	"END-EVALUATE": true, "END-EXEC": true, "END-IF": true, "END-MULTIPLY": true,
	"END-PERFORM": true, "END-READ": true, "END-RECEIVE": true, "END-RETURN": true,
	"END-REWRITE": true, "END-SEARCH": true, "END-START": true, "END-STRING": true,
	"END-SUBTRACT": true, "END-UNSTRING": true, "END-WRITE": true,
}

// isParagraphHeader: a single COBOL name token ending with ".",
// not a keyword, not a level number.
func isParagraphHeader(l *cobolLine) bool {
	if len(l.tokens) != 1 {
		return false
	}
	tok := l.tokens[0]
	if !strings.HasSuffix(tok, ".") {
		return false
	}
	name := strings.TrimSuffix(tok, ".")
	if name == "" {
		return false
	}
	if paragraphReserved[name] {
		return false
	}
	if parseLevel(name) > 0 {
		return false
	}
	return true
}

// parseLevel returns the integer level number (01-88) or 0.
func parseLevel(s string) int {
	s = strings.TrimSuffix(s, ".")
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	// COBOL valid levels: 01-49, 66 (RENAMES), 77 (independent), 88 (condition).
	if (n >= 1 && n <= 49) || n == 66 || n == 77 || n == 88 {
		return n
	}
	return 0
}

// extractPIC finds the PIC/PICTURE clause value. Recognises the full
// PIC string including all edit characters until the next clause keyword
// (USAGE, VALUE, OCCURS, REDEFINES, PIC, ".") is encountered.
//
// v26.58 (D065): the four clause extractors used strings.Index on the whole
// clause, so a keyword inside the field name matched (WS-PIC gave type "?",
// WS-VALUE gave value=PIC). They now use clauseWordIndex, which only matches
// a whole word. A legal data name is never a reserved word, so the field name
// can no longer match.
func extractPIC(upper string) string {
	var start int = -1
	var keyLen int
	for _, k := range []string{"PICTURE", "PIC"} {
		if idx := clauseWordIndex(upper, k); idx >= 0 {
			start = idx
			keyLen = len(k)
			break
		}
	}
	if start < 0 {
		return "?"
	}
	rest := strings.TrimSpace(upper[start+keyLen:])
	rest = strings.TrimPrefix(rest, "IS ")
	// Read until whitespace+next-keyword or end-of-clause period.
	// PIC strings can contain X 9 A Z $ , . / S V P + - CR DB ( )
	// We stop at the first whitespace-separated token that is a clause
	// keyword, or at a trailing period.
	toks := strings.Fields(rest)
	if len(toks) == 0 {
		return "?"
	}
	stopWords := map[string]bool{
		"USAGE": true, "VALUE": true, "OCCURS": true, "REDEFINES": true,
		"BLANK": true, "JUSTIFIED": true, "JUST": true, "SIGN": true,
		"SYNCHRONIZED": true, "SYNC": true, "PIC": true, "PICTURE": true,
		"COMP": true, "COMP-1": true, "COMP-2": true, "COMP-3": true,
		"COMP-4": true, "COMP-5": true, "BINARY": true, "DISPLAY": true,
		"PACKED-DECIMAL": true,
	}
	out := toks[0]
	// If the PIC has parentheses split across tokens like "X (10)", join them.
	if len(toks) > 1 && strings.HasPrefix(toks[1], "(") {
		out += toks[1]
	}
	out = strings.TrimSuffix(out, ".")
	if stopWords[out] || out == "" { // "" for a bare "PIC." (v26.58 D065)
		return "?"
	}
	return out
}

// extractUsage returns the USAGE clause value (COMP-3, COMP, BINARY,
// DISPLAY, PACKED-DECIMAL, COMP-1..5, INDEX, POINTER) or "".
func extractUsage(upper string) string {
	// USAGE IS COMP-3 / USAGE COMP-3 / COMP-3 (bare)
	usageWords := []string{"COMP-3", "COMP-4", "COMP-5", "COMP-1", "COMP-2",
		"COMP", "BINARY", "PACKED-DECIMAL", "DISPLAY", "INDEX", "POINTER"}
	// Sort longest first so COMP-3 wins over COMP.
	sort.Slice(usageWords, func(i, j int) bool {
		return len(usageWords[i]) > len(usageWords[j])
	})
	for _, w := range usageWords {
		// Match as a whole word.
		if containsWord(upper, w) {
			return w
		}
	}
	return ""
}

// extractOccurs returns (count, dependingOnName).
//
//	OCCURS 10 TIMES                          → (10, "")
//	OCCURS 1 TO 100 TIMES DEPENDING ON CNT  → (100, "CNT")
//	OCCURS 5 TIMES INDEXED BY X             → (5, "")
func extractOccurs(upper string) (int, string) {
	idx := clauseWordIndex(upper, "OCCURS")
	if idx < 0 {
		return 0, ""
	}
	rest := strings.Fields(upper[idx:])
	if len(rest) < 2 {
		return 0, ""
	}
	// v26.58 (D066): the old loop kept the largest number up to TIMES, and
	// TIMES is optional, so "OCCURS 10 PIC 9(3) VALUE 100" gave 100. The count
	// is the number right after OCCURS, or for "OCCURS m TO n" the n after TO.
	// rest[0] = "OCCURS"; rest[1] = the count or the minimum.
	count := parseIntLoose(rest[1])
	if len(rest) >= 4 && rest[2] == "TO" {
		if n := parseIntLoose(rest[3]); n > 0 {
			count = n
		}
	}

	// DEPENDING ON <name>
	dep := ""
	for i, t := range rest {
		if t == "DEPENDING" && i+2 < len(rest) && rest[i+1] == "ON" {
			dep = strings.TrimSuffix(rest[i+2], ".")
		}
	}
	return count, dep
}

// extractRedefines returns the redefined field name or "".
//
//	05 NEW-VIEW REDEFINES OLD-RECORD PIC X(80).
func extractRedefines(upper string) string {
	idx := clauseWordIndex(upper, "REDEFINES")
	if idx < 0 {
		return ""
	}
	rest := strings.Fields(upper[idx:])
	if len(rest) < 2 {
		return ""
	}
	return strings.TrimSuffix(rest[1], ".")
}

// extractValue returns the VALUE clause literal or "".
//
//	VALUE 'Y'  /  VALUE ZEROS  /  VALUE 100  /  VALUE IS SPACES
func extractValue(upper string) string {
	idx := clauseWordIndex(upper, "VALUE")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(upper[idx+len("VALUE"):])
	rest = strings.TrimPrefix(rest, "IS ")
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return ""
	}
	// Quoted literal? Return the whole quoted block.
	if strings.HasPrefix(rest, "'") || strings.HasPrefix(rest, "\"") {
		quote := rest[0:1]
		end := strings.Index(rest[1:], quote)
		if end >= 0 {
			return rest[:end+2]
		}
		return rest
	}
	// Bare token
	toks := strings.Fields(rest)
	if len(toks) > 0 {
		return strings.TrimSuffix(toks[0], ".")
	}
	return ""
}

// parseIntLoose returns the integer value of s, ignoring trailing period.
// Returns 0 for non-numeric.
func parseIntLoose(s string) int {
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return 0
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// containsWord returns true if upper contains word as a whole token
// (surrounded by whitespace, start/end of string, or punctuation).
func containsWord(upper, word string) bool {
	idx := strings.Index(upper, word)
	for idx >= 0 {
		before := byte(' ')
		after := byte(' ')
		if idx > 0 {
			before = upper[idx-1]
		}
		if idx+len(word) < len(upper) {
			after = upper[idx+len(word)]
		}
		if !isWordChar(before) && !isWordChar(after) {
			return true
		}
		next := strings.Index(upper[idx+1:], word)
		if next < 0 {
			break
		}
		idx = idx + 1 + next
	}
	return false
}

// clauseWordIndex returns the byte index of the first whole-word occurrence of
// word in upper, or -1. Word boundaries follow containsWord. v26.58 (D065).
func clauseWordIndex(upper, word string) int {
	from := 0
	for {
		i := strings.Index(upper[from:], word)
		if i < 0 {
			return -1
		}
		idx := from + i
		end := idx + len(word)
		if (idx == 0 || !isWordChar(upper[idx-1])) && (end == len(upper) || !isWordChar(upper[end])) {
			return idx
		}
		from = idx + 1
	}
}

func isWordChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') || c == '-' || c == '_'
}

// encodeTag serialises a metadata map as "k1=v1;k2=v2", omitting empty values.
// Keys are sorted for stable output (important for checksum/diff stability).
func encodeTag(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ";")
}

// isCICSVerb returns true for the recognised CICS command verbs.
func isCICSVerb(tok string) bool {
	switch tok {
	case "READ", "WRITE", "REWRITE", "DELETE",
		"LINK", "XCTL", "RETURN", "ABEND",
		"SEND", "RECEIVE", "CONVERSE",
		"SYNCPOINT", "ROLLBACK",
		"ASSIGN", "ADDRESS", "HANDLE", "IGNORE",
		"WRITEQ", "READQ", "DELETEQ",
		"READNEXT", "READPREV", "STARTBR", "ENDBR", "RESETBR",
		"INQUIRE", "PERFORM", "SET",
		"ALLOCATE", "FREEMAIN", "GETMAIN",
		"DUMP", "JOURNAL", "WAIT", "POST", "POP":
		return true
	}
	return false
}

// isSQLVerb returns true for top-level embedded SQL verbs.
func isSQLVerb(tok string) bool {
	switch tok {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "MERGE",
		"DECLARE", "OPEN", "CLOSE", "FETCH", "COMMIT", "ROLLBACK",
		"PREPARE", "EXECUTE", "WHENEVER":
		return true
	}
	return false
}

// isExportedCOBOL: COBOL paragraph/section names are conventionally
// considered exported; names starting with '-' are internal by convention.
func isExportedCOBOL(name string) bool {
	return !strings.HasPrefix(name, "-") && name != ""
}

// cobolBaseName returns the filename without extension.
func cobolBaseName(filePath string) string {
	base := filePath
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, "\\"); idx >= 0 {
		base = base[idx+1:]
	}
	for _, ext := range []string{".cob", ".cbl", ".cpy", ".cobol"} {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}
