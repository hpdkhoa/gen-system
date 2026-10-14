// v23.39: Go if NodeBranch now preserves the condition text (exprText(st.Cond)) in RawSource for the CFG decision node.
// go_frontend.go (v23.6): convertBody now walks the function body
// statement-by-statement and emits NodeBranch/NodeLoop/NodeReturn/NodeAssign
// with nested Children (was a flat goast.Inspect that only produced NodeCall),
// so the CFG builder actually splits if/for/switch into branch & loop blocks.
// v23.7: if-else now puts the else body in Node.Else (distinct CFG block).
package ast

// ─────────────────────────────────────────────────────────────
// go_frontend.go: Go language frontend
//
// Uses the standard library go/ast + go/parser for exact,
// spec-accurate parsing. Every Go source construct we need
// is covered; unknown constructs (cgo pragmas, build constraints
// at the expression level) become NodeUnknown with explanation.
// ─────────────────────────────────────────────────────────────

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	goast "go/ast"
	goformat "go/format"
	goparser "go/parser"
	gotoken "go/token"
	"strings"
)

// GoFrontend implements Frontend for the Go language.
type GoFrontend struct{}

func init() { Register(&GoFrontend{}) }

func (g *GoFrontend) Language() Language   { return LangGo }
func (g *GoFrontend) Extensions() []string { return []string{".go"} }
func (g *GoFrontend) Available() bool      { return true } // stdlib, always present

// ─── Parse ───────────────────────────────────────────────────

func (g *GoFrontend) Parse(filePath string, source []byte) (*Module, error) {
	fset := gotoken.NewFileSet()
	file, err := goparser.ParseFile(fset, filePath, source, goparser.ParseComments)
	if err != nil {
		// go/parser returns a partial AST even on syntax errors. We proceed
		// but record the error as unrecoverable only if file is nil.
		if file == nil {
			return nil, fmt.Errorf("go_frontend: parse %s: %w", filePath, err)
		}
	}

	sum := sha256.Sum256(source)
	m := &Module{
		Language:  LangGo,
		FilePath:  filePath,
		Checksum:  fmt.Sprintf("%x", sum),
		RawSource: source,
	}
	if file.Name != nil {
		m.PackageName = file.Name.Name
	}

	cv := &goConverter{fset: fset, filePath: filePath}

	// ── Top-level declarations ────────────────────────────
	// v18 (D1#5): the previous code iterated file.Imports first and let
	// convertGenDecl below also emit NodeImport for each ImportSpec, so
	// every import was duplicated (e.g. 3 imports → 6 nodes). Drop the
	// redundant file.Imports loop and rely on the GenDecl walk, which
	// is the only source of top-level declarations.
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *goast.FuncDecl:
			m.Nodes = append(m.Nodes, cv.convertFunc(d))
		case *goast.GenDecl:
			m.Nodes = append(m.Nodes, cv.convertGenDecl(d)...)
		default:
			m.Nodes = append(m.Nodes, &Node{
				Kind:          NodeUnknown,
				Language:      LangGo,
				Span:          cv.span(decl.Pos(), decl.End()),
				RawSource:     extractSource(source, fset, decl.Pos(), decl.End()),
				UnknownReason: fmt.Sprintf("unrecognized top-level decl type %T", decl),
			})
		}
	}

	if err != nil {
		m.ParseErrors = append(m.ParseErrors, ParseError{
			Message:     err.Error(),
			Recoverable: true,
		})
	}
	return m, nil
}

// ─── Emit ────────────────────────────────────────────────────

func (g *GoFrontend) Emit(module *Module) ([]byte, error) {
	// Round-trip via go/format on the original source for unchanged files.
	// For modified modules, re-emit from the stored raw source plus any
	// structural changes applied via ast mutation functions.
	if module.RawSource != nil {
		var buf bytes.Buffer
		fset := gotoken.NewFileSet()
		file, err := goparser.ParseFile(fset, module.FilePath, module.RawSource, goparser.ParseComments)
		if err != nil {
			return module.RawSource, nil // return original on parse failure
		}
		if err := goformat.Node(&buf, fset, file); err != nil {
			return module.RawSource, nil
		}
		return buf.Bytes(), nil
	}
	return module.RawSource, nil
}

