// py_frontend.go — TASK 2 (v19): Python analysis script extended to emit
// body_nodes (calls, branches, loops, returns, raises) for function bodies.
// pyParseOutput now converts them to NodeBranch/NodeLoop/NodeReturn/NodeCall.
package ast

// ─────────────────────────────────────────────────────────────
// py_frontend.go — Python language frontend
//
// Offline strategy: spawn python3 (or python) with an embedded
// analysis script that uses Python's own ast module.  This gives
// us the full Python AST without any third-party dependency.
//
// If python3/python is not available, Available() returns false
// and Parse produces a single NodeUnknown with reason
// "python_runtime_unavailable".
// ─────────────────────────────────────────────────────────────

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// PyFrontend implements Frontend for Python source files.
type PyFrontend struct {
	pythonBin string // cached path after first Available() call
}

func init() { Register(&PyFrontend{}) }

func (py *PyFrontend) Language() Language   { return LangPython }
func (py *PyFrontend) Extensions() []string { return []string{".py", ".pyw"} }

func (py *PyFrontend) Available() bool {
	if py.pythonBin != "" {
		return true
	}
	for _, candidate := range []string{"python3", "python"} {
		if path, err := exec.LookPath(candidate); err == nil {
			py.pythonBin = path
			return true
		}
	}
	return false
}

// ─── Embedded analysis script ────────────────────────────────

