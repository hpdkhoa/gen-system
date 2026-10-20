// symbols/symbol.go (v23.32, stdlib provenance): added Provenance field +
// type. Language-agnostic origin tier so the index, genbench metrics, skill
// export, and Understand UI can distinguish a project's own symbols from
// standard-library / third-party ones without branching per language. Default
// zero value is ProvProject, so every pre-existing symbol is unchanged.
package symbols

// ─────────────────────────────────────────────────────────────
// symbol.go: language-agnostic symbol types
//
// A Symbol is a named, typed, located entity in a codebase:
// a function, a type, a variable, a constant.  Symbols are
// produced from the unified AST and stored in the Index so
// any part of GenSystem can ask "what is this name?" without
// re-parsing source.
// ─────────────────────────────────────────────────────────────

import "github.com/hpdkhoa/gen-system/generator/internal/ast"

// Provenance is the origin tier of a symbol. It is language-agnostic: the
// shared index/metric/UI code keys off this, never off Language. A frontend
// or the stdlib loader sets it; everything downstream only reads it.
type Provenance string

const (
	// ProvProject is a symbol defined in the user's own codebase. It is the
	// zero value on purpose: any Symbol created before v23.32, or by any code
	// path that doesn't set Provenance, is treated as project source.
	ProvProject Provenance = ""

	// ProvStdlib is a symbol from a language's standard library, parsed
	// read-only from a configured stdlib root (e.g. GOROOT/src for Go, JDK
	// signature sources for Java).
	ProvStdlib Provenance = "stdlib"

	// ProvThirdParty is reserved for vendored / module-cache dependencies.
	// Not populated yet (no loader emits it); defined now so the taxonomy is
	// stable and downstream switch statements are exhaustive.
	ProvThirdParty Provenance = "thirdparty"
)

// IsExternal reports whether the symbol originates outside the user's project
// (stdlib or third-party). Shared code uses this to limit "internal recall"
// to project symbols and to drive the default Understand filter.
func (p Provenance) IsExternal() bool {
	return p == ProvStdlib || p == ProvThirdParty
}

// SymbolKind classifies what a symbol represents.
type SymbolKind string

const (
	SymFunc      SymbolKind = "func"
	SymMethod    SymbolKind = "method"
	SymType      SymbolKind = "type" // struct, class, interface, enum
	SymVar       SymbolKind = "var"
	SymConst     SymbolKind = "const"
	SymField     SymbolKind = "field"     // struct/class field
	SymParam     SymbolKind = "param"     // function parameter
	SymModule    SymbolKind = "module"    // file-level module symbol
	SymImport    SymbolKind = "import"    // import binding
	SymProcedure SymbolKind = "procedure" // CA Gen PROCEDURE
)

// RefKind describes how a symbol is used at a reference site.
type RefKind string

const (
	RefRead    RefKind = "read"
	RefWrite   RefKind = "write"
	RefCall    RefKind = "call"
	RefImport  RefKind = "import"
	RefExtends RefKind = "extends"
	RefImpl    RefKind = "implements"
)

// Symbol is a single named entity in the codebase.
type Symbol struct {
	// ID is globally unique across the whole repository.
	// Format: "<filePath>#<kind>:<qualifiedName>"
	// e.g. "backend/internal/user/handler.go#func:CreateUser"
	ID string

	Name      string
	QualName  string // qualified name, e.g. "http.Handler" or "User.Email"
	Kind      SymbolKind
	Language  ast.Language
	DefinedIn string // absolute file path
	Span      ast.Span

	// Type information
	Type    ast.TypeRef
	Params  []ast.Parameter // for functions/methods
	Returns []ast.TypeRef   // for functions/methods

	// For methods: the receiver type name
	ReceiverType string
	// ReceiverName is the receiver's variable name in Go (t in func (t *Timer) End). v26.74.
	ReceiverName string `json:",omitempty"`
	// Locals are Go local variables with a type the declaration shows; see ast.Node.Locals. v26.74.
	Locals []ast.Local `json:",omitempty"`

	// For imports: the explicit alias, "" when the import has none.
	// v26.58 (D101): resolveImportPath needs it to match an unaliased import by
	// the last segment of its path without letting an aliased one claim it.
	ImportAlias string `json:",omitempty"`

	Exported   bool
	DocComment string

	// Program is the COBOL program that holds this paragraph, section or
	// program, set only in a file with several programs (v26.69). A PERFORM
	// resolves only to a paragraph of the caller's own program.
	Program string `json:",omitempty"`

	// Layer is an optional architectural classification:
	// "model", "repo", "service", "handler", "frontend", "mobile"
	// Inferred from directory structure or explicit annotation.
	Layer string

	// Provenance is the origin tier (project / stdlib / thirdparty).
	// Zero value (ProvProject) means the user's own code, so existing
	// callers that never set this field are unaffected.
	Provenance Provenance
}

// Reference is one usage site of a symbol.
type Reference struct {
	SymbolID string // the symbol being referenced
	In       string // file path containing the reference
	Span     ast.Span
	Kind     RefKind
}

// CallEdge represents a directed call between two symbols.
type CallEdge struct {
	CallerID string // symbol that contains the call
	CalleeID string // symbol being called (may be unresolved)
	// CalleeName is the name as written in source, for unresolved calls.
	CalleeName string
	Span       ast.Span
}

// SymbolID builds a canonical ID from its components.
func SymbolID(filePath string, kind SymbolKind, qualName string) string {
	return filePath + "#" + string(kind) + ":" + qualName
}
