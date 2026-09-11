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

def _expr_flow(node, out):
    """v26.6 — EXPRESSION-LEVEL control flow. Walk an expression and emit branch
    markers for the constructs that actually change control flow WITHIN a
    statement, so the CFG shows them instead of hiding them inside one process box:

      - short-circuit  a and b   /  a or b   → each right-hand operand is
        conditionally evaluated, so it becomes a branch whose taken-arm is that
        operand's own sub-flow (nested a-and-b-and-c chains right-fold naturally).
      - ternary        x if c else y         → a two-arm branch (then=x, else=y).
      - comprehension  [f(x) for x in xs if p(x)] → the loop + its filter + the
        element expression become a loop with a nested filter branch.
      - calls anywhere in the expression are still surfaced (so a call inside a
        ternary arm or a comprehension body appears in the graph).

    Emits into 'out' (a list) and returns it. Purely additive: a plain expression
    with none of these yields only its calls, exactly as before."""
    if node is None:
        return out
    ln = getattr(node, 'lineno', 0); col = getattr(node, 'col_offset', 0)

    # short-circuit boolean: a and b and c  /  a or b
    if isinstance(node, ast.BoolOp):
        op = 'and' if isinstance(node.op, ast.And) else 'or'
        # first operand always evaluates; the rest are conditional on it.
        vals = node.values
        _expr_flow(vals[0], out)
        for v in vals[1:]:
            arm = []
            _expr_flow(v, arm)
            out.append({'kind': 'branch', 'name': op, 'line': getattr(v, 'lineno', ln),
                        'col': getattr(v, 'col_offset', col),
                        'cond': cond_text(node.values[0]) if op else '',
                        'expr': True, 'body': arm, 'orelse': []})
        return out

    # ternary: body if test else orelse
    if isinstance(node, ast.IfExp):
        thenb = []; elseb = []
        _expr_flow(node.body, thenb)
        _expr_flow(node.orelse, elseb)
        out.append({'kind': 'branch', 'name': 'if-expr', 'line': ln, 'col': col,
                    'cond': cond_text(node.test), 'expr': True,
                    'body': thenb, 'orelse': elseb})
        _expr_flow(node.test, out)
        return out

    # comprehensions: [elt for x in it if cond ...]  (list/set/dict/generator)
    if isinstance(node, (ast.ListComp, ast.SetComp, ast.GeneratorExp, ast.DictComp)):
        # outermost generator becomes the loop; its ifs become a nested filter.
        gens = node.generators
        body = []
        # element expression(s)
        if isinstance(node, ast.DictComp):
            _expr_flow(node.key, body); _expr_flow(node.value, body)
        else:
            _expr_flow(node.elt, body)
        # fold inner generators + filters into the body (innermost first)
        for gen in reversed(gens):
            filt = []
            for cnd in gen.ifs:
                fbody = body
                body = []
                body.append({'kind': 'branch', 'name': 'comp-if', 'line': ln, 'col': col,
                             'cond': cond_text(cnd), 'expr': True, 'body': fbody, 'orelse': []})
                _expr_flow(cnd, body)
            loopn = {'kind': 'loop', 'name': 'comp-for', 'line': ln, 'col': col,
                     'cond': 'for ' + cond_text(gen.target) + ' in ' + cond_text(gen.iter),
                     'expr': True, 'body': body}
            body = [loopn]
        out.extend(body)
        return out

    # walrus (assignment expression): named := value — surface the value's flow.
    if hasattr(ast, 'NamedExpr') and isinstance(node, ast.NamedExpr):
        _expr_flow(node.value, out)
        return out

    # a call is a leaf we care about
    if isinstance(node, ast.Call):
        out.append({'kind': 'call', 'callee': safe_name(node.func), 'line': ln, 'col': col})
        for a in list(node.args) + [k.value for k in node.keywords]:
            _expr_flow(a, out)
        return out

    if isinstance(node, ast.Await):
        return _expr_flow(node.value, out)

    # generic: descend into child expressions so nested calls/ternaries surface.
    for child in ast.iter_child_nodes(node):
        if isinstance(child, ast.expr):
            _expr_flow(child, out)
    return out


def expr_nodes(node):
    """Expression-level flow nodes for one expression, as a list."""
    return _expr_flow(node, [])