// pyAnalysisScript is embedded inline so there are no external files.
// It uses Python's own ast module (stdlib since Python 2.6).
// v19 (TASK 2): function nodes now include a 'body_nodes' list with
// structural control-flow items (branch, loop, return, raise, call).
const pyAnalysisScript = `
import ast, json, sys

def safe_name(node):
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Attribute):
        return safe_name(node.value) + '.' + node.attr
    return '?'

def cond_text(node):
    """Best-effort source text for a condition/iter (v23.49). Uses ast.unparse
    when available (py3.9+); falls back to a compact manual rendering."""
    if node is None:
        return ''
    try:
        return ast.unparse(node)
    except Exception:
        pass
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Attribute):
        return safe_name(node)
    if isinstance(node, ast.Compare):
        return safe_name(node.left) if hasattr(node, 'left') else '?'
    return '?'

def type_annotation(node):
    if node is None:
        return {'name': ''}
    if isinstance(node, ast.Name):
        return {'name': node.id}
    if isinstance(node, ast.Attribute):
        return {'name': node.attr, 'package': safe_name(node.value)}
    if isinstance(node, ast.Subscript):
        base = type_annotation(node.value)
        base['is_slice'] = True
        return base
    if isinstance(node, ast.BinOp):
        return {'name': '?', 'note': 'union_type'}
    if isinstance(node, ast.Constant):
        return {'name': str(node.value)}
    return {'name': '?'}

def extract_body_nodes(stmts):
    """Walk function body statements and emit structural IR nodes.
    Recursively descends into branches/loops to capture nested control flow."""
    out = []
    for stmt in stmts:
        ln = getattr(stmt, 'lineno', 0)
        col = getattr(stmt, 'col_offset', 0)

        if isinstance(stmt, (ast.If,)):
            out.append({'kind': 'branch', 'name': 'if', 'line': ln, 'col': col})
            out.extend(extract_body_nodes(stmt.body))
            if stmt.orelse:
                out.extend(extract_body_nodes(stmt.orelse))

        elif isinstance(stmt, (ast.For, ast.AsyncFor)):
            _t = cond_text(stmt.target); _i = cond_text(stmt.iter)
            out.append({'kind': 'loop', 'name': 'for', 'line': ln, 'col': col,
                        'cond': ('for ' + _t + ' in ' + _i) if (_t or _i) else ''})
            out.extend(extract_body_nodes(stmt.body))
            if stmt.orelse:
                out.extend(extract_body_nodes(stmt.orelse))

        elif isinstance(stmt, ast.While):
            out.append({'kind': 'loop', 'name': 'while', 'line': ln, 'col': col,
                        'cond': cond_text(stmt.test)})
            out.extend(extract_body_nodes(stmt.body))
            if stmt.orelse:
                out.extend(extract_body_nodes(stmt.orelse))

        elif isinstance(stmt, ast.Try):
            out.append({'kind': 'branch', 'name': 'try', 'line': ln, 'col': col})
            out.extend(extract_body_nodes(stmt.body))
            for handler in stmt.handlers:
                out.extend(extract_body_nodes(handler.body))
            if stmt.finalbody:
                out.extend(extract_body_nodes(stmt.finalbody))
            if stmt.orelse:
                out.extend(extract_body_nodes(stmt.orelse))

        elif isinstance(stmt, ast.With) or isinstance(stmt, ast.AsyncWith):
            out.extend(extract_body_nodes(stmt.body))

        elif isinstance(stmt, ast.Return):
            out.append({'kind': 'return', 'name': 'return', 'line': ln, 'col': col})

        elif isinstance(stmt, ast.Raise):
            out.append({'kind': 'return', 'name': 'raise', 'line': ln, 'col': col})

        elif isinstance(stmt, (ast.Expr,)):
            # Bare expression statement — look for calls.
            val = stmt.value
            if isinstance(val, ast.Call):
                callee = safe_name(val.func)
                out.append({'kind': 'call', 'callee': callee, 'line': ln, 'col': col})
            elif isinstance(val, (ast.Await,)):
                inner = val.value
                if isinstance(inner, ast.Call):
                    callee = safe_name(inner.func)
                    out.append({'kind': 'call', 'callee': callee, 'line': ln, 'col': col})

        elif isinstance(stmt, ast.Assign):
            # Assignment with RHS call: e.g. result = foo()
            if isinstance(stmt.value, ast.Call):
                callee = safe_name(stmt.value.func)
                out.append({'kind': 'call', 'callee': callee, 'line': ln, 'col': col})

    return out

def analyze(source, filepath):
    try:
        tree = ast.parse(source, filename=filepath)
    except SyntaxError as e:
        return {'error': str(e), 'nodes': []}

    nodes = []

    for node in ast.walk(tree):
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            params = []
            for arg in node.args.args:
                params.append({
                    'name': arg.arg,
                    'type': type_annotation(arg.annotation) if arg.annotation else {'name': ''}
                })
            if node.args.vararg:
                params.append({'name': '*' + node.args.vararg.arg, 'variadic': True, 'type': {'name': ''}})
            returns = type_annotation(node.returns) if node.returns else {'name': ''}
            doc = ast.get_docstring(node) or ''
            body_nodes = extract_body_nodes(node.body)
            nodes.append({
                'kind': 'function',
                'name': node.name,
                'async': isinstance(node, ast.AsyncFunctionDef),
                'line': node.lineno,
                'col': node.col_offset,
                'end_line': node.end_lineno if hasattr(node, 'end_lineno') else node.lineno,
                'params': params,
                'returns': returns,
                'doc': doc,
                'exported': not node.name.startswith('_'),
                'body_nodes': body_nodes,
            })

        elif isinstance(node, ast.ClassDef):
            fields = []
            methods = []
            for item in node.body:
                if isinstance(item, (ast.FunctionDef, ast.AsyncFunctionDef)):
                    methods.append(item.name)
                elif isinstance(item, ast.AnnAssign):
                    if isinstance(item.target, ast.Name):
                        fields.append({
                            'name': item.target.id,
                            'type': type_annotation(item.annotation),
                            'optional': False,
                            'exported': not item.target.id.startswith('_'),
                        })
            bases = [safe_name(b) for b in node.bases]
            doc = ast.get_docstring(node) or ''
            nodes.append({
                'kind': 'class',
                'name': node.name,
                'line': node.lineno,
                'col': node.col_offset,
                'end_line': node.end_lineno if hasattr(node, 'end_lineno') else node.lineno,
                'bases': bases,
                'fields': fields,
                'methods': methods,
                'doc': doc,
                'exported': not node.name.startswith('_'),
            })

        elif isinstance(node, ast.Import):
            for alias in node.names:
                nodes.append({
                    'kind': 'import',
                    'name': alias.asname if alias.asname else alias.name,
                    'path': alias.name,
                    'alias': alias.asname or '',
                    'line': node.lineno,
                    'col': node.col_offset,
                })

        elif isinstance(node, ast.ImportFrom):
            module = node.module or ''
            for alias in node.names:
                nodes.append({
                    'kind': 'import',
                    'name': alias.asname if alias.asname else alias.name,
                    'path': module,
                    'alias': alias.asname or '',
                    'import_names': [alias.name],
                    'line': node.lineno,
                    'col': node.col_offset,
                })

        elif isinstance(node, ast.AnnAssign):
            if isinstance(node.target, ast.Name):
                # Only top-level annotated assignments
                if hasattr(node, 'col_offset') and node.col_offset == 0:
                    val = ''
                    if isinstance(node.value, ast.Constant):
                        val = repr(node.value.value)
                    nodes.append({
                        'kind': 'variable',
                        'name': node.target.id,
                        'type': type_annotation(node.annotation),
                        'value': val,
                        'line': node.lineno,
                        'col': node.col_offset,
                        'exported': not node.target.id.startswith('_'),
                    })

        elif isinstance(node, ast.Assign):
            # Top-level simple assignments (constants)
            if hasattr(node, 'col_offset') and node.col_offset == 0:
                for target in node.targets:
                    if isinstance(target, ast.Name):
                        val = ''
                        if isinstance(node.value, ast.Constant):
                            val = repr(node.value.value)
                        nodes.append({
                            'kind': 'variable',
                            'name': target.id,
                            'type': {'name': ''},
                            'value': val,
                            'line': node.lineno,
                            'col': 0,
                            'exported': not target.id.startswith('_'),
                        })

    return {'nodes': nodes}

source = sys.stdin.buffer.read().decode('utf-8', errors='replace')
filepath_arg = sys.argv[1] if len(sys.argv) > 1 else '<unknown>'
result = analyze(source, filepath_arg)
print(json.dumps(result))
`