// ─── Converter ───────────────────────────────────────────────

type goConverter struct {
	fset     *gotoken.FileSet
	filePath string
}

func (c *goConverter) pos(p gotoken.Pos) Pos {
	if !p.IsValid() {
		return Pos{}
	}
	pp := c.fset.Position(p)
	return Pos{Line: pp.Line, Column: pp.Column, Offset: pp.Offset}
}

func (c *goConverter) span(start, end gotoken.Pos) Span {
	return Span{Start: c.pos(start), End: c.pos(end)}
}

// ── Import ────────────────────────────────────────────────────

func (c *goConverter) convertImport(imp *goast.ImportSpec) *Node {
	path := strings.Trim(imp.Path.Value, `"`)
	alias := ""
	if imp.Name != nil {
		alias = imp.Name.Name
	}
	parts := strings.Split(path, "/")
	id := "import:" + path
	return &Node{
		ID:          id,
		Kind:        NodeImport,
		Name:        parts[len(parts)-1],
		Language:    LangGo,
		Span:        c.span(imp.Pos(), imp.End()),
		ImportPath:  path,
		ImportAlias: alias,
		DocComment:  docText(imp.Comment),
	}
}

// ── GenDecl (var, const, type) ────────────────────────────────

func (c *goConverter) convertGenDecl(d *goast.GenDecl) []*Node {
	var nodes []*Node
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *goast.TypeSpec:
			nodes = append(nodes, c.convertTypeSpec(s, d))
		case *goast.ValueSpec:
			nodes = append(nodes, c.convertValueSpec(s, d)...)
		case *goast.ImportSpec:
			nodes = append(nodes, c.convertImport(s))
		}
	}
	return nodes
}

func (c *goConverter) convertTypeSpec(s *goast.TypeSpec, d *goast.GenDecl) *Node {
	exported := len(s.Name.Name) > 0 && s.Name.Name[0] >= 'A' && s.Name.Name[0] <= 'Z'
	doc := docText(d.Doc)

	switch t := s.Type.(type) {
	case *goast.StructType:
		n := &Node{
			ID:         "struct:" + s.Name.Name,
			Kind:       NodeStruct,
			Name:       s.Name.Name,
			Language:   LangGo,
			Span:       c.span(s.Pos(), s.End()),
			Exported:   exported,
			DocComment: doc,
		}
		if t.Fields != nil {
			for _, field := range t.Fields.List {
				n.Fields = append(n.Fields, c.convertField(field)...)
			}
		}
		return n

	case *goast.InterfaceType:
		n := &Node{
			ID:         "interface:" + s.Name.Name,
			Kind:       NodeInterface,
			Name:       s.Name.Name,
			Language:   LangGo,
			Span:       c.span(s.Pos(), s.End()),
			Exported:   exported,
			DocComment: doc,
		}
		if t.Methods != nil {
			for _, m := range t.Methods.List {
				child := c.convertInterfaceMethod(m, s.Name.Name)
				if child != nil {
					n.Children = append(n.Children, child)
				}
			}
		}
		return n

	default:
		return &Node{
			ID:          "type:" + s.Name.Name,
			Kind:        NodeTypeAlias,
			Name:        s.Name.Name,
			Language:    LangGo,
			Span:        c.span(s.Pos(), s.End()),
			Exported:    exported,
			DocComment:  doc,
			AliasTarget: c.convertExpr(s.Type),
		}
	}
}

func (c *goConverter) convertField(f *goast.Field) []Field {
	typ := c.convertExpr(f.Type)
	tag := ""
	if f.Tag != nil {
		tag = strings.Trim(f.Tag.Value, "`")
	}
	if len(f.Names) == 0 {
		// Embedded type
		name := typ.Name
		return []Field{{Name: name, Type: typ, Tag: tag, Exported: isExported(name),
			Line: c.span(f.Pos(), f.End()).Start.Line}}
	}
	var fields []Field
	for _, name := range f.Names {
		fields = append(fields, Field{
			Name:       name.Name,
			Type:       typ,
			Tag:        tag,
			Exported:   isExported(name.Name),
			DocComment: docText(f.Comment),
			Line:       c.span(name.Pos(), name.End()).Start.Line,
		})
	}
	return fields
}