def extract_body_nodes(stmts):
    """v26.6 — walk function body statements and emit a FAITHFUL, NESTED IR:
    branches carry their then-body in 'body' and else-body in 'orelse'; loops
    carry their body in 'body'; try carries body + handler/else/finally in
    'orelse'. This replaces the old flattened emission (which appended nested
    bodies as siblings and lost the branching topology entirely). Expression-level
    flow inside conditions and statements is folded in via expr_nodes()."""
    out = []
    for stmt in stmts:
        ln = getattr(stmt, 'lineno', 0)
        col = getattr(stmt, 'col_offset', 0)

        if isinstance(stmt, ast.If):
            # the test itself can contain expression-level control flow
            pre = expr_nodes(stmt.test)
            out.extend(pre)
            out.append({'kind': 'branch', 'name': 'if', 'line': ln, 'col': col,
                        'cond': cond_text(stmt.test),
                        'body': extract_body_nodes(stmt.body),
                        'orelse': extract_body_nodes(stmt.orelse)})

        elif isinstance(stmt, (ast.For, ast.AsyncFor)):
            _t = cond_text(stmt.target); _i = cond_text(stmt.iter)
            body = extract_body_nodes(stmt.body)
            if stmt.orelse:
                # for/else: the else runs when the loop completes without break.
                body_else = extract_body_nodes(stmt.orelse)
            else:
                body_else = []
            out.extend(expr_nodes(stmt.iter))
            out.append({'kind': 'loop', 'name': 'for', 'line': ln, 'col': col,
                        'cond': ('for ' + _t + ' in ' + _i) if (_t or _i) else '',
                        'body': body, 'orelse': body_else})

        elif isinstance(stmt, ast.While):
            out.extend(expr_nodes(stmt.test))
            out.append({'kind': 'loop', 'name': 'while', 'line': ln, 'col': col,
                        'cond': cond_text(stmt.test),
                        'body': extract_body_nodes(stmt.body),
                        'orelse': extract_body_nodes(stmt.orelse) if stmt.orelse else []})

        elif isinstance(stmt, ast.Try):
            handlers = []
            for handler in stmt.handlers:
                handlers.extend(extract_body_nodes(handler.body))
            if stmt.orelse:
                handlers.extend(extract_body_nodes(stmt.orelse))
            if stmt.finalbody:
                handlers.extend(extract_body_nodes(stmt.finalbody))
            out.append({'kind': 'try', 'name': 'try', 'line': ln, 'col': col,
                        'body': extract_body_nodes(stmt.body),
                        'orelse': handlers})

        elif isinstance(stmt, ast.Match) if hasattr(ast, 'Match') else False:
            # Python 3.10 match/case → multi-way branch (like a switch/EVALUATE):
            # a parent branch whose Children are per-case arms, each with its own
            # body. RawSource on the parent = the subject; on each arm = the pattern.
            arms = []
            for case in stmt.cases:
                arms.append({'kind': 'branch', 'name': 'case', 'line': getattr(case, 'lineno', ln),
                             'col': col, 'cond': _match_pattern_text(case.pattern),
                             'body': extract_body_nodes(case.body), 'orelse': []})
            out.append({'kind': 'match', 'name': 'match', 'line': ln, 'col': col,
                        'cond': cond_text(stmt.subject), 'cases': arms})

        elif isinstance(stmt, (ast.With, ast.AsyncWith)):
            # with-context: the items can contain calls; body flows normally.
            for item in stmt.items:
                out.extend(expr_nodes(item.context_expr))
            out.extend(extract_body_nodes(stmt.body))

        elif isinstance(stmt, ast.Return):
            if stmt.value is not None:
                out.extend(expr_nodes(stmt.value))
            out.append({'kind': 'return', 'name': 'return', 'line': ln, 'col': col})

        elif isinstance(stmt, ast.Raise):
            out.append({'kind': 'return', 'name': 'raise', 'line': ln, 'col': col})

        elif isinstance(stmt, ast.Expr):
            out.extend(expr_nodes(stmt.value))

        elif isinstance(stmt, (ast.Assign, ast.AnnAssign, ast.AugAssign)):
            val = getattr(stmt, 'value', None)
            if val is not None:
                out.extend(expr_nodes(val))

    return out


def _match_pattern_text(pat):
    """Best-effort text for a match/case pattern (Python 3.10+)."""
    try:
        return cond_text(pat)
    except Exception:
        return 'case'

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
	Kind        string           `json:"kind"`
	Name        string           `json:"name"`
	Async       bool             `json:"async"`
	Line        int              `json:"line"`
	Col         int              `json:"col"`
	EndLine     int              `json:"end_line"`
	Params      []pyParamJSON    `json:"params"`
	Returns     pyTypeJSON       `json:"returns"`
	Doc         string           `json:"doc"`
	Exported    bool             `json:"exported"`
	Bases       []string         `json:"bases"`
	Fields      []pyFieldJSON    `json:"fields"`
	Methods     []string         `json:"methods"`
	Path        string           `json:"path"`
	Alias       string           `json:"alias"`
	ImportNames []string         `json:"import_names"`
	Type        pyTypeJSON       `json:"type"`
	Value       string           `json:"value"`
	BodyNodes   []pyBodyNodeJSON `json:"body_nodes"` // v19 TASK 2
}