// ─── Parse ───────────────────────────────────────────────────

func (py *PyFrontend) Parse(filePath string, source []byte) (*Module, error) {
	sum := sha256.Sum256(source)
	m := &Module{
		Language:  LangPython,
		FilePath:  filePath,
		Checksum:  fmt.Sprintf("%x", sum),
		RawSource: source,
	}

	// Derive module name from file path
	base := filePath
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	m.PackageName = strings.TrimSuffix(strings.TrimSuffix(base, ".pyw"), ".py")

	if !py.Available() {
		m.Nodes = []*Node{{
			Kind:          NodeUnknown,
			Language:      LangPython,
			Span:          Span{Start: Pos{Line: 1, Column: 1}},
			UnknownReason: "python_runtime_unavailable",
			RawSource:     string(source[:min(len(source), 80)]),
		}}
		m.ParseErrors = []ParseError{{
			Message:     "python3/python not found in PATH; cannot parse .py files",
			Recoverable: false,
		}}
		return m, nil
	}

	// Run analysis script via subprocess
	cmd := exec.Command(py.pythonBin, "-", filePath)
	cmd.Stdin = strings.NewReader(pyAnalysisScript + "\n")

	// Feed source to stdin of the python interpreter — but we need the
	// script to also read from stdin. Work around by writing script to
	// a temp file approach: embed source in the script call differently.
	// Better: pipe source via stdin by using -c flag with the script.

	cmd2 := exec.Command(py.pythonBin, "-c", pyAnalysisScript, filePath)
	cmd2.Stdin = bytes.NewReader(source)
	out, err := cmd2.Output()
	if err != nil {
		m.ParseErrors = append(m.ParseErrors, ParseError{
			Message:     fmt.Sprintf("python analysis script failed: %v", err),
			Recoverable: false,
		})
		m.Nodes = []*Node{{
			Kind:          NodeUnknown,
			Language:      LangPython,
			UnknownReason: "analysis_script_failed",
		}}
		return m, nil
	}

	nodes, errs := pyParseOutput(filePath, out)
	m.Nodes = nodes
	m.ParseErrors = append(m.ParseErrors, errs...)
	return m, nil
}

