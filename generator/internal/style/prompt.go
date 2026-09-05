// generator/internal/style/prompt.go — v26.33: what the model is told.
//
// Two parts. PromptBlock renders the active rules as short lines, and it is
// built from the same ActiveRules list the gate checks, so the model is never
// asked for something the gate does not check, and the gate never checks
// something the model was not told. Exemplars pulls reference functions out
// of a team repository through the Understand index: retrieval by symbol
// name and layer over the AST graph, not by embedding. It is deterministic,
// needs no model, and shows the coder the team's own code in the shape the
// operation is about to take.
package style

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// PromptBlock renders the profile for a prompt. Empty when p is nil or has
// no active rule and no notes.
func PromptBlock(p *Profile) string {
	if p == nil {
		return ""
	}
	var lines []string
	for _, rule := range p.ActiveRules() {
		if l := ruleLine(p, rule); l != "" {
			lines = append(lines, l)
		}
	}
	if p.Code.Paradigm != "" {
		lines = append(lines, "Paradigm: "+paradigmText(p.Code.Paradigm)+".")
	}
	if p.Structure.Layout != "" {
		lines = append(lines, "Layout: "+p.Structure.Layout+" (keep model, repository, service and handler code in their own layers).")
	}
	switch p.Code.Iteration {
	case "stream":
		lines = append(lines, "In TypeScript prefer array methods (map, filter, reduce) over hand written loops.")
	case "loop":
		lines = append(lines, "In TypeScript prefer plain for loops over chained array methods.")
	}
	lines = append(lines, p.Prompt.Notes...)
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nTEAM STYLE RULES (a gate checks generated code against these; rules marked [error] block the run):\n")
	for _, l := range lines {
		b.WriteString("- " + l + "\n")
	}
	return b.String()
}

func tag(p *Profile, rule string) string {
	if p.SeverityOf(rule) == SevError {
		return " [error]"
	}
	return ""
}

func ruleLine(p *Profile, rule string) string {
	t := tag(p, rule)
	switch rule {
	case "naming.files":
		return fmt.Sprintf("File names are %s.%s", p.Naming.Files, t)
	case "naming.exported_funcs":
		return fmt.Sprintf("Exported function names are %s.%s", p.Naming.ExportedFuncs, t)
	case "naming.local_funcs":
		return fmt.Sprintf("Unexported function and helper names are %s.%s", p.Naming.LocalFuncs, t)
	case "naming.types":
		return fmt.Sprintf("Type names are %s.%s", p.Naming.Types, t)
	case "naming.receivers":
		return "Method receivers are 1 to 3 letters (s, h, repo)." + t
	case "naming.type_suffixes":
		var parts []string
		for _, layer := range sortedKeys(p.Naming.TypeSuffixes) {
			parts = append(parts, layer+" types end with "+p.Naming.TypeSuffixes[layer])
		}
		return strings.Join(parts, "; ") + "." + t
	case "structure.required_dirs":
		return "Code lives in these directories: " + strings.Join(p.Structure.RequiredDirs, ", ") + "." + t
	case "structure.forbidden_dirs":
		return "Never create these directories: " + strings.Join(p.Structure.ForbiddenDirs, ", ") + "." + t
	case "structure.file_pattern":
		var parts []string
		for _, layer := range sortedKeys(p.Structure.FilePattern) {
			parts = append(parts, layer+" files match "+p.Structure.FilePattern[layer])
		}
		return strings.Join(parts, "; ") + "." + t
	case "patterns.require.constructor":
		return "Every exported struct has a New<Type> constructor; dependencies are passed in, not reached for." + t
	case "patterns.require.repository":
		return "Data access goes through the repository layer." + t
	case "patterns.require.service-layer":
		return "Business rules live in the service layer, not in handlers." + t
	case "patterns.require.error-wrapping", "code.error_wrapping":
		if rule == "code.error_wrapping" && contains(p.Patterns.Require, "error-wrapping") {
			return ""
		}
		return "When you pass an error to fmt.Errorf use %w, never %v or %s." + t
	case "patterns.forbid.panic":
		return "Never call panic; return an error." + t
	case "patterns.forbid.global-state":
		return "No mutable package level variables; keep state in structs." + t
	case "patterns.forbid.init-func":
		return "No func init()." + t
	case "patterns.forbid.reflection":
		return "Do not import reflect." + t
	case "code.max_func_lines":
		return fmt.Sprintf("Keep every function at or under %d lines; split helpers out.%s", p.Code.MaxFuncLines, t)
	case "code.max_branches_per_func":
		return fmt.Sprintf("At most %d branch points (if, for, switch, case, &&, ||) per function.%s", p.Code.MaxBranches, t)
	case "code.max_params":
		return fmt.Sprintf("At most %d parameters per function; group more into a struct.%s", p.Code.MaxParams, t)
	case "code.iteration":
		if p.Code.Iteration == "range" {
			return "Iterate with for ... range, not an index loop over len()." + t
		}
		return "Iterate with an index loop, not range." + t
	case "code.allowed_imports":
		return "Import only from: " + strings.Join(p.Code.AllowedImports, ", ") + "." + t
	case "code.forbidden_imports":
		return "Never import: " + strings.Join(p.Code.ForbiddenImports, ", ") + "." + t
	}
	return ""
}