// pyBodyNodeJSON represents one structural node inside a function body. v26.6:
// now NESTED — a branch carries its then-body in Body and else-body in Orelse; a
// loop carries its body in Body; a try carries body + handler/else/finally in
// Orelse; a match carries its case-arms in Cases. Expr marks nodes that came from
// expression-level flow (short-circuit / ternary / comprehension) so the CFG can
// style them distinctly if desired.
type pyBodyNodeJSON struct {
	Kind   string `json:"kind"`   // "branch" | "loop" | "return" | "call" | "try" | "match"
	Name   string `json:"name"`   // keyword name or empty for calls
	Callee string `json:"callee"` // for kind=="call"
	Cond   string `json:"cond"`   // loop/branch/match condition text for the CFG label
	Line   int    `json:"line"`
	Col    int    `json:"col"`
	Expr   bool   `json:"expr"` // v26.6: from expression-level flow

	// v26.6 nested structure (matches the CFG builder's contract: Children/Else).
	Body   []pyBodyNodeJSON `json:"body"`   // then-body / loop-body / try-body
	Orelse []pyBodyNodeJSON `json:"orelse"` // else-body / catch+finally
	Cases  []pyBodyNodeJSON `json:"cases"`  // match/case arms (each a branch)
}

// pyBodyToNodes converts the Python analysis script's nested body_nodes into the
// unified IR the CFG builder consumes. The mapping mirrors the Go/Java frontends
// exactly so buildStructuralCFG/processNode produce a real graph:
//
//	branch → NodeBranch{RawSource:cond, Children:body, Else:orelse}
//	loop   → NodeLoop  {RawSource:cond, Children:body}   (Orelse folded after)
//	try    → NodeTry   {Children:body, Else:handlers}
//	match  → NodeBranch multi-way: RawSource=subject, Children=case-arm branches
//	return → NodeReturn ; call → NodeCall
//
// Recurses so arbitrarily nested control flow (an if inside a loop inside a try,
// a ternary inside a comprehension) is preserved.
func pyBodyToNodes(bns []pyBodyNodeJSON) []*Node {
	var out []*Node
	for _, bn := range bns {
		bspan := Span{Start: Pos{Line: bn.Line, Column: bn.Col + 1}}
		switch bn.Kind {
		case "branch":
			out = append(out, &Node{
				Kind:      NodeBranch,
				Name:      bn.Name,
				Language:  LangPython,
				RawSource: bn.Cond, // condition → decision label; empty is fine
				Span:      bspan,
				Children:  pyBodyToNodes(bn.Body),
				Else:      pyBodyToNodes(bn.Orelse),
			})
		case "loop":
			loop := &Node{
				Kind:      NodeLoop,
				Name:      bn.Name,
				Language:  LangPython,
				RawSource: bn.Cond,
				Span:      bspan,
				Children:  pyBodyToNodes(bn.Body),
			}
			out = append(out, loop)
			// for/while-else runs after the loop completes without break; emit it
			// as straight-line successors so it appears after the loop in the CFG.
			if len(bn.Orelse) > 0 {
				out = append(out, pyBodyToNodes(bn.Orelse)...)
			}
		case "try":
			out = append(out, &Node{
				Kind:     NodeTry,
				Name:     bn.Name,
				Language: LangPython,
				Span:     bspan,
				Children: pyBodyToNodes(bn.Body),
				Else:     pyBodyToNodes(bn.Orelse),
			})
		case "match":
			// Multi-way: parent NodeBranch whose RawSource is the subject and whose
			// Children are per-case NodeBranch arms (each RawSource=pattern). This
			// is exactly the shape isMultiWayBranch/processNode fan out (like Go
			// switch / COBOL EVALUATE), so match/case gets the v26.1 lane-column
			// layout for free.
			arms := make([]*Node, 0, len(bn.Cases))
			for _, cs := range bn.Cases {
				arms = append(arms, &Node{
					Kind:      NodeBranch,
					Name:      "case",
					Language:  LangPython,
					RawSource: cs.Cond,
					Span:      Span{Start: Pos{Line: cs.Line, Column: cs.Col + 1}},
					Children:  pyBodyToNodes(cs.Body),
				})
			}
			out = append(out, &Node{
				Kind:      NodeBranch,
				Name:      "match",
				Language:  LangPython,
				RawSource: bn.Cond,
				Span:      bspan,
				Children:  arms,
			})
		case "return":
			out = append(out, &Node{
				Kind:     NodeReturn,
				Name:     bn.Name,
				Language: LangPython,
				Span:     bspan,
			})
		case "call":
			out = append(out, &Node{
				Kind:     NodeCall,
				Callee:   bn.Callee,
				Language: LangPython,
				Span:     bspan,
			})
		}
	}
	return out
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
			// Build children: opaque body marker + structural nodes.
			// v26.6: the conversion is now RECURSIVE — pyBodyToNodes builds the
			// nested NodeBranch{Children,Else}/NodeLoop{Children}/NodeTry/multi-way
			// tree the CFG builder already knows how to walk (the same shape Go and
			// Java emit), so Python renders a faithful control-flow graph instead of
			// a flat list of markers.
			children := []*Node{{
				Kind:          NodeUnknown,
				UnknownReason: "body_not_captured",
			}}
			children = append(children, pyBodyToNodes(jn.BodyNodes)...)
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