// ─── Output Parser ───────────────────────────────────────────

type pyNodeJSON struct {
	Kind        string          `json:"kind"`
	Name        string          `json:"name"`
	Async       bool            `json:"async"`
	Line        int             `json:"line"`
	Col         int             `json:"col"`
	EndLine     int             `json:"end_line"`
	Params      []pyParamJSON   `json:"params"`
	Returns     pyTypeJSON      `json:"returns"`
	Doc         string          `json:"doc"`
	Exported    bool            `json:"exported"`
	Bases       []string        `json:"bases"`
	Fields      []pyFieldJSON   `json:"fields"`
	Methods     []string        `json:"methods"`
	Path        string          `json:"path"`
	Alias       string          `json:"alias"`
	ImportNames []string        `json:"import_names"`
	Type        pyTypeJSON      `json:"type"`
	Value       string          `json:"value"`
	BodyNodes   []pyBodyNodeJSON `json:"body_nodes"` // v19 TASK 2
}

// pyBodyNodeJSON represents one structural node inside a function body.
type pyBodyNodeJSON struct {
	Kind   string `json:"kind"`   // "branch", "loop", "return", "call"
	Name   string `json:"name"`   // keyword name or empty for calls
	Callee string `json:"callee"` // for kind=="call"
	Cond   string `json:"cond"`   // v23.49: loop/branch condition text for the CFG label
	Line   int    `json:"line"`
	Col    int    `json:"col"`
}

type pyParamJSON struct {
	Name     string     `json:"name"`
	Type     pyTypeJSON `json:"type"`
	Variadic bool       `json:"variadic"`
}

type pyTypeJSON struct {
	Name    string `json:"name"`
	Package string `json:"package"`
	IsSlice bool   `json:"is_slice"`
}

type pyFieldJSON struct {
	Name     string     `json:"name"`
	Type     pyTypeJSON `json:"type"`
	Optional bool       `json:"optional"`
	Exported bool       `json:"exported"`
}

type pyOutput struct {
	Error string       `json:"error"`
	Nodes []pyNodeJSON `json:"nodes"`
}