func (c *goConverter) convertInterfaceMethod(f *goast.Field, ifaceName string) *Node {
	if len(f.Names) == 0 {
		// Embedded interface
		return &Node{
			Kind:     NodeImport, // re-use as "embedded interface" ref
			Name:     c.convertExpr(f.Type).Name,
			Language: LangGo,
			Span:     c.span(f.Pos(), f.End()),
		}
	}
	ft, ok := f.Type.(*goast.FuncType)
	if !ok {
		return nil
	}
	n := &Node{
		ID:       "method:" + ifaceName + "." + f.Names[0].Name,
		Kind:     NodeMethod,
		Name:     f.Names[0].Name,
		Language: LangGo,
		Span:     c.span(f.Pos(), f.End()),
		Exported: isExported(f.Names[0].Name),
	}
	if ft.Params != nil {
		for _, param := range ft.Params.List {
			n.Params = append(n.Params, c.convertParams(param)...)
		}
	}
	if ft.Results != nil {
		for _, res := range ft.Results.List {
			n.Returns = append(n.Returns, c.convertExpr(res.Type))
		}
	}
	return n
}

func (c *goConverter) convertValueSpec(s *goast.ValueSpec, d *goast.GenDecl) []*Node {
	var nodes []*Node
	kind := NodeVariable
	if d.Tok == gotoken.CONST {
		kind = NodeConstant
	}
	typeHint := UnknownType
	if s.Type != nil {
		typeHint = c.convertExpr(s.Type)
	}
	for i, name := range s.Names {
		val := ""
		if i < len(s.Values) {
			val = exprText(s.Values[i])
		}
		nodes = append(nodes, &Node{
			ID:           fmt.Sprintf("%s:%s", string(kind), name.Name),
			Kind:         kind,
			Name:         name.Name,
			Language:     LangGo,
			Span:         c.span(s.Pos(), s.End()),
			Exported:     isExported(name.Name),
			TypeHint:     typeHint,
			ValueLiteral: val,
			DocComment:   docText(d.Doc),
		})
	}
	return nodes
}

// ── FuncDecl ──────────────────────────────────────────────────

func (c *goConverter) convertFunc(d *goast.FuncDecl) *Node {
	kind := NodeFunction
	id := "func:" + d.Name.Name

	var receiver *TypeRef
	if d.Recv != nil && len(d.Recv.List) > 0 {
		kind = NodeMethod
		recv := d.Recv.List[0]
		t := c.convertExpr(recv.Type)
		receiver = &t
		id = "method:" + t.Name + "." + d.Name.Name
	}

	n := &Node{
		ID:         id,
		Kind:       kind,
		Name:       d.Name.Name,
		Language:   LangGo,
		Span:       c.span(d.Pos(), d.End()),
		Exported:   isExported(d.Name.Name),
		Receiver:   receiver,
		DocComment: docText(d.Doc),
	}

	if d.Type.Params != nil {
		for _, p := range d.Type.Params.List {
			n.Params = append(n.Params, c.convertParams(p)...)
		}
	}
	if d.Type.Results != nil {
		for _, r := range d.Type.Results.List {
			n.Returns = append(n.Returns, c.convertExpr(r.Type))
		}
	}

	// Extract call sites from the function body
	if d.Body != nil {
		n.Children = c.convertBody(d.Body)
	}

	return n
}

// convertBody walks a function body statement-by-statement, preserving control
// structure (v23.6). Previously this used goast.Inspect and only emitted a flat
// list of NodeCall for every call anywhere in the body. So if/for/switch never
// became NodeBranch/NodeLoop and the CFG collapsed every Go function into one
// block. Now each statement becomes the right node kind with nested Children,
// which lets the CFG builder split then/merge and loop blocks.
func (c *goConverter) convertBody(body *goast.BlockStmt) []*Node {
	if body == nil {
		return nil
	}
	return c.convertStmtList(body.List)
}

