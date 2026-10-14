// unified_ir.go (v23.7): Node.Else added (ELSE body of an if-branch) so the
// CFG can render a distinct else-block.
package ast

// ─────────────────────────────────────────────────────────────
// unified_ir.go: language-agnostic Intermediate Representation
//
// Every language frontend (Go, TypeScript, Python, CA Gen) must
// convert its native parse tree into this canonical IR.
//
// This IR follows three design principles.
//  1. Lossless. Parse → IR → emit must produce byte-identical
//     output for regions the parser understands.
//  2. Explicit ignorance. A construct outside the supported subset
//     becomes NodeUnknown with an UnknownReason. The IR never drops it silently.
//  3. Human-verifiable. Every Node carries a Span so the
//     presentation layer can highlight it in the source viewer.
// ─────────────────────────────────────────────────────────────

// NodeKind discriminates the type of a Node.
type NodeKind string

const (
	// Declarations
	NodeModule    NodeKind = "module"
	NodeFunction  NodeKind = "function"
	NodeMethod    NodeKind = "method"
	NodeStruct    NodeKind = "struct"
	NodeClass     NodeKind = "class"
	NodeInterface NodeKind = "interface"
	NodeEnum      NodeKind = "enum"
	NodeTypeAlias NodeKind = "type_alias"
	NodeVariable  NodeKind = "variable"
	NodeConstant  NodeKind = "constant"
	NodeImport    NodeKind = "import"
	NodeExport    NodeKind = "export"

	// Statements (inside function bodies)
	NodeCall      NodeKind = "call"
	NodeReturn    NodeKind = "return"
	NodeAssign    NodeKind = "assign"
	NodeBranch    NodeKind = "branch"    // if/switch/match
	NodeLoop      NodeKind = "loop"      // for/while/each
	NodeDefer     NodeKind = "defer"     // Go defer
	NodeTry       NodeKind = "try"       // try/catch/except
	NodeEscape    NodeKind = "escape"    // CA Gen ESCAPE
	NodeProcedure NodeKind = "procedure" // CA Gen PROCEDURE block

	// Explicit ignorance: the parser met this but cannot fully model it.
	// Always populated: UnknownReason + RawSource.
	NodeUnknown NodeKind = "unknown"
)

// ─── Position ────────────────────────────────────────────────

// Pos is a single point in a source file.
type Pos struct {
	Line   int // 1-based
	Column int // 1-based
	Offset int // byte offset from start of file
}

// Span covers a contiguous region [Start, End).
type Span struct {
	Start Pos
	End   Pos
}

// Patched in v18 (D1#1): the prior implementation
//
//	s.Start.Offset < s.End.Offset || s.Start.Line > 0
//
// returned true for any span whose Start.Line was set, including the
// inverted span {Start:{Line:1,Offset:5}, End:{Line:1,Offset:3}} that
// the test fixture deliberately marks invalid. The corrected predicate
// treats a span as valid iff both endpoints carry a positive line and
// the start does not lie after the end (compared by line first, then
// offset for same-line spans).
func (s Span) IsValid() bool {
	if s.Start.Line <= 0 || s.End.Line <= 0 {
		return false
	}
	if s.Start.Line != s.End.Line {
		return s.Start.Line < s.End.Line
	}
	return s.Start.Offset <= s.End.Offset
}

// ─── Type System ─────────────────────────────────────────────

// TypeRef is a simplified, language-agnostic type reference.
// It captures enough structure to reason about compatibility
// without reproducing a full type checker.
type TypeRef struct {
	Name    string    // bare type name, e.g. "string", "User", "T"
	Package string    // qualifier, e.g. "http" in http.Request
	IsSlice bool      // []T
	IsMap   bool      // map[K]V or Dict[K,V]
	IsPtr   bool      // *T or Optional[T]
	IsFunc  bool      // func(...) or Callable[...]
	Params  []TypeRef // generic parameters
	KeyType *TypeRef  // for maps: key type
}

func (t TypeRef) String() string {
	if t.Package != "" {
		return t.Package + "." + t.Name
	}
	return t.Name
}

// UnknownType is returned when we cannot parse a type expression.
var UnknownType = TypeRef{Name: "?"}

// ─── Structural Elements ──────────────────────────────────────

// Parameter represents one parameter of a function or method.
type Parameter struct {
	Name     string
	Type     TypeRef
	Variadic bool   // *args, ...T
	Optional bool   // TypeScript optional params
	Default  string // default value literal if present
}

