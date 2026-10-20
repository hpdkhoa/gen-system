package review

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// Options says what to review.
type Options struct {
	Root string // repo root, absolute
	Base string // git revision to compare the working tree with (default HEAD); ignored when Diff is set
	Diff string // a unified diff to review instead of asking git; then nothing before the change is known
	Hops int    // how many calls up to look for a test (default 3)
}

// Ref names one symbol.
type Ref struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// Func is one function the change touched.
type Func struct {
	Ref
	bare           string   // the name as a call writes it
	recvType       string   // a method's receiver type
	dir            string   // the folder the function is in, absolute
	Status         string   `json:"status"` // added, changed or removed
	Language       string   `json:"language"`
	IsTest         bool     `json:"is_test,omitempty"`
	SigBefore      string   `json:"signature_before,omitempty"`
	SigAfter       string   `json:"signature_after,omitempty"`
	EffectsBefore  []string `json:"effects_before,omitempty"`
	EffectsAfter   []string `json:"effects_after,omitempty"`
	BranchesBefore int      `json:"branches_before"`
	BranchesAfter  int      `json:"branches_after"`
	CallersOutside []Ref    `json:"callers_outside,omitempty"` // callers the change did not touch
	CallersInside  int      `json:"callers_inside"`
	ReachedBy      []Ref    `json:"reached_by_tests,omitempty"` // tests that reach it within Hops calls
	StillCalledBy  []Ref    `json:"still_called_by,omitempty"`  // for a removed function: calls left to its name
}