func (c *goConverter) convertStmtList(stmts []goast.Stmt) []*Node {
	var out []*Node
	for _, s := range stmts {
		out = append(out, c.convertStmt(s)...)
	}
	return out
}

// convertStmt maps one Go statement to one (or a few) IR nodes. Control-flow
// statements carry their body in Children; the condition's source line is kept
// via Span so the CFG shows real code. Calls nested in a statement's
// expressions are surfaced as child NodeCall so effect/call analysis still sees
// them.
func (c *goConverter) convertStmt(s goast.Stmt) []*Node {
	switch st := s.(type) {

	case *goast.IfStmt:
		n := &Node{Kind: NodeBranch, Language: LangGo, Name: "if", RawSource: exprText(st.Cond), Span: c.span(st.Pos(), st.End())}
		// v26.58 (D059): the calls in the Init (`if err := f(); ...`) were
		// dropped, so the call graph missed f.
		// v26.68: they went into Children, the then-body, so the CFG drew
		// `err := f()` inside the yes arm, after the decision. The Init and the
		// calls in the condition run before the branch, so they now come before
		// the node. An "else if" keeps working: Else holds a list of nodes.
		pre := c.initStmt(st.Init)
		pre = append(pre, c.callsIn(st.Cond)...)
		n.Children = append(n.Children, c.convertStmtList(st.Body.List)...)
		// v23.7: else / else-if goes into Else (a distinct block in the CFG),
		// not flattened into the then-body. An "else if" nests as a single
		// NodeBranch inside Else.
		if st.Else != nil {
			n.Else = c.convertStmt(st.Else)
		}
		return append(pre, n)

	case *goast.ForStmt:
		// v23.49: capture the loop condition so the CFG loop decision shows the
		// real test (e.g. "i < n") instead of a bare diamond. A bare `for {}` has
		// no Cond → leave RawSource empty (renders as "loop").
		cond := ""
		if st.Cond != nil {
			cond = exprText(st.Cond)
		}
		n := &Node{Kind: NodeLoop, Language: LangGo, Name: "for", RawSource: cond, Span: c.span(st.Pos(), st.End())}
		// v26.58 (D059): Init calls were dropped. v26.68: the Init runs once,
		// before the loop, so it comes before the node, not inside the body.
		pre := c.initStmt(st.Init)
		if st.Cond != nil {
			n.Children = append(n.Children, c.callsIn(st.Cond)...)
		}
		n.Children = append(n.Children, c.convertStmtList(st.Body.List)...)
		// v26.59 (N09): the Post calls were dropped, so `i = next(i)` left next
		// out of the call graph. Post runs after each pass of the body, so its
		// calls follow the body statements. callsIn returns nil for a nil Post.
		n.Children = append(n.Children, c.callsIn(st.Post)...)
		return append(pre, n)

	case *goast.RangeStmt:
		// v23.49: "range <x>" as the loop label.
		n := &Node{Kind: NodeLoop, Language: LangGo, Name: "range", RawSource: "range " + exprText(st.X), Span: c.span(st.Pos(), st.End())}
		n.Children = append(n.Children, c.callsIn(st.X)...)
		n.Children = append(n.Children, c.convertStmtList(st.Body.List)...)
		return []*Node{n}

	case *goast.SwitchStmt:
		// v25.9: a Go `switch` is a multi-way branch, not an if/else. Emit the
		// same shape COBOL's EVALUATE uses (proven in v23.41): the switch node
		// carries the tag in RawSource, and each case clause becomes a child
		// NodeBranch whose RawSource is the case value, holding that case's
		// statements as its own children. flow.Build fans these out to one
		// case-block each with a shared merge.
		//
		// Previously every case body was concatenated into Children with no Else
		// and no case values, so flow.Build's if/else path flattened the switch
		// into a cascade of fake decisions whose conditions were borrowed from
		// the `if`s *inside* the case bodies.
		n := &Node{Kind: NodeBranch, Language: LangGo, Name: "switch", Span: c.span(st.Pos(), st.End())}
		// v26.58 (D059): Init calls were dropped. v26.68: the Init and the tag
		// run once, before the cases, so they come before the node.
		pre := c.initStmt(st.Init)
		if st.Tag != nil {
			n.RawSource = exprText(st.Tag)
			pre = append(pre, c.callsIn(st.Tag)...)
		}
		n.Children = append(n.Children, c.caseClauses(st.Body.List)...)
		return append(pre, n)

	case *goast.TypeSwitchStmt:
		// v25.9: same multi-way treatment. The "tag" is the type-switch guard.
		// v26.58 (D059): the Init and the guard (`t := x.(type)`) were never
		// read, so their calls were lost and RawSource was empty. The guard text
		// now labels the node, and the calls in both come before the cases.
		n := &Node{Kind: NodeBranch, Language: LangGo, Name: "type-switch", RawSource: stmtText(st.Assign), Span: c.span(st.Pos(), st.End())}
		// v26.68: the Init and the guard run before the cases, so their calls
		// come before the node.
		pre := c.initStmt(st.Init)
		pre = append(pre, c.callsIn(st.Assign)...)
		n.Children = append(n.Children, c.caseClauses(st.Body.List)...)
		return append(pre, n)

	case *goast.SelectStmt:
		// v25.9: select is multi-way too; each CommClause becomes a case child
		// whose RawSource is the comm operation (or "default").
		n := &Node{Kind: NodeBranch, Language: LangGo, Name: "select", Span: c.span(st.Pos(), st.End())}
		for _, cl := range st.Body.List {
			cc, ok := cl.(*goast.CommClause)
			if !ok {
				continue
			}
			label := "default"
			if cc.Comm != nil {
				label = stmtText(cc.Comm)
			}
			caseNode := &Node{
				Kind: NodeBranch, Language: LangGo, Name: "case",
				RawSource: label,
				Span:      c.span(cc.Pos(), cc.End()),
			}
			caseNode.Children = append(caseNode.Children, c.convertStmtList(cc.Body)...)
			n.Children = append(n.Children, caseNode)
		}
		return []*Node{n}

	case *goast.ReturnStmt:
		n := &Node{Kind: NodeReturn, Language: LangGo, Span: c.span(st.Pos(), st.End())}
		for _, r := range st.Results {
			n.Children = append(n.Children, c.callsIn(r)...)
		}
		return []*Node{n}

	case *goast.DeferStmt:
		n := &Node{Kind: NodeDefer, Language: LangGo, Span: c.span(st.Pos(), st.End()), Callee: calleeName(st.Call.Fun)}
		// v26.58 (D060): the defer node held only the callee name, so the
		// call graph saw neither the deferred call nor the calls in its
		// arguments or in a deferred func literal. The calls go in as children.
		n.Children = c.callsIn(st.Call)
		return []*Node{n}

	case *goast.BlockStmt:
		return c.convertStmtList(st.List)

	case *goast.ExprStmt:
		// a bare expression statement, usually a call.
		// v26.58 (D060): the outer call alone lost the calls in its arguments
		// (outer(nested())). callsIn walks the whole expression and still
		// yields the outer call first, as AssignStmt already does.
		return c.callsIn(st.X)

	case *goast.AssignStmt:
		// assignment: emit one NodeAssign (named by its first LHS) plus any
		// calls on the RHS so the CFG line shows the real assignment.
		name := ""
		if len(st.Lhs) > 0 {
			name = exprText(st.Lhs[0])
		}
		n := &Node{Kind: NodeAssign, Language: LangGo, Name: name, Span: c.span(st.Pos(), st.End())}
		for _, r := range st.Rhs {
			n.Children = append(n.Children, c.callsIn(r)...)
		}
		return []*Node{n}

	case *goast.LabeledStmt:
		return c.convertStmt(st.Stmt)

	default:
		// Any other statement: surface calls within it so nothing is lost.
		var out []*Node
		goast.Inspect(s, func(nn goast.Node) bool {
			if call, ok := nn.(*goast.CallExpr); ok {
				out = append(out, c.callNode(call))
			}
			return true
		})
		return out
	}
}