func pyParseOutput(filePath string, raw []byte) ([]*Node, []ParseError) {
	var result pyOutput
	if err := json.Unmarshal(raw, &result); err != nil {
		return []*Node{{
			Kind:          NodeUnknown,
			Language:      LangPython,
			UnknownReason: "json_decode_error: " + err.Error(),
		}}, []ParseError{{Message: err.Error(), Recoverable: false}}
	}

	if result.Error != "" {
		return []*Node{{
			Kind:          NodeUnknown,
			Language:      LangPython,
			UnknownReason: "python_syntax_error: " + result.Error,
		}}, []ParseError{{Message: result.Error, Recoverable: false}}
	}

	var nodes []*Node
	var errs []ParseError

	for _, jn := range result.Nodes {
		span := Span{Start: Pos{Line: jn.Line, Column: jn.Col + 1}}
		if jn.EndLine > 0 {
			span.End = Pos{Line: jn.EndLine}
		}

		switch jn.Kind {
		case "function":
			n := &Node{
				ID:         "func:" + jn.Name,
				Kind:       NodeFunction,
				Name:       jn.Name,
				Language:   LangPython,
				Span:       span,
				Exported:   jn.Exported,
				Async:      jn.Async,
				DocComment: jn.Doc,
				Returns:    []TypeRef{pyTypeToRef(jn.Returns)},
			}
			for _, p := range jn.Params {
				n.Params = append(n.Params, Parameter{
					Name:     p.Name,
					Type:     pyTypeToRef(p.Type),
					Variadic: p.Variadic,
				})
			}
			// Build children: opaque body marker + structural nodes (v19 TASK 2).
			children := []*Node{{
				Kind:          NodeUnknown,
				UnknownReason: "body_not_captured",
			}}
			for _, bn := range jn.BodyNodes {
				bspan := Span{Start: Pos{Line: bn.Line, Column: bn.Col + 1}}
				switch bn.Kind {
				case "branch":
					children = append(children, &Node{
						Kind:     NodeBranch,
						Name:     bn.Name,
						Language: LangPython,
						Span:     bspan,
					})
				case "loop":
					children = append(children, &Node{
						Kind:      NodeLoop,
						Name:      bn.Name,
						Language:  LangPython,
						RawSource: bn.Cond, // v23.49: loop condition for the CFG label
						Span:      bspan,
					})
				case "return":
					children = append(children, &Node{
						Kind:     NodeReturn,
						Name:     bn.Name,
						Language: LangPython,
						Span:     bspan,
					})
				case "call":
					children = append(children, &Node{
						Kind:     NodeCall,
						Callee:   bn.Callee,
						Language: LangPython,
						Span:     bspan,
					})
				}
			}
			n.Children = children
			nodes = append(nodes, n)

		case "class":
			n := &Node{
				ID:         "class:" + jn.Name,
				Kind:       NodeClass,
				Name:       jn.Name,
				Language:   LangPython,
				Span:       span,
				Exported:   jn.Exported,
				DocComment: jn.Doc,
			}
			if len(jn.Bases) > 0 {
				n.Extends = jn.Bases[0]
				if len(jn.Bases) > 1 {
					n.Implements = jn.Bases[1:]
				}
			}
			for _, f := range jn.Fields {
				n.Fields = append(n.Fields, Field{
					Name:     f.Name,
					Type:     pyTypeToRef(f.Type),
					Optional: f.Optional,
					Exported: f.Exported,
				})
			}
			// Methods listed by name only
			for _, mname := range jn.Methods {
				n.Children = append(n.Children, &Node{
					ID:       "method:" + jn.Name + "." + mname,
					Kind:     NodeMethod,
					Name:     mname,
					Language: LangPython,
				})
			}
			nodes = append(nodes, n)

		case "import":
			n := &Node{
				ID:          "import:" + jn.Path,
				Kind:        NodeImport,
				Name:        jn.Name,
				Language:    LangPython,
				Span:        span,
				ImportPath:  jn.Path,
				ImportAlias: jn.Alias,
				ImportNames: jn.ImportNames,
			}
			nodes = append(nodes, n)

		case "variable":
			n := &Node{
				ID:           "var:" + jn.Name,
				Kind:         NodeVariable,
				Name:         jn.Name,
				Language:     LangPython,
				Span:         span,
				Exported:     jn.Exported,
				TypeHint:     pyTypeToRef(jn.Type),
				ValueLiteral: jn.Value,
			}
			nodes = append(nodes, n)

		default:
			nodes = append(nodes, &Node{
				Kind:          NodeUnknown,
				Language:      LangPython,
				Span:          span,
				UnknownReason: "unrecognized_kind:" + jn.Kind,
			})
		}
	}

	return nodes, errs
}

func pyTypeToRef(t pyTypeJSON) TypeRef {
	if t.Name == "" || t.Name == "?" {
		return UnknownType
	}
	return TypeRef{Name: t.Name, Package: t.Package, IsSlice: t.IsSlice}
}

// Emit returns the original source bytes (Python files not reformatted).
func (py *PyFrontend) Emit(module *Module) ([]byte, error) {
	return module.RawSource, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