func paradigmText(v string) string {
	switch v {
	case "struct-methods":
		return "methods on structs that hold their dependencies; avoid free functions with many parameters"
	case "procedural":
		return "plain functions over simple data; avoid deep type hierarchies"
	case "functional":
		return "small pure helpers, no hidden state, data in and data out"
	}
	return v
}

// Exemplar is one reference function pulled from the team repository.
type Exemplar struct {
	File   string
	Name   string
	Layer  string
	Source string
}

var (
	exemplarMu    sync.Mutex
	exemplarCache = map[string]*snapshot{}
)

// Exemplars returns up to n functions from p.Prompt.ExemplarRoot that look
// like the operation about to be generated. Ranking: shared lowercase words
// between the function name and hints (the operation verbs), then the
// preferred layer, then shorter bodies; ties break on symbol id, so the pick
// is stable. Functions longer than 60 lines or shorter than 3 are skipped.
func Exemplars(p *Profile, hints []string, layer string, n int) []Exemplar {
	if p == nil || p.Prompt.ExemplarRoot == "" || n <= 0 {
		return nil
	}
	exemplarMu.Lock()
	s, ok := exemplarCache[p.Prompt.ExemplarRoot]
	if !ok {
		s = load(p.Prompt.ExemplarRoot)
		exemplarCache[p.Prompt.ExemplarRoot] = s
	}
	exemplarMu.Unlock()
	return pickExemplars(s, hints, layer, n)
}

// ResetExemplarCache drops cached repositories (tests, long lived servers).
func ResetExemplarCache() {
	exemplarMu.Lock()
	exemplarCache = map[string]*snapshot{}
	exemplarMu.Unlock()
}

func pickExemplars(s *snapshot, hints []string, layer string, n int) []Exemplar {
	words := map[string]bool{}
	for _, h := range hints {
		for _, w := range splitWords(h) {
			words[w] = true
		}
	}
	type scored struct {
		f     funcInfo
		score int
	}
	var cands []scored
	for _, f := range s.funcs {
		if f.IsTest || f.Lines < 3 || f.Lines > 60 || !f.IsMethod && !f.Exported {
			continue
		}
		sc := 0
		for _, w := range splitWords(f.Name) {
			if words[w] {
				sc += 10
			}
		}
		if layer != "" && f.Layer == layer {
			sc += 5
		}
		cands = append(cands, scored{f, sc})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		if cands[i].f.Lines != cands[j].f.Lines {
			return cands[i].f.Lines < cands[j].f.Lines
		}
		return cands[i].f.ID < cands[j].f.ID
	})
	var out []Exemplar
	for _, c := range cands {
		if len(out) == n {
			break
		}
		out = append(out, Exemplar{File: c.f.File, Name: c.f.Name, Layer: c.f.Layer, Source: c.f.Body})
	}
	return out
}

// RenderExemplars formats exemplars for a prompt.
func RenderExemplars(ex []Exemplar) string {
	if len(ex) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nREFERENCE CODE FROM THE TEAM REPOSITORY (match this style):\n")
	for _, e := range ex {
		fmt.Fprintf(&b, "// %s (%s)\n%s\n\n", e.File, e.Name, strings.TrimSpace(e.Source))
	}
	return b.String()
}

// splitWords lowercases and splits an identifier or phrase into words.
func splitWords(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 2 {
			out = append(out, strings.ToLower(cur.String()))
		}
		cur.Reset()
	}
	prevLower := false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			if prevLower {
				flush()
			}
			cur.WriteRune(r)
			prevLower = false
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			cur.WriteRune(r)
			prevLower = r >= 'a' && r <= 'z'
		default:
			flush()
			prevLower = false
		}
	}
	flush()
	return out
}