// Field represents one field in a struct, class, or interface.
type Field struct {
	Name       string
	Type       TypeRef
	Tag        string // Go struct tags, e.g. `json:"name"`
	Optional   bool   // TypeScript ?
	Exported   bool
	DocComment string
	// Line is the source line of the field's declaration; 0 when the frontend
	// does not record it. v26.68: fields had no position, so every field
	// symbol reported line 0.
	Line int `json:",omitempty"`
}

// EnumMember represents one value in an enum.
type EnumMember struct {
	Name  string
	Value string // string or int literal
}

// ─── Node ─────────────────────────────────────────────────────

// Node is the unified AST element. Fields are populated according
// to Kind. See inline comments for which fields are relevant.
type Node struct {
	// Universal fields
	ID       string // unique within module, e.g. "func:CreateUser"
	Kind     NodeKind
	Name     string
	Language Language
	Span     Span

	// Modifiers
	Exported   bool
	Abstract   bool // abstract class/method
	Async      bool // async function
	Static     bool // static method
	DocComment string

	// ── NodeFunction / NodeMethod ──────────────────────────
	// Receiver is non-nil for methods (Go: pointer/value receiver,
	// Python: self/cls, CA Gen: entity the procedure belongs to).
	Receiver *TypeRef
	Params   []Parameter
	Returns  []TypeRef

	// ── NodeStruct / NodeClass / NodeInterface ─────────────
	Fields     []Field
	Implements []string // interface names this type satisfies
	Extends    string   // parent class (single inheritance)
	TypeParams []string // generic type parameter names

	// ── NodeTypeAlias ──────────────────────────────────────
	// The right-hand side type (may be complex union/intersection).
	AliasTarget TypeRef

	// ── NodeEnum ───────────────────────────────────────────
	EnumMembers []EnumMember

	// ── NodeImport ─────────────────────────────────────────
	ImportPath  string   // raw import path / module name
	ImportAlias string   // 'as' alias
	ImportNames []string // named imports: {X, Y as Z}
	ImportAll   bool     // import * as X

	// ── NodeVariable / NodeConstant ────────────────────────
	TypeHint     TypeRef
	ValueLiteral string // literal value if statically known

	// ── NodeCall ───────────────────────────────────────────
	Callee   string   // function/method name being called
	CallArgs []string // argument expressions (text snippets)

	// ── NodeUnknown ────────────────────────────────────────
	// Always populated when Kind == NodeUnknown.
	// This is the "explicit ignorance channel". The presentation
	// layer surfaces these to the human for verification.
	RawSource     string
	UnknownReason string

	// Children are nested nodes (e.g. function body statements,
	// struct fields as NodeVariable, class methods as NodeMethod).
	// For NodeBranch (if), Children is the then body; Else is the else body
	// (v23.7) so the CFG builder can emit a distinct else-block. Else is nil
	// for if-without-else and for switch/select (whose cases are in Children).
	Children []*Node
	Else     []*Node
}

// ─── Module ──────────────────────────────────────────────────

// Module is the top-level result of parsing one source file.
// It corresponds to one file regardless of language.
type Module struct {
	Language    Language
	FilePath    string  // absolute path
	PackageName string  // Go package, Python module, TS module name
	Nodes       []*Node // top-level declarations and imports
	ParseErrors []ParseError

	// Checksum (SHA-256 hex) of the source bytes at parse time.
	// Used by the cache to detect staleness without re-reading disk.
	Checksum string

	// RawSource is retained for the lossless emit path.
	// Frontends may store the original bytes here.
	RawSource []byte
}

// TopLevelByKind returns all top-level nodes matching one or more kinds.
func (m *Module) TopLevelByKind(kinds ...NodeKind) []*Node {
	set := make(map[NodeKind]bool, len(kinds))
	for _, k := range kinds {
		set[k] = true
	}
	var out []*Node
	for _, n := range m.Nodes {
		if set[n.Kind] {
			out = append(out, n)
		}
	}
	return out
}

// UnknownNodes returns all NodeUnknown entries at any depth.
// Used by the presentation layer to build the "needs human review" list.
func (m *Module) UnknownNodes() []*Node {
	var out []*Node
	var walk func(nodes []*Node)
	walk = func(nodes []*Node) {
		for _, n := range nodes {
			if n.Kind == NodeUnknown {
				out = append(out, n)
			}
			walk(n.Children)
		}
	}
	walk(m.Nodes)
	return out
}

// ─── ParseError ──────────────────────────────────────────────

// ParseError captures a specific location where parsing failed or
// where the parser had to make an assumption.
type ParseError struct {
	Message     string
	Span        Span
	Recoverable bool // true: we emitted an NodeUnknown and continued
	// false: the file is not parseable at all
}
