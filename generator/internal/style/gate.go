// generator/internal/style/gate.go (v26.33): check a generated tree against a
// profile. Deterministic: same tree, same profile, same findings in the same
// order. The gate reads the Understand snapshot (analyze.go); it does not run
// the model and it does not modify anything. Acting on findings belongs to
// the agent (report, halt, or route to repair) exactly as the Understand
// verify stage does for structural findings.
package style

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Finding is one rule violation.
type Finding struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	File     string   `json:"file,omitempty"` // relative, forward slashes
	Line     int      `json:"line,omitempty"`
	Symbol   string   `json:"symbol,omitempty"`
	Detail   string   `json:"detail"`
	// Fixable says the repair loop can act on the file. Structure findings
	// (missing directories) are not fixable by rewriting a function body.
	Fixable bool `json:"fixable"`
}

// Report is the result of one gate pass.
type Report struct {
	Profile      string    `json:"profile"`
	Root         string    `json:"root"`
	Files        int       `json:"files"`
	Functions    int       `json:"functions"`
	RulesChecked []string  `json:"rules_checked"`
	Findings     []Finding `json:"findings"`
}

// Errors counts error severity findings.
func (r Report) Errors() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == SevError {
			n++
		}
	}
	return n
}

// Warnings counts warn severity findings.
func (r Report) Warnings() int { return len(r.Findings) - r.Errors() }

// Blocking reports whether the run should halt in gate mode.
func (r Report) Blocking() bool { return r.Errors() > 0 }

// FixableFiles lists the files with fixable findings, sorted and unique.
func (r Report) FixableFiles() []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range r.Findings {
		if f.Fixable && f.File != "" && !seen[f.File] {
			seen[f.File] = true
			out = append(out, f.File)
		}
	}
	sort.Strings(out)
	return out
}

// Check runs every active rule of p against the Go tree under root.
func Check(root string, p *Profile) Report {
	if p == nil {
		return Report{Root: root}
	}
	return checkSnapshot(load(root), p)
}

func checkSnapshot(s *snapshot, p *Profile) Report {
	rep := Report{Profile: p.Name, Root: s.root, Files: len(s.files), Functions: len(s.funcs), RulesChecked: p.ActiveRules()}
	g := &gate{p: p, s: s}
	for _, rule := range rep.RulesChecked {
		g.run(rule)
	}
	sort.SliceStable(g.out, func(i, j int) bool {
		a, b := g.out[i], g.out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Symbol < b.Symbol
	})
	rep.Findings = g.out
	return rep
}

type gate struct {
	p   *Profile
	s   *snapshot
	out []Finding
}

func (g *gate) add(rule, file string, line int, symbol, detail string, fixable bool) {
	g.out = append(g.out, Finding{
		Rule: rule, Severity: g.p.SeverityOf(rule), File: file, Line: line,
		Symbol: symbol, Detail: detail, Fixable: fixable,
	})
}

