// generator/internal/style/analyze.go — v26.33: the shared view of a Go tree
// that both Derive and Check read. Everything comes from the Understand engine
// (symbols, spans, control flow graphs) plus the file text it already holds;
// nothing here re-parses Go with a second toolchain. That is the point: the
// same graph that answers "what calls what" also answers "what does this team's
// code look like", and the gate and the prompt read one snapshot.
package style

import (
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	agentctx "gen-system/generator/internal/context"
	"gen-system/generator/internal/flow"
	"gen-system/generator/internal/symbols"
)

type funcInfo struct {
	ID           string
	Name         string
	File         string // relative, forward slashes
	Line         int
	Lines        int
	Exported     bool
	IsMethod     bool
	IsTest       bool
	Receiver     string // receiver variable name, "" for functions
	ReceiverType string
	Params       int
	Layer        string
	Branches     int
	Body         string // source text of the whole declaration
}

type typeInfo struct {
	ID         string
	Name       string
	File       string
	Line       int
	Exported   bool
	IsTest     bool
	Layer      string
	HasMethods bool // at least one method has this receiver type
}

type snapshot struct {
	root  string
	files map[string]string // relative slash path -> content, Go files only
	funcs []funcInfo
	types []typeInfo
	dirs  map[string]bool // relative slash dirs that hold Go files
}

// load reads root through the Understand engine and builds the snapshot.
// Output order is fixed (sorted by file then line) so every caller is
// deterministic.
func load(root string) *snapshot {
	ctx := agentctx.LoadContext(root)
	return fromContext(ctx)
}

func fromContext(ctx agentctx.Context) *snapshot {
	s := &snapshot{root: ctx.Root, files: map[string]string{}, dirs: map[string]bool{}}
	for rel, content := range ctx.Files {
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		s.files[rel] = content
		d := path.Dir(rel)
		if d == "." {
			d = ""
		}
		s.dirs[d] = true
	}
	if ctx.Index == nil {
		return s
	}
	cfgByFunc := map[string]*flow.CFG{}
	for _, set := range ctx.CFGs {
		for id, c := range set {
			if c != nil {
				cfgByFunc[id] = c
			}
		}
	}
	for _, sym := range ctx.Index.AllSymbols() {
		if sym == nil || sym.Provenance != symbols.ProvProject {
			continue
		}
		rel := relOf(ctx.Root, sym.DefinedIn)
		content, ok := s.files[rel]
		if !ok {
			continue
		}
		switch sym.Kind {
		case symbols.SymFunc, symbols.SymMethod:
			body := declText(content, sym.Span.Start.Line, sym.Span.End.Line)
			f := funcInfo{
				ID:           sym.ID,
				Name:         sym.Name,
				File:         rel,
				Line:         sym.Span.Start.Line,
				Lines:        strings.Count(body, "\n") + 1,
				Exported:     sym.Exported,
				IsMethod:     sym.Kind == symbols.SymMethod || sym.ReceiverType != "",
				IsTest:       strings.HasSuffix(rel, "_test.go"),
				ReceiverType: strings.TrimPrefix(sym.ReceiverType, "*"),
				Params:       len(sym.Params),
				Layer:        layerOf(rel),
				Body:         body,
			}
			if f.Name == "" {
				continue
			}
			f.Receiver = receiverVar(body)
			if c, ok := cfgByFunc[sym.ID]; ok {
				f.Branches = branchesFromCFG(c)
			} else {
				f.Branches = branchesFromText(body)
			}
			s.funcs = append(s.funcs, f)
		case symbols.SymType:
			s.types = append(s.types, typeInfo{
				ID: sym.ID, Name: sym.Name, File: rel, Line: sym.Span.Start.Line,
				Exported: sym.Exported, IsTest: strings.HasSuffix(rel, "_test.go"),
				Layer: layerOf(rel),
			})
		}
	}
	receivers := map[string]bool{}
	for _, f := range s.funcs {
		if f.IsMethod && f.ReceiverType != "" {
			receivers[f.ReceiverType] = true
		}
	}
	for i := range s.types {
		s.types[i].HasMethods = receivers[s.types[i].Name]
	}
	sort.Slice(s.funcs, func(i, j int) bool {
		if s.funcs[i].File != s.funcs[j].File {
			return s.funcs[i].File < s.funcs[j].File
		}
		if s.funcs[i].Line != s.funcs[j].Line {
			return s.funcs[i].Line < s.funcs[j].Line
		}
		return s.funcs[i].ID < s.funcs[j].ID
	})
	sort.Slice(s.types, func(i, j int) bool {
		if s.types[i].File != s.types[j].File {
			return s.types[i].File < s.types[j].File
		}
		if s.types[i].Line != s.types[j].Line {
			return s.types[i].Line < s.types[j].Line
		}
		return s.types[i].ID < s.types[j].ID
	})
	return s
}