// Point is one thing a reviewer should look at.
type Point struct {
	Level  string `json:"level"` // break, behavior, test, size
	Symbol string `json:"symbol"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Text   string `json:"text"`
}

// Report is the review of one change.
type Report struct {
	Base      string   `json:"base,omitempty"`
	Files     int      `json:"files_changed"`
	Parsed    int      `json:"files_parsed"`
	Skipped   []string `json:"files_not_parsed,omitempty"`
	Points    []Point  `json:"points"`
	Functions []Func   `json:"functions"`
	Seconds   float64  `json:"seconds"`
	Notes     []string `json:"notes,omitempty"`
}

// Levels in the order a reviewer reads them.
var levelOrder = map[string]int{"break": 0, "behavior": 1, "test": 2, "size": 3}

// Run reviews the working tree against opt.Base (or opt.Diff) using idx, the index of the current code.
func Run(idx *symbols.Index, opt Options) (*Report, error) {
	t0 := time.Now()
	if opt.Hops <= 0 {
		opt.Hops = 3
	}
	rep := &Report{}
	var files []*FileDiff
	haveBefore := opt.Diff == ""
	if haveBefore {
		if opt.Base == "" {
			opt.Base = "HEAD"
		}
		rep.Base = opt.Base
		var err error
		if files, err = changesSince(opt.Root, opt.Base); err != nil {
			return nil, err
		}
	} else {
		files = ParseDiff(opt.Diff)
		rep.Notes = append(rep.Notes, "a diff without a git base: signature, effect and branch changes and removed functions are not checked")
	}
	rep.Files = len(files)

	g := newGraph(idx, opt.Root)
	changed := map[string]bool{} // symbol ids the change touched, after the change
	var funcs []*Func
	for _, fd := range files {
		if fd.Binary {
			continue
		}
		fs, ok := reviewFile(opt, fd, haveBefore)
		if !ok {
			p := fd.NewPath
			if p == "" {
				p = fd.OldPath
			}
			if l := ast.DetectLanguage(p); l != "" && l != ast.LangUnknown {
				rep.Skipped = append(rep.Skipped, p)
			}
			continue
		}
		rep.Parsed++
		for _, f := range fs {
			if f.Status != "removed" {
				changed[f.ID] = true
			}
		}
		funcs = append(funcs, fs...)
	}

	for _, f := range funcs {
		if f.Status == "removed" {
			for _, c := range g.stillCalling(f) {
				f.StillCalledBy = append(f.StillCalledBy, c)
			}
			continue
		}
		for _, c := range g.callers(f.ID) {
			if changed[c.ID] {
				f.CallersInside++
			} else {
				f.CallersOutside = append(f.CallersOutside, c)
			}
		}
		if !f.IsTest {
			f.ReachedBy = g.testsReaching(f.ID, opt.Hops)
		}
	}
	for _, f := range funcs {
		rep.Points = append(rep.Points, pointsFor(f, opt.Hops, haveBefore)...)
		rep.Functions = append(rep.Functions, *f)
	}
	sort.SliceStable(rep.Points, func(i, j int) bool {
		a, b := rep.Points[i], rep.Points[j]
		if levelOrder[a.Level] != levelOrder[b.Level] {
			return levelOrder[a.Level] < levelOrder[b.Level]
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	if rep.Points == nil {
		rep.Points = []Point{}
	}
	if rep.Functions == nil {
		rep.Functions = []Func{}
	}
	rep.Seconds = time.Since(t0).Seconds()
	return rep, nil
}

// parsed is one version of one file.
type parsed struct {
	syms    map[string]*symbols.Symbol // key: kind:qualified name
	cfgs    flow.CFGSet
	effects map[string]*flow.FuncEffects
}

func parseVersion(absPath string, src []byte) (*parsed, bool) {
	m, err := ast.ParseFile(absPath, src)
	if err != nil || m == nil {
		return nil, false
	}
	idx := symbols.NewIndex()
	idx.AddModule(m)
	idx.ResolveAll()
	cfgs := flow.Build(m, idx)
	p := &parsed{syms: map[string]*symbols.Symbol{}, cfgs: cfgs, effects: flow.InferEffects(m, cfgs)}
	for _, s := range idx.AllSymbols() {
		if isFunc(s) {
			p.syms[string(s.Kind)+":"+s.QualName] = s
		}
	}
	return p, true
}

func isFunc(s *symbols.Symbol) bool {
	return s.Kind == symbols.SymFunc || s.Kind == symbols.SymMethod || s.Kind == symbols.SymProcedure
}

// reviewFile lists the functions one file's change touched.
func reviewFile(opt Options, fd *FileDiff, haveBefore bool) ([]*Func, bool) {
	var after, before *parsed
	newAbs := ""
	if fd.NewPath != "" {
		newAbs = filepath.Join(opt.Root, filepath.FromSlash(fd.NewPath))
		src, err := os.ReadFile(newAbs)
		if err != nil {
			return nil, false
		}
		var ok bool
		if after, ok = parseVersion(newAbs, src); !ok {
			return nil, false
		}
	}
	if haveBefore && fd.OldPath != "" {
		// Parse the old text under the new path, so both versions give the same symbol ids.
		at := newAbs
		if at == "" {
			at = filepath.Join(opt.Root, filepath.FromSlash(fd.OldPath))
		}
		if src, err := showAt(opt.Root, opt.Base, fd.OldPath); err == nil {
			before, _ = parseVersion(at, src)
		}
	}
	if after == nil && before == nil {
		return nil, false
	}

	var out []*Func
	if after != nil {
		for key, s := range after.syms {
			a, b := s.Span.Start.Line, s.Span.End.Line
			var old *symbols.Symbol
			if before != nil {
				old = before.syms[key]
			}
			status := ""
			switch {
			case fd.Added || (haveBefore && old == nil):
				status = "added"
			case overlaps(fd.NewLines, a, b):
				status = "changed"
			case old != nil && overlaps(fd.OldLines, old.Span.Start.Line, old.Span.End.Line):
				status = "changed" // only lines were removed inside it
			default:
				continue
			}
			f := newFunc(s, fd.NewPath, status)
			f.SigAfter = signature(s)
			f.EffectsAfter, f.BranchesAfter = effectsOf(after, s.ID), branches(after, s.ID)
			if old != nil {
				f.SigBefore = signature(old)
				f.EffectsBefore, f.BranchesBefore = effectsOf(before, old.ID), branches(before, old.ID)
			}
			out = append(out, f)
		}
	}
	if before != nil {
		for key, s := range before.syms {
			if after != nil && after.syms[key] != nil {
				continue
			}
			f := newFunc(s, fd.OldPath, "removed")
			f.SigBefore = signature(s)
			f.EffectsBefore, f.BranchesBefore = effectsOf(before, s.ID), branches(before, s.ID)
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, true
}

func newFunc(s *symbols.Symbol, rel, status string) *Func {
	return &Func{
		Ref:      Ref{ID: s.ID, Name: displayName(s), File: rel, Line: s.Span.Start.Line},
		bare:     s.Name,
		recvType: s.ReceiverType,
		dir:      filepath.Dir(s.DefinedIn),
		Status:   status,
		Language: string(s.Language),
		IsTest:   isTestFile(rel),
	}
}

func displayName(s *symbols.Symbol) string {
	if s.QualName != "" {
		return s.QualName
	}
	return s.Name
}

func signature(s *symbols.Symbol) string {
	var ps []string
	for _, p := range s.Params {
		t := p.Type.String()
		if p.Variadic {
			t = "..." + t
		}
		ps = append(ps, strings.TrimSpace(p.Name+" "+t))
	}
	var rs []string
	for _, r := range s.Returns {
		rs = append(rs, r.String())
	}
	sig := "(" + strings.Join(ps, ", ") + ")"
	if len(rs) == 1 {
		sig += " " + rs[0]
	} else if len(rs) > 1 {
		sig += " (" + strings.Join(rs, ", ") + ")"
	}
	return sig
}

// effectsOf lists what a function touches: db, db write, fs, network. Pure and unknown are left out.
func effectsOf(p *parsed, id string) []string {
	e := p.effects[id]
	if e == nil {
		return nil
	}
	var out []string
	for _, k := range []flow.EffectKind{flow.EffectDB, flow.EffectFS, flow.EffectNetwork} {
		if e.Effects[k] {
			out = append(out, string(k))
		}
	}
	if e.DBWrite {
		out = append(out, "db write")
	}
	return out
}

// branches counts the blocks that split the flow.
func branches(p *parsed, id string) int {
	c := p.cfgs[id]
	if c == nil {
		return 0
	}
	n := 0
	for _, b := range c.Blocks {
		if len(b.Succs) > 1 {
			n++
		}
	}
	return n
}

// isTestFile tells a test file by its name, the way each language's tools do.
func isTestFile(rel string) bool {
	p := strings.ToLower(filepath.ToSlash(rel))
	base := p[strings.LastIndex(p, "/")+1:]
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return true
	case strings.HasSuffix(base, ".py") && (strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")):
		return true
	case strings.Contains(base, ".test.") || strings.Contains(base, ".spec."):
		return true
	case strings.HasSuffix(base, "test.java") || strings.HasSuffix(base, "tests.java"):
		return true
	case strings.Contains(p, "/src/test/") || strings.HasPrefix(p, "src/test/"):
		return true
	case strings.HasSuffix(base, ".py") && (strings.Contains(p, "/tests/") || strings.HasPrefix(p, "tests/")):
		return true
	}
	return false
}

var effectWords = map[string]string{"db": "the database", "db write": "database writes", "fs": "files", "network": "the network"}

func pointsFor(f *Func, hops int, haveBefore bool) []Point {
	var out []Point
	add := func(level, text string) {
		out = append(out, Point{Level: level, Symbol: f.Name, File: f.File, Line: f.Line, Text: text})
	}
	if f.Status == "removed" {
		if n := len(f.StillCalledBy); n > 0 {
			add("break", fmt.Sprintf("%s was removed but is still called from %s: %s", f.Name, count(n, "place"), refList(f.StillCalledBy, 5)))
		}
		return out
	}
	if haveBefore && f.Status == "changed" && f.SigBefore != f.SigAfter && len(f.CallersOutside) > 0 {
		add("break", fmt.Sprintf("%s changed its signature from %s to %s; %s outside this change: %s",
			f.Name, f.SigBefore, f.SigAfter, count(len(f.CallersOutside), "caller"), refList(f.CallersOutside, 5)))
	}
	if haveBefore {
		before := map[string]bool{}
		for _, e := range f.EffectsBefore {
			before[e] = true
		}
		var gained []string
		for _, e := range f.EffectsAfter {
			if !before[e] && !(e == "db" && before["db write"]) {
				gained = append(gained, effectWords[e])
			}
		}
		if len(gained) > 0 {
			verb := "now touches"
			if f.Status == "added" {
				verb = "is new and touches"
			}
			add("behavior", fmt.Sprintf("%s %s %s", f.Name, verb, strings.Join(gained, " and ")))
		}
	}
	if !f.IsTest && len(f.ReachedBy) == 0 && testable(f.Language) && !entryPoint[f.bare] {
		add("test", fmt.Sprintf("no test reaches %s within %d calls", f.Name, hops))
	}
	if d := f.BranchesAfter - f.BranchesBefore; haveBefore && d >= 2 {
		add("size", fmt.Sprintf("%s has %d more branches (%d to %d)", f.Name, d, f.BranchesBefore, f.BranchesAfter))
	}
	return out
}

// entryPoint: functions the runtime calls, never a test.
var entryPoint = map[string]bool{"main": true, "init": true, "TestMain": true, "__main__": true}

// testable: languages whose tests the name rules above can find.
func testable(lang string) bool {
	switch ast.Language(lang) {
	case ast.LangGo, ast.LangPython, ast.LangTS, ast.LangJava:
		return true
	}
	return false
}

func count(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func refList(rs []Ref, max int) string {
	var parts []string
	for i, r := range rs {
		if i == max {
			parts = append(parts, fmt.Sprintf("and %d more", len(rs)-max))
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%s:%d)", r.Name, r.File, r.Line))
	}
	return strings.Join(parts, ", ")
}