// initStmt converts the Init of an if, for or switch (`if err := f(); ...`)
// into the nodes that run before the statement; nil when there is none.
func (c *goConverter) initStmt(s goast.Stmt) []*Node {
	if s == nil {
		return nil
	}
	return c.convertStmt(s)
}

// callNode builds a NodeCall from a call expression.
func (c *goConverter) callNode(call *goast.CallExpr) *Node {
	return &Node{
		Kind:     NodeCall,
		Language: LangGo,
		Span:     c.span(call.Pos(), call.End()),
		Callee:   calleeName(call.Fun),
	}
}

// FuncLiteralCallee is the Callee of a call whose function is an anonymous
// function literal invoked in place: `func() { ... }()`. Until v26.34 the
// whole literal's source text was the callee name, which can never resolve
// and which the verify stage then reported as an unresolved project call.
const FuncLiteralCallee = "func literal"

// calleeName renders the function part of a call by shape rather than by
// source text (v26.34):
//
//	helper()                      -> helper
//	pkg.F()                       -> pkg.F
//	s.mu.Lock()                   -> s.mu.Lock
//	json.NewDecoder(r).Decode()   -> json.NewDecoder().Decode
//	w.Header().Set()              -> w.Header().Set
//	func() { ... }()              -> func literal
//	g[int](x)                     -> g
//
// Argument lists are dropped from chained receivers, so the name is stable
// and short, and the first segment is always the identifier the resolver
// needs: a package alias or a receiver variable. Conversions such as
// []byte(x) keep the type text, as before.
func calleeName(fun goast.Expr) string {
	switch f := fun.(type) {
	case nil:
		return ""
	case *goast.Ident:
		return f.Name
	case *goast.SelectorExpr:
		base := calleeBase(f.X)
		if base == "" {
			return f.Sel.Name
		}
		return base + "." + f.Sel.Name
	case *goast.FuncLit:
		return FuncLiteralCallee
	case *goast.ParenExpr:
		return calleeName(f.X)
	case *goast.IndexExpr:
		return calleeName(f.X)
	case *goast.IndexListExpr:
		return calleeName(f.X)
	}
	return exprText(fun)
}