func relOf(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

// layerOf mirrors the directory convention the symbol index uses for
// Symbol.Layer, so a profile written against one agrees with the other.
func layerOf(rel string) string {
	lower := "/" + strings.ToLower(rel)
	switch {
	case strings.Contains(lower, "/model/") || strings.HasSuffix(lower, "_model.go"):
		return "model"
	case strings.Contains(lower, "/repository/") || strings.Contains(lower, "/repo/") || strings.HasSuffix(lower, "_repo.go"):
		return "repo"
	case strings.Contains(lower, "/service/") || strings.HasSuffix(lower, "_service.go"):
		return "service"
	case strings.Contains(lower, "/handler/") || strings.HasSuffix(lower, "_handler.go"):
		return "handler"
	case strings.Contains(lower, "/validation/") || strings.HasSuffix(lower, "_validation.go"):
		return "validation"
	}
	return ""
}

// declText returns the source lines of a declaration. When the span has a
// real end it is used; otherwise the body is found by brace counting from the
// start line (string and comment aware enough for generated Go).
func declText(content string, start, end int) string {
	lines := strings.Split(content, "\n")
	if start < 1 || start > len(lines) {
		return ""
	}
	if end >= start && end <= len(lines) {
		return strings.Join(lines[start-1:end], "\n")
	}
	depth, opened := 0, false
	for i := start - 1; i < len(lines); i++ {
		for _, ch := range stripLineNoise(lines[i]) {
			switch ch {
			case '{':
				depth++
				opened = true
			case '}':
				depth--
			}
		}
		if opened && depth <= 0 {
			return strings.Join(lines[start-1:i+1], "\n")
		}
	}
	return strings.Join(lines[start-1:], "\n")
}

// stripLineNoise removes string literals and line comments so braces inside
// them do not count.
func stripLineNoise(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		line = line[:i]
	}
	var b strings.Builder
	inStr := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inStr != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		if c == '"' || c == '`' || c == '\'' {
			inStr = c
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

var receiverRe = regexp.MustCompile(`^func\s*\(\s*(\w+)\s+\*?[\w\[\], ]+\)`)

func receiverVar(body string) string {
	first := body
	if i := strings.Index(body, "\n"); i >= 0 {
		first = body[:i]
	}
	if m := receiverRe.FindStringSubmatch(first); m != nil {
		return m[1]
	}
	return ""
}

func branchesFromCFG(c *flow.CFG) int {
	n := 0
	for _, b := range c.Blocks {
		if b != nil && strings.TrimSpace(b.Cond) != "" {
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return n
}

var branchRe = regexp.MustCompile(`(^|[^\w])(if|for|switch|case|select)\b|&&|\|\|`)

// branchesFromText is the cyclomatic style fallback used when no CFG exists
// for a function (and for operation bodies that are not yet files).
func branchesFromText(body string) int {
	n := 0
	for _, line := range strings.Split(body, "\n") {
		n += len(branchRe.FindAllString(stripLineNoise(line), -1))
	}
	return n
}

// imports returns the import paths declared in a Go file, sorted.
func imports(content string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.Trim(strings.TrimSpace(p), `"`)
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	inBlock := false
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "import ("):
			inBlock = true
		case inBlock && t == ")":
			inBlock = false
		case inBlock:
			if i := strings.Index(t, `"`); i >= 0 {
				add(t[i:])
			}
		case strings.HasPrefix(t, "import "):
			if i := strings.Index(t, `"`); i >= 0 {
				add(t[i:])
			}
		}
	}
	sort.Strings(out)
	return out
}

// Case helpers.

var (
	pascalRe = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	camelRe  = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
	snakeRe  = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)
	kebabRe  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

func matchesCase(name, style string) bool {
	switch style {
	case "PascalCase":
		return pascalRe.MatchString(name)
	case "camelCase":
		return camelRe.MatchString(name)
	case "snake_case":
		return snakeRe.MatchString(name)
	case "kebab-case":
		return kebabRe.MatchString(name)
	}
	return true
}

// caseOf names the case style of an identifier, or "" when it fits none.
func caseOf(name string) string {
	switch {
	case strings.Contains(name, "_") && snakeRe.MatchString(name):
		return "snake_case"
	case strings.Contains(name, "-") && kebabRe.MatchString(name):
		return "kebab-case"
	case pascalRe.MatchString(name):
		return "PascalCase"
	case camelRe.MatchString(name):
		return "camelCase"
	case snakeRe.MatchString(name):
		return "snake_case"
	}
	return ""
}

// isComponent reports whether a type is the kind the suffix and constructor
// rules are about: exported, in a layer directory other than model, and with
// at least one method. Plain data structs and interfaces are left alone.
func (t typeInfo) isComponent() bool {
	return t.Exported && !t.IsTest && t.Layer != "" && t.Layer != "model" && t.HasMethods
}

// fileStem is the file name without directory, extension, or _test.
func fileStem(rel string) string {
	base := path.Base(rel)
	base = strings.TrimSuffix(base, ".go")
	base = strings.TrimSuffix(base, "_test")
	return base
}

// trailingWord is the last capitalised word of a PascalCase name, e.g.
// "OrderService" -> "Service". Empty when the name is one word.
var trailingWordRe = regexp.MustCompile(`[A-Z][a-z0-9]+$`)

func trailingWord(name string) string {
	w := trailingWordRe.FindString(name)
	if w == "" || w == name {
		return ""
	}
	return w
}

// Regexes shared by the gate and the op check.
var (
	panicRe      = regexp.MustCompile(`(^|[^\w.])panic\(`)
	initFuncRe   = regexp.MustCompile(`(?m)^func init\(\)`)
	globalVarRe  = regexp.MustCompile(`(?m)^var\s+(\w+)\s*(=|map\[|\[\]|\*|\w)`)
	errorfRe     = regexp.MustCompile(`fmt\.Errorf\(\s*"([^"]*)"\s*,([^)]*)\)`)
	indexLoopRe  = regexp.MustCompile(`for\s+\w+\s*:?=\s*0\s*;\s*\w+\s*<\s*len\(`)
	rangeLoopRe  = regexp.MustCompile(`for\s+[\w, ]*:?=?\s*range\s`)
	constructorP = "New"
)

// errorfWithoutWrap reports fmt.Errorf calls that pass an err value without
// %w. It is the check behind patterns.require.error-wrapping.
func errorfWithoutWrap(text string) int {
	n := 0
	for _, m := range errorfRe.FindAllStringSubmatch(text, -1) {
		format, args := m[1], m[2]
		if !passesErrValue(args) {
			continue
		}
		if !strings.Contains(format, "%w") {
			n++
		}
	}
	return n
}

// passesErrValue reports whether an argument list carries an error VALUE:
// an identifier containing "err" (err, parseErr, errList) that is not a
// function call such as errors.New(...) or fmt.Errorf(...).
func passesErrValue(args string) bool {
	tokens := identRe.FindAllStringIndex(args, -1)
	for _, t := range tokens {
		ident := args[t[0]:t[1]]
		if !strings.Contains(strings.ToLower(ident), "err") {
			continue
		}
		rest := strings.TrimLeft(args[t[1]:], " \t")
		if strings.HasPrefix(rest, "(") || strings.HasPrefix(rest, ".") {
			continue // a call (errors.New) or a package selector (errors.)
		}
		return true
	}
	return false
}

var identRe = regexp.MustCompile(`[A-Za-z_]\w*`)

// mutableGlobals counts package level vars that are not error sentinels,
// blank, or regexp/const like values.
func mutableGlobals(content string) int {
	n := 0
	for _, m := range globalVarRe.FindAllStringSubmatch(content, -1) {
		line := m[0]
		if m[1] == "_" || strings.Contains(line, "errors.New") || strings.Contains(line, "regexp.MustCompile") {
			continue
		}
		n++
	}
	return n
}
