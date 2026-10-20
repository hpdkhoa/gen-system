package symbols

// ─────────────────────────────────────────────────────────────
// table.go: per-file symbol table
//
// A Table holds all symbols defined in one source file.
// It is produced by walking a unified ast.Module.
// ─────────────────────────────────────────────────────────────

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

// Table is a flat symbol table for one file.
type Table struct {
	FilePath string
	Symbols  map[string]*Symbol   // keyed by Symbol.ID
	ByName   map[string][]*Symbol // name → possibly multiple (overloads, etc.)
}

// NewTable creates an empty Table for filePath.
func NewTable(filePath string) *Table {
	return &Table{
		FilePath: filePath,
		Symbols:  make(map[string]*Symbol),
		ByName:   make(map[string][]*Symbol),
	}
}

// Add inserts a symbol, updating both lookup maps.
func (t *Table) Add(s *Symbol) {
	t.Symbols[s.ID] = s
	t.ByName[s.Name] = append(t.ByName[s.Name], s)
}

// Get returns a symbol by its full ID.
func (t *Table) Get(id string) (*Symbol, bool) {
	s, ok := t.Symbols[id]
	return s, ok
}

// Lookup returns all symbols in this file with the given name.
func (t *Table) Lookup(name string) []*Symbol {
	return t.ByName[name]
}