// calleeBase renders the receiver side of a selector without argument lists.
func calleeBase(x goast.Expr) string {
	switch b := x.(type) {
	case nil:
		return ""
	case *goast.Ident:
		return b.Name
	case *goast.SelectorExpr:
		base := calleeBase(b.X)
		if base == "" {
			return b.Sel.Name
		}
		return base + "." + b.Sel.Name
	case *goast.CallExpr:
		return calleeName(b.Fun) + "()"
	case *goast.ParenExpr:
		return calleeBase(b.X)
	case *goast.StarExpr:
		return calleeBase(b.X)
	case *goast.IndexExpr:
		return calleeBase(b.X)
	case *goast.FuncLit:
		return FuncLiteralCallee + "()"
	case *goast.CompositeLit:
		return exprText(b.Type) + "{}"
	}
	return exprText(x)
}

// callsIn returns NodeCall children for every call expression inside expr, so
// calls embedded in conditions / RHS / return values are still tracked.
func (c *goConverter) callsIn(expr goast.Node) []*Node {
	if expr == nil {
		return nil
	}
	var out []*Node
	goast.Inspect(expr, func(nn goast.Node) bool {
		if call, ok := nn.(*goast.CallExpr); ok {
			out = append(out, c.callNode(call))
		}
		return true
	})
	return out
}

// ── Type Expression Conversion ────────────────────────────────