func (g *gate) run(rule string) {
	p, s := g.p, g.s
	switch {
	case rule == "naming.files":
		for _, rel := range sortedKeys(s.files) {
			stem := fileStem(rel)
			if !matchesCase(stem, p.Naming.Files) {
				g.add(rule, rel, 0, "", fmt.Sprintf("file name %q is not %s", path.Base(rel), p.Naming.Files), false)
			}
		}
	case rule == "naming.exported_funcs" || rule == "naming.local_funcs":
		want := p.Naming.ExportedFuncs
		exported := true
		if rule == "naming.local_funcs" {
			want, exported = p.Naming.LocalFuncs, false
		}
		for _, f := range s.funcs {
			if f.IsTest || f.Exported != exported {
				continue
			}
			if !matchesCase(f.Name, want) {
				g.add(rule, f.File, f.Line, f.Name, fmt.Sprintf("function %s is not %s", f.Name, want), true)
			}
		}
	case rule == "naming.types":
		for _, t := range s.types {
			if t.IsTest {
				continue
			}
			if !matchesCase(t.Name, p.Naming.Types) {
				g.add(rule, t.File, t.Line, t.Name, fmt.Sprintf("type %s is not %s", t.Name, p.Naming.Types), true)
			}
		}
	case rule == "naming.receivers":
		for _, f := range s.funcs {
			if f.IsTest || !f.IsMethod || f.Receiver == "" || f.Receiver == "_" {
				continue
			}
			if len(f.Receiver) > 3 {
				g.add(rule, f.File, f.Line, f.Name, fmt.Sprintf("receiver %q on %s is longer than 3 letters", f.Receiver, f.Name), true)
			}
		}
	case rule == "naming.type_suffixes":
		for _, layer := range sortedKeys(p.Naming.TypeSuffixes) {
			suffix := p.Naming.TypeSuffixes[layer]
			for _, t := range s.types {
				if !t.isComponent() || t.Layer != layer {
					continue
				}
				if !strings.HasSuffix(t.Name, suffix) {
					g.add(rule, t.File, t.Line, t.Name, fmt.Sprintf("exported type %s in layer %s should end with %s", t.Name, layer, suffix), true)
				}
			}
		}
	case rule == "structure.required_dirs":
		for _, d := range p.Structure.RequiredDirs {
			if !g.dirExists(d) {
				g.add(rule, d, 0, "", fmt.Sprintf("required directory %s is missing", d), false)
			}
		}
	case rule == "structure.forbidden_dirs":
		for _, d := range p.Structure.ForbiddenDirs {
			if g.dirExists(d) {
				g.add(rule, d, 0, "", fmt.Sprintf("forbidden directory %s exists", d), false)
			}
		}
	case rule == "structure.file_pattern":
		for _, layer := range sortedKeys(p.Structure.FilePattern) {
			glob := p.Structure.FilePattern[layer]
			for _, rel := range sortedKeys(s.files) {
				if layerOf(rel) != layer || strings.HasSuffix(rel, "_test.go") {
					continue
				}
				if ok, _ := path.Match(glob, path.Base(rel)); !ok {
					g.add(rule, rel, 0, "", fmt.Sprintf("file in layer %s does not match %s", layer, glob), false)
				}
			}
		}
	case rule == "patterns.require.constructor":
		funcNames := map[string]bool{}
		for _, f := range s.funcs {
			if !f.IsMethod {
				funcNames[f.Name] = true
			}
		}
		for _, t := range s.types {
			if !t.isComponent() {
				continue
			}
			if !funcNames[constructorP+t.Name] {
				g.add(rule, t.File, t.Line, t.Name, fmt.Sprintf("type %s has no New%s constructor", t.Name, t.Name), true)
			}
		}
	case rule == "patterns.require.repository" || rule == "patterns.require.service-layer":
		layer, dir := "repo", "internal/repository"
		if rule == "patterns.require.service-layer" {
			layer, dir = "service", "internal/service"
		}
		found := false
		for _, t := range s.types {
			if t.Exported && t.Layer == layer {
				found = true
				break
			}
		}
		if !found {
			g.add(rule, dir, 0, "", fmt.Sprintf("no exported type in the %s layer (%s)", layer, dir), false)
		}
	case rule == "patterns.require.error-wrapping":
		for _, f := range s.funcs {
			if f.IsTest {
				continue
			}
			if n := errorfWithoutWrap(f.Body); n > 0 {
				g.add(rule, f.File, f.Line, f.Name, fmt.Sprintf("%d fmt.Errorf call(s) in %s pass err without %%w", n, f.Name), true)
			}
		}
	case rule == "patterns.forbid.panic":
		for _, f := range s.funcs {
			if f.IsTest {
				continue
			}
			if panicRe.MatchString(f.Body) {
				g.add(rule, f.File, f.Line, f.Name, fmt.Sprintf("%s calls panic", f.Name), true)
			}
		}
	case rule == "patterns.forbid.init-func":
		for _, rel := range sortedKeys(s.files) {
			if strings.HasSuffix(rel, "_test.go") {
				continue
			}
			if initFuncRe.MatchString(s.files[rel]) {
				g.add(rule, rel, 0, "init", "file declares func init()", true)
			}
		}
	case rule == "patterns.forbid.global-state":
		for _, rel := range sortedKeys(s.files) {
			if strings.HasSuffix(rel, "_test.go") {
				continue
			}
			if n := mutableGlobals(s.files[rel]); n > 0 {
				g.add(rule, rel, 0, "", fmt.Sprintf("%d mutable package level var(s)", n), true)
			}
		}
	case rule == "patterns.forbid.reflection":
		for _, rel := range sortedKeys(s.files) {
			for _, imp := range imports(s.files[rel]) {
				if imp == "reflect" {
					g.add(rule, rel, 0, "", "imports reflect", true)
				}
			}
		}
	case rule == "code.max_func_lines":
		for _, f := range s.funcs {
			if f.IsTest {
				continue
			}
			if f.Lines > p.Code.MaxFuncLines {
				g.add(rule, f.File, f.Line, f.Name, fmt.Sprintf("%s is %d lines (max %d)", f.Name, f.Lines, p.Code.MaxFuncLines), true)
			}
		}
	case rule == "code.max_branches_per_func":
		for _, f := range s.funcs {
			if f.IsTest {
				continue
			}
			if f.Branches > p.Code.MaxBranches {
				g.add(rule, f.File, f.Line, f.Name, fmt.Sprintf("%s has %d branch points (max %d)", f.Name, f.Branches, p.Code.MaxBranches), true)
			}
		}
	case rule == "code.max_params":
		for _, f := range s.funcs {
			if f.IsTest {
				continue
			}
			if f.Params > p.Code.MaxParams {
				g.add(rule, f.File, f.Line, f.Name, fmt.Sprintf("%s takes %d parameters (max %d)", f.Name, f.Params, p.Code.MaxParams), true)
			}
		}
	case rule == "code.error_wrapping":
		// Same check as the pattern; a profile may state either. Report once.
		if !contains(p.Patterns.Require, "error-wrapping") {
			for _, f := range s.funcs {
				if f.IsTest {
					continue
				}
				if n := errorfWithoutWrap(f.Body); n > 0 {
					g.add(rule, f.File, f.Line, f.Name, fmt.Sprintf("%d fmt.Errorf call(s) in %s pass err without %%w", n, f.Name), true)
				}
			}
		}
	case rule == "code.iteration":
		for _, f := range s.funcs {
			if f.IsTest {
				continue
			}
			switch p.Code.Iteration {
			case "range":
				if indexLoopRe.MatchString(f.Body) {
					g.add(rule, f.File, f.Line, f.Name, fmt.Sprintf("%s uses an index loop over len(); the team style is range", f.Name), true)
				}
			case "index":
				if rangeLoopRe.MatchString(f.Body) {
					g.add(rule, f.File, f.Line, f.Name, fmt.Sprintf("%s uses range; the team style is an index loop", f.Name), true)
				}
			}
		}
	case rule == "code.allowed_imports":
		for _, rel := range sortedKeys(s.files) {
			for _, imp := range imports(s.files[rel]) {
				if !hasPrefixAny(imp, p.Code.AllowedImports) {
					g.add(rule, rel, 0, "", fmt.Sprintf("import %q is outside the allowed list", imp), true)
				}
			}
		}
	case rule == "code.forbidden_imports":
		for _, rel := range sortedKeys(s.files) {
			for _, imp := range imports(s.files[rel]) {
				if hasPrefixAny(imp, p.Code.ForbiddenImports) {
					g.add(rule, rel, 0, "", fmt.Sprintf("import %q is forbidden", imp), true)
				}
			}
		}
	}
}

func (g *gate) dirExists(rel string) bool {
	if g.s.dirs[strings.Trim(rel, "/")] {
		return true
	}
	if g.s.root == "" {
		return false
	}
	st, err := os.Stat(filepath.Join(g.s.root, filepath.FromSlash(rel)))
	return err == nil && st.IsDir()
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// hasPrefixAny matches an import path against prefixes. "net/http" matches
// "net/http" and "net/http/httptest"; "net" matches "net" and "net/..." but
// not "network".
func hasPrefixAny(imp string, prefixes []string) bool {
	for _, p := range prefixes {
		if imp == p || strings.HasPrefix(imp, p+"/") {
			return true
		}
	}
	return false
}

// RenderReport is the plain text a human reads.
func RenderReport(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "gen-system style gate: %s\n", r.Profile)
	fmt.Fprintf(&b, "root: %s  files: %d  functions: %d  rules: %d\n", r.Root, r.Files, r.Functions, len(r.RulesChecked))
	fmt.Fprintf(&b, "findings: %d error, %d warn\n", r.Errors(), r.Warnings())
	for _, f := range r.Findings {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		fmt.Fprintf(&b, "  [%s] %s  %s  %s\n", f.Severity, f.Rule, loc, f.Detail)
	}
	return b.String()
}