// All returns every symbol in this table, sorted by ID.
//
// v26.58 (D101): the order came from the map and changed between runs, so a
// caller that took the first match (resolveImportPath) was not deterministic.
func (t *Table) All() []*Symbol {
	out := make([]*Symbol, 0, len(t.Symbols))
	for _, s := range t.Symbols {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ─── Build ────────────────────────────────────────────────────

// BuildTable extracts all symbols from a parsed Module.
func BuildTable(m *ast.Module) *Table {
	t := NewTable(m.FilePath)
	layer := inferLayer(m.FilePath)

	// Module-level symbol
	modID := SymbolID(m.FilePath, SymModule, m.PackageName)
	t.Add(&Symbol{
		ID:        modID,
		Name:      m.PackageName,
		QualName:  m.PackageName,
		Kind:      SymModule,
		Language:  m.Language,
		DefinedIn: m.FilePath,
		Exported:  true,
		Layer:     layer,
	})

	for _, n := range m.Nodes {
		extractNode(t, n, m.FilePath, m.Language, layer, "", "")
	}
	return t
}

// extractNode recursively extracts symbols from a Node and its children.
// ownerName and ownerKind name the enclosing node ("" at file level).
func extractNode(t *Table, n *ast.Node, filePath string, lang ast.Language, layer, ownerName string, ownerKind ast.NodeKind) {
	if n == nil {
		return
	}

	switch n.Kind {
	case ast.NodeFunction:
		s := &Symbol{
			ID:         SymbolID(filePath, SymFunc, n.SymbolName()),
			Name:       n.Name,
			QualName:   qualName(ownerName, n.SymbolName()),
			Kind:       SymFunc,
			Program:    n.Program,
			Language:   lang,
			DefinedIn:  filePath,
			Span:       n.Span,
			Exported:   n.Exported,
			DocComment: n.DocComment,
			Params:     n.Params,
			Returns:    n.Returns,
			Layer:      layer,
			Locals:     n.Locals,
		}
		t.Add(s)
		for _, child := range n.Children {
			extractNode(t, child, filePath, lang, layer, n.Name, n.Kind)
		}

	case ast.NodeMethod:
		// v26.58 (D102): TS and Python methods have no Receiver, so the QualName
		// was ".name" and two classes with the same method name in one file shared
		// one ID. With no Receiver the owner name now qualifies the method. Only a
		// class or struct owner becomes the ReceiverType: a Go interface method
		// has no receiver, and callers such as the style gate read ReceiverType as
		// "this type has methods with bodies".
		receiver := ""
		owner := ownerName
		if n.Receiver != nil {
			receiver = n.Receiver.Name
			owner = receiver
		} else if ownerKind == ast.NodeClass || ownerKind == ast.NodeStruct {
			receiver = ownerName
		}
		qn := owner + "." + n.Name
		s := &Symbol{
			ID:           SymbolID(filePath, SymMethod, qn),
			Name:         n.Name,
			QualName:     qn,
			Kind:         SymMethod,
			Language:     lang,
			DefinedIn:    filePath,
			Span:         n.Span,
			Exported:     n.Exported,
			DocComment:   n.DocComment,
			Params:       n.Params,
			Returns:      n.Returns,
			ReceiverType: receiver,
			ReceiverName: n.ReceiverName,
			Locals:       n.Locals,
			Layer:        layer,
		}
		t.Add(s)

	case ast.NodeStruct, ast.NodeClass:
		kind := SymType
		s := &Symbol{
			ID:         SymbolID(filePath, kind, n.Name),
			Name:       n.Name,
			QualName:   qualName(ownerName, n.Name),
			Kind:       kind,
			Language:   lang,
			DefinedIn:  filePath,
			Span:       n.Span,
			Exported:   n.Exported,
			DocComment: n.DocComment,
			Layer:      layer,
		}
		t.Add(s)
		// Fields
		for _, f := range n.Fields {
			// v26.68: a field had no span, so it reported line 0. It takes its own
			// line when the frontend records one, else its parent's.
			span := n.Span
			if f.Line > 0 {
				span = ast.Span{Start: ast.Pos{Line: f.Line}, End: ast.Pos{Line: f.Line}}
			}
			t.Add(&Symbol{
				ID:        SymbolID(filePath, SymField, n.Name+"."+f.Name),
				Name:      f.Name,
				QualName:  n.Name + "." + f.Name,
				Kind:      SymField,
				Language:  lang,
				DefinedIn: filePath,
				Span:      span,
				Type:      f.Type,
				Exported:  f.Exported,
				Layer:     layer,
			})
		}
		// Methods / nested nodes
		for _, child := range n.Children {
			extractNode(t, child, filePath, lang, layer, n.Name, n.Kind)
		}

	case ast.NodeInterface:
		s := &Symbol{
			ID:         SymbolID(filePath, SymType, n.Name),
			Name:       n.Name,
			QualName:   qualName(ownerName, n.Name),
			Kind:       SymType,
			Language:   lang,
			DefinedIn:  filePath,
			Span:       n.Span,
			Exported:   n.Exported,
			DocComment: n.DocComment,
			Layer:      layer,
		}
		t.Add(s)
		for _, child := range n.Children {
			extractNode(t, child, filePath, lang, layer, n.Name, n.Kind)
		}

	case ast.NodeTypeAlias, ast.NodeEnum:
		t.Add(&Symbol{
			ID:        SymbolID(filePath, SymType, n.Name),
			Name:      n.Name,
			QualName:  qualName(ownerName, n.Name),
			Kind:      SymType,
			Language:  lang,
			DefinedIn: filePath,
			Span:      n.Span,
			Exported:  n.Exported,
			Layer:     layer,
		})

	case ast.NodeVariable:
		t.Add(&Symbol{
			ID:        SymbolID(filePath, SymVar, qualName(ownerName, n.Name)),
			Name:      n.Name,
			QualName:  qualName(ownerName, n.Name),
			Kind:      SymVar,
			Language:  lang,
			DefinedIn: filePath,
			Span:      n.Span,
			Exported:  n.Exported,
			Type:      n.TypeHint,
			Layer:     layer,
		})

	case ast.NodeConstant:
		t.Add(&Symbol{
			ID:        SymbolID(filePath, SymConst, qualName(ownerName, n.Name)),
			Name:      n.Name,
			QualName:  qualName(ownerName, n.Name),
			Kind:      SymConst,
			Language:  lang,
			DefinedIn: filePath,
			Span:      n.Span,
			Exported:  n.Exported,
			Type:      n.TypeHint,
			Layer:     layer,
		})

	case ast.NodeImport:
		importName := n.Name
		if n.ImportAlias != "" {
			importName = n.ImportAlias // v26.34: `import js "encoding/json"` is referred to as js
		}
		t.Add(&Symbol{
			ID:          SymbolID(filePath, SymImport, n.ImportPath),
			Name:        importName,
			QualName:    n.ImportPath,
			Kind:        SymImport,
			Language:    lang,
			DefinedIn:   filePath,
			Span:        n.Span,
			Layer:       layer,
			ImportAlias: n.ImportAlias, // v26.58 (D101)
		})

	case ast.NodeProcedure:
		t.Add(&Symbol{
			ID:         SymbolID(filePath, SymProcedure, n.SymbolName()),
			Name:       n.Name,
			QualName:   n.SymbolName(),
			Kind:       SymProcedure,
			Program:    n.Program,
			Language:   lang,
			DefinedIn:  filePath,
			Span:       n.Span,
			Exported:   true,
			DocComment: n.DocComment,
			Layer:      "procedure",
		})
		for _, child := range n.Children {
			extractNode(t, child, filePath, lang, layer, n.Name, n.Kind)
		}

	// v26.69: a COBOL file may hold several programs. Only the first one became
	// a module symbol (from PackageName), so a CALL to any later program of the
	// file found no definition and counted as external.
	case ast.NodeModule:
		if lang != ast.LangCOBOL || n.Name == "" {
			return
		}
		id := SymbolID(filePath, SymModule, n.Name)
		if s, ok := t.Symbols[id]; ok {
			s.Span = n.Span
			s.Program = n.Program
			return
		}
		t.Add(&Symbol{
			ID:        id,
			Name:      n.Name,
			QualName:  n.Name,
			Kind:      SymModule,
			Language:  lang,
			DefinedIn: filePath,
			Span:      n.Span,
			Exported:  true,
			Program:   n.Program,
			Layer:     layer,
		})

	// Calls embedded in function bodies
	case ast.NodeCall:
		// Not a definition: skip (handled by resolver)

	case ast.NodeUnknown:
		// Not a definition: skip (surfaced via Module.UnknownNodes)
	}
}

// ─── Helpers ──────────────────────────────────────────────────

func qualName(owner, name string) string {
	if owner == "" {
		return name
	}
	return owner + "." + name
}

// inferLayer derives an architectural layer from the file path.
// Matches the convention in the existing v14 SystemGraph.
//
// v26.58 (D106): the folder rules tested the raw OS path, so on Windows they all
// missed; the path is now in slash form first. The CA Gen rule used Contains, so
// any path with ".ad" in it (x.admin.go) got "cagen"; it now tests the extension.
func inferLayer(filePath string) string {
	lower := strings.ToLower(filepath.ToSlash(filePath))
	ext := filepath.Ext(lower)
	switch {
	case strings.Contains(lower, "/model/") || strings.HasSuffix(lower, "_model.go"):
		return "model"
	case strings.Contains(lower, "/repository/") || strings.Contains(lower, "/repo/") ||
		strings.HasSuffix(lower, "_repo.go"):
		return "repo"
	case strings.Contains(lower, "/service/") || strings.HasSuffix(lower, "_service.go"):
		return "service"
	case strings.Contains(lower, "/handler/") || strings.HasSuffix(lower, "_handler.go"):
		return "handler"
	case strings.Contains(lower, "/validation/") || strings.HasSuffix(lower, "_validation.go"):
		return "validation"
	case strings.Contains(lower, "/frontend/") || strings.HasSuffix(lower, ".tsx") ||
		strings.HasSuffix(lower, ".ts"):
		return "frontend"
	case strings.Contains(lower, "/mobile/"):
		return "mobile"
	case ext == ".ad" || ext == ".cagen":
		return "cagen"
	default:
		return ""
	}
}