func (c *goConverter) convertExpr(expr goast.Expr) TypeRef {
	if expr == nil {
		return UnknownType
	}
	switch t := expr.(type) {
	case *goast.Ident:
		return TypeRef{Name: t.Name}
	case *goast.StarExpr:
		inner := c.convertExpr(t.X)
		inner.IsPtr = true
		return inner
	case *goast.ArrayType:
		inner := c.convertExpr(t.Elt)
		inner.IsSlice = true
		return inner
	case *goast.MapType:
		key := c.convertExpr(t.Key)
		val := c.convertExpr(t.Value)
		val.IsMap = true
		val.KeyType = &key
		return val
	case *goast.SelectorExpr:
		pkg := exprText(t.X)
		return TypeRef{Name: t.Sel.Name, Package: pkg}
	case *goast.FuncType:
		return TypeRef{Name: "func", IsFunc: true}
	case *goast.InterfaceType:
		return TypeRef{Name: "interface{}"}
	case *goast.ChanType:
		return TypeRef{Name: "chan " + c.convertExpr(t.Value).String()}
	case *goast.Ellipsis:
		inner := c.convertExpr(t.Elt)
		inner.Name = "..." + inner.Name
		return inner
	default:
		return TypeRef{Name: exprText(expr)}
	}
}

func (c *goConverter) convertParams(field *goast.Field) []Parameter {
	typ := c.convertExpr(field.Type)
	variadic := false
	if _, ok := field.Type.(*goast.Ellipsis); ok {
		variadic = true
	}
	if len(field.Names) == 0 {
		return []Parameter{{Type: typ, Variadic: variadic}}
	}
	var params []Parameter
	for _, name := range field.Names {
		params = append(params, Parameter{
			Name:     name.Name,
			Type:     typ,
			Variadic: variadic,
		})
	}
	return params
}

// ─── Helpers ──────────────────────────────────────────────────

func isExported(name string) bool {
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

func docText(cg *goast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	var parts []string
	for _, c := range cg.List {
		parts = append(parts, strings.TrimPrefix(strings.TrimPrefix(c.Text, "//"), " "))
	}
	return strings.Join(parts, "\n")
}

func exprText(expr goast.Expr) string {
	if expr == nil {
		return ""
	}
	var buf bytes.Buffer
	_ = goformat.Node(&buf, gotoken.NewFileSet(), expr)
	return buf.String()
}

// stmtText renders a statement to source text (used for a select's comm clause,
// e.g. `v := <-ch`). v25.9.
func stmtText(st goast.Stmt) string {
	if st == nil {
		return ""
	}
	var buf bytes.Buffer
	_ = goformat.Node(&buf, gotoken.NewFileSet(), st)
	return strings.TrimSpace(buf.String())
}

// caseClauses converts a switch/type-switch body into one case node per clause.
// Each case node is a NodeBranch whose RawSource is the comma-joined case value
// (or "default" for the default clause) and whose Children are that clause's
// statements. This is the multi-way shape flow.Build fans out. It mirrors how
// COBOL's EVALUATE/WHEN is modelled. v25.9.
func (c *goConverter) caseClauses(list []goast.Stmt) []*Node {
	var out []*Node
	for _, cl := range list {
		cc, ok := cl.(*goast.CaseClause)
		if !ok {
			continue
		}
		label := "default"
		if len(cc.List) > 0 {
			parts := make([]string, 0, len(cc.List))
			for _, e := range cc.List {
				parts = append(parts, exprText(e))
			}
			label = strings.Join(parts, ", ")
		}
		caseNode := &Node{
			Kind: NodeBranch, Language: LangGo, Name: "case",
			RawSource: label,
			Span:      c.span(cc.Pos(), cc.End()),
		}
		caseNode.Children = append(caseNode.Children, c.convertStmtList(cc.Body)...)
		out = append(out, caseNode)
	}
	return out
}

func extractSource(src []byte, fset *gotoken.FileSet, start, end gotoken.Pos) string {
	if !start.IsValid() || !end.IsValid() {
		return ""
	}
	s := fset.Position(start).Offset
	e := fset.Position(end).Offset
	if s < 0 || e > len(src) || s >= e {
		return ""
	}
	return string(src[s:e])
}
