package bench

// The review_diff benchmark. It copies a Go repo, makes edits with a known answer, runs the review, and scores it:
//   signature  add a parameter to a function: the callers Go's type checker rejects are the answer
//   remove     delete a function: the places Go's type checker reports are the answer
//   fs, network, db  insert one call of that kind: the review must report that new effect
//   control:*  insert a call that touches nothing: the review must report no new effect
//   branch     insert two if statements: the review must report the growth
//   coverage   (optional) go test -coverpkg: is "no test reaches it" right for each function?
//   history    real commits: how many points a real change gets
// Only Go has a compiler judge here; the review itself runs the same code for every language.

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	genast "github.com/hpdkhoa/gen-system/generator/internal/ast"
	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/review"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// ReviewBenchOptions configures one run.
type ReviewBenchOptions struct {
	Repo     string // a git repo with a Go module at its root; its HEAD is copied, the repo is not touched
	N        int    // cases per kind
	Seed     int64
	Hops     int
	Commits  int  // real commits for the history check
	Coverage bool // run go test -coverpkg=./... for the test check
}

// ReviewCase is one edit and how the review did.
type ReviewCase struct {
	Kind     string   `json:"kind"`
	Function string   `json:"function"`
	File     string   `json:"file"`
	Truth    []string `json:"truth,omitempty"`
	Found    []string `json:"found,omitempty"`
	TP       int      `json:"tp"`
	FP       int      `json:"fp"`
	FN       int      `json:"fn"`
	Pass     bool     `json:"pass"`
	Seconds  float64  `json:"seconds"`
	Note     string   `json:"note,omitempty"`
}

// KindScore sums the cases of one kind.
type KindScore struct {
	Cases     int     `json:"cases"`
	Pass      int     `json:"pass"`
	TP        int     `json:"tp"`
	FP        int     `json:"fp"`
	FN        int     `json:"fn"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	MedianSec float64 `json:"median_seconds"`
}

// CoverageScore compares "no test reaches it" with go test coverage, per function.
type CoverageScore struct {
	Functions   int                    `json:"functions"`
	Covered     int                    `json:"covered"`
	TestSeconds float64                `json:"go_test_seconds"`
	TestExit    string                 `json:"go_test_exit,omitempty"`
	ByHops      map[string]CoverageHop `json:"by_hops"`
	// Disagree lists, at 3 hops, the functions where the review and coverage differ: kind|function|file:line.
	Disagree []string `json:"disagree_at_3_hops,omitempty"`
}

// CoverageHop scores the untested flag at one hop limit. Positive: the review says no test reaches it.
type CoverageHop struct {
	TP        int     `json:"untested_and_not_covered"`
	FP        int     `json:"untested_but_covered"`
	FN        int     `json:"reached_but_not_covered"`
	TN        int     `json:"reached_and_covered"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	Agreement float64 `json:"agreement"`
}

// HistoryRow is the review of one real commit.
type HistoryRow struct {
	Commit    string         `json:"commit"`
	Subject   string         `json:"subject"`
	Files     int            `json:"files"`
	Functions int            `json:"functions"`
	Points    map[string]int `json:"points"`
	Seconds   float64        `json:"seconds"`
}

// ReviewBenchResult is the whole run.
type ReviewBenchResult struct {
	Repo        string                `json:"repo"`
	Commit      string                `json:"commit"`
	Date        string                `json:"date"`
	Functions   int                   `json:"go_functions"`
	LoadSeconds float64               `json:"load_seconds"`
	Baseline    int                   `json:"type_errors_before_any_edit"`
	Scores      map[string]*KindScore `json:"scores"`
	Errors      int                   `json:"cases_that_did_not_run,omitempty"`
	Coverage    *CoverageScore        `json:"coverage,omitempty"`
	History     []HistoryRow          `json:"history,omitempty"`
	Cases       []ReviewCase          `json:"cases"`
}

var (
	effectStmts = map[string]string{
		"fs":      `_, _ = os.ReadFile("review-bench")`,
		"network": `_, _ = http.Get("http://review-bench")`,
		"db":      `_, _ = db.Exec("UPDATE t SET x = 1")`,
	}
	controlStmts = map[string]string{
		"control:len":         `_ = len("review-bench")`,
		"control:strings":     `_ = strings.ToUpper("review-bench")`,
		"control:cache.Get":   `_ = rbCache.Get("key")`,
		"control:list.Remove": `rbList.Remove(nil)`,
		"control:sb.Create":   `_ = rbBuilder.Create()`,
	}
	branchStmt = "if len(\"review-bench\") > 0 {\n\t_ = 1\n}\nif len(\"review-bench\") > 1 {\n\t_ = 2\n}"
)

// RunReviewBench runs the benchmark.
func RunReviewBench(o ReviewBenchOptions) (*ReviewBenchResult, error) {
	if o.N <= 0 {
		o.N = 20
	}
	if o.Hops <= 0 {
		o.Hops = 3
	}
	tmp, err := os.MkdirTemp("", "review-bench-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	clone := filepath.Join(tmp, "repo")
	if out, err := exec.Command("git", "clone", "--quiet", "--no-hardlinks", o.Repo, clone).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git clone: %v: %s", err, out)
	}
	head := strings.TrimSpace(gitOut(clone, "rev-parse", "HEAD"))
	res := &ReviewBenchResult{Repo: o.Repo, Commit: head, Date: time.Now().Format("2006-01-02"), Scores: map[string]*KindScore{}}
	if _, err := os.Stat(filepath.Join(clone, "go.mod")); err != nil {
		return nil, fmt.Errorf("no go.mod at the repo root")
	}

	fmt.Fprintln(os.Stderr, "review-bench: compiling and type-checking the clean tree")
	judge, err := newTypeJudge(clone)
	if err != nil {
		return nil, err
	}
	res.Baseline = judge.recordBaseline()

	t0 := time.Now()
	ctx := agentctx.LoadContext(clone)
	res.LoadSeconds = time.Since(t0).Seconds()
	idx := ctx.Index

	funcs := goFunctions(idx, clone, judge)
	res.Functions = len(funcs)
	rng := rand.New(rand.NewSource(o.Seed))
	rng.Shuffle(len(funcs), func(i, j int) { funcs[i], funcs[j] = funcs[j], funcs[i] })
	called := withCallers(funcs, idx)

	if o.Coverage {
		fmt.Fprintln(os.Stderr, "review-bench: go test -coverpkg=./... (slow)")
		res.Coverage = coverageScore(clone, idx, judge, funcs)
	}

	run := func(kind string, s *symbols.Symbol, edit func([]string, *symbols.Symbol) ([]string, bool), score func(*review.Report, *symbols.Symbol, *ReviewCase)) {
		path := s.DefinedIn
		orig, err := os.ReadFile(path)
		if err != nil {
			return
		}
		lines := strings.Split(string(orig), "\n")
		newLines, ok := edit(lines, s)
		if !ok {
			return
		}
		rel := relSlash(clone, path)
		c := ReviewCase{Kind: kind, Function: s.QualName, File: rel}
		writeAndIndex(idx, path, []byte(strings.Join(newLines, "\n")))
		t := time.Now()
		rep, err := review.Run(idx, review.Options{Root: clone, Base: "HEAD", Hops: o.Hops})
		if err != nil {
			// Windows can hold a file in the copy for a moment (a virus scan, the indexer); try once more
			time.Sleep(2 * time.Second)
			t = time.Now()
			rep, err = review.Run(idx, review.Options{Root: clone, Base: "HEAD", Hops: o.Hops})
		}
		c.Seconds = time.Since(t).Seconds()
		if err != nil {
			// the review did not run: no answer to score, so the case is counted apart
			c.Kind += ":error"
			c.Note = err.Error()
		} else {
			score(rep, s, &c)
		}
		writeAndIndex(idx, path, orig)
		res.Cases = append(res.Cases, c)
	}

	callerScore := func(want func(*review.Func) []review.Ref) func(*review.Report, *symbols.Symbol, *ReviewCase) {
		return func(rep *review.Report, s *symbols.Symbol, c *ReviewCase) {
			truth := map[string]bool{}
			self := relSlash(clone, s.DefinedIn) + ":" + s.QualName
			for _, e := range judge.brokenBy(filepath.Dir(s.DefinedIn)) {
				if f := funcAt(e.File, e.Line, clone); f != "" && f != self {
					truth[f] = true
				}
			}
			found := map[string]bool{}
			for _, f := range rep.Functions {
				if f.ID == s.ID {
					for _, r := range want(&f) {
						found[r.File+":"+r.Name] = true
					}
				}
			}
			c.Truth, c.Found = keys(truth), keys(found)
			unchecked := 0
			for k := range found {
				switch {
				case truth[k]:
					c.TP++
				case !judge.checkable(filepath.Join(clone, filepath.FromSlash(k[:strings.LastIndex(k, ":")]))):
					unchecked++ // a caller the type checker never sees (an _examples folder): not counted
				default:
					c.FP++
				}
			}
			if unchecked > 0 {
				c.Note = fmt.Sprintf("%d callers in files the type checker does not see, not counted", unchecked)
			}
			for k := range truth {
				if !found[k] {
					c.FN++
				}
			}
			c.Pass = c.FP == 0 && c.FN == 0
		}
	}

	fmt.Fprintln(os.Stderr, "review-bench: signature and remove edits")
	for _, s := range take(called, o.N, func(s *symbols.Symbol) bool { _, ok := addParam(nil, s); return ok }) {
		run("signature", s, addParam, callerScore(func(f *review.Func) []review.Ref { return f.CallersOutside }))
	}
	for _, s := range take(called, o.N, nil) {
		run("remove", s, removeFunc, callerScore(func(f *review.Func) []review.Ref { return f.StillCalledBy }))
	}

	fmt.Fprintln(os.Stderr, "review-bench: effect, control and branch edits")
	pool := take(funcs, o.N, func(s *symbols.Symbol) bool { _, ok := insertStmt(nil, s, "x"); return ok })
	kinds := sortedKeys(effectStmts)
	controls := sortedKeys(controlStmts)
	for _, s := range pool {
		for _, k := range kinds {
			k := k
			run(k, s, func(l []string, s *symbols.Symbol) ([]string, bool) { return insertStmt(l, s, effectStmts[k]) },
				effectScore(k))
		}
		for _, k := range controls {
			k := k
			run(k, s, func(l []string, s *symbols.Symbol) ([]string, bool) { return insertStmt(l, s, controlStmts[k]) },
				effectScore(""))
		}
		run("branch", s, func(l []string, s *symbols.Symbol) ([]string, bool) { return insertStmt(l, s, branchStmt) },
			func(rep *review.Report, s *symbols.Symbol, c *ReviewCase) {
				for _, f := range rep.Functions {
					if f.ID == s.ID {
						c.Found = []string{fmt.Sprintf("%d to %d", f.BranchesBefore, f.BranchesAfter)}
						c.Pass = f.BranchesAfter-f.BranchesBefore == 2
					}
				}
				if !c.Pass {
					c.FN = 1
				} else {
					c.TP = 1
				}
			})
	}

	if o.Commits > 0 {
		fmt.Fprintln(os.Stderr, "review-bench: real commits")
		res.History = history(clone, o.Commits, o.Hops)
		_ = exec.Command("git", "-C", clone, "checkout", "-q", head).Run()
	}
	res.Scores = scoreKinds(res.Cases)
	for _, c := range res.Cases {
		if strings.HasSuffix(c.Kind, ":error") {
			res.Errors++
		}
	}
	return res, nil
}

func effectScore(want string) func(*review.Report, *symbols.Symbol, *ReviewCase) {
	return func(rep *review.Report, s *symbols.Symbol, c *ReviewCase) {
		var gained []string
		for _, f := range rep.Functions {
			if f.ID != s.ID {
				continue
			}
			before := map[string]bool{}
			for _, e := range f.EffectsBefore {
				before[e] = true
			}
			for _, e := range f.EffectsAfter {
				if !before[e] {
					gained = append(gained, e)
				}
			}
		}
		c.Found = gained
		for _, f := range rep.Functions {
			if f.ID == s.ID && want != "" {
				for _, e := range f.EffectsBefore {
					if e == want || (want == "db" && e == "db write") {
						c.Kind += ":skip"
						c.Note = "the function already had this effect"
						return
					}
				}
			}
		}
		hit := false
		for _, g := range gained {
			if g == want || (want == "db" && g == "db write") {
				hit = true
			} else {
				c.FP++
			}
		}
		if want == "" {
			c.Pass = len(gained) == 0
			return
		}
		c.Truth = []string{want}
		if hit {
			c.TP = 1
		} else {
			c.FN = 1
		}
		c.Pass = hit && c.FP == 0
	}
}

func scoreKinds(cases []ReviewCase) map[string]*KindScore {
	out := map[string]*KindScore{}
	secs := map[string][]float64{}
	for _, c := range cases {
		k := c.Kind
		if strings.HasSuffix(k, ":skip") || strings.HasSuffix(k, ":error") {
			continue
		}
		if strings.HasPrefix(k, "control:") {
			k = "control"
		}
		s := out[k]
		if s == nil {
			s = &KindScore{}
			out[k] = s
		}
		s.Cases++
		if c.Pass {
			s.Pass++
		}
		s.TP += c.TP
		s.FP += c.FP
		s.FN += c.FN
		secs[k] = append(secs[k], c.Seconds)
	}
	for k, s := range out {
		if s.TP+s.FP > 0 {
			s.Precision = float64(s.TP) / float64(s.TP+s.FP)
		}
		if s.TP+s.FN > 0 {
			s.Recall = float64(s.TP) / float64(s.TP+s.FN)
		}
		sort.Float64s(secs[k])
		s.MedianSec = secs[k][len(secs[k])/2]
	}
	return out
}

// goFunctions lists the Go functions and methods of the module that a package build includes, tests left out.
func goFunctions(idx *symbols.Index, root string, j *typeJudge) []*symbols.Symbol {
	var out []*symbols.Symbol
	for _, s := range idx.AllSymbols() {
		if s.Language != genast.LangGo || s.Provenance.IsExternal() || (s.Kind != symbols.SymFunc && s.Kind != symbols.SymMethod) {
			continue
		}
		rel := relSlash(root, s.DefinedIn)
		if strings.HasSuffix(rel, "_test.go") || strings.Contains(rel, "testdata/") || s.Name == "main" || s.Name == "init" {
			continue
		}
		if s.Span.End.Line-s.Span.Start.Line < 2 || j.byDir[filepath.Dir(filepath.Clean(s.DefinedIn))] == nil {
			continue
		}
		gp := j.byDir[filepath.Dir(filepath.Clean(s.DefinedIn))]
		if !contains(gp.GoFiles, filepath.Base(s.DefinedIn)) {
			continue // a file the build excludes (build tags, another OS)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

func withCallers(funcs []*symbols.Symbol, idx *symbols.Index) []*symbols.Symbol {
	var out []*symbols.Symbol
	for _, s := range funcs {
		for _, c := range idx.Callers(s.ID) {
			if c.ID != s.ID && !c.Provenance.IsExternal() {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

func take(list []*symbols.Symbol, n int, ok func(*symbols.Symbol) bool) []*symbols.Symbol {
	var out []*symbols.Symbol
	for _, s := range list {
		if len(out) == n {
			break
		}
		if ok == nil || ok(s) {
			out = append(out, s)
		}
	}
	return out
}

func readLines(s *symbols.Symbol) []string {
	b, err := os.ReadFile(s.DefinedIn)
	if err != nil {
		return nil
	}
	return strings.Split(string(b), "\n")
}

// addParam puts a new first parameter on the function's declaration line. Generic functions are left out.
func addParam(lines []string, s *symbols.Symbol) ([]string, bool) {
	if lines == nil {
		lines = readLines(s)
	}
	i := s.Span.Start.Line - 1
	if i < 0 || i >= len(lines) {
		return nil, false
	}
	re := regexp.MustCompile(`^\s*func\s*(\([^)]*\)\s*)?` + regexp.QuoteMeta(s.Name) + `\(`)
	m := re.FindStringIndex(lines[i])
	if m == nil {
		return nil, false
	}
	line := lines[i]
	p := "rbX int, "
	if m[1] < len(line) && line[m[1]] == ')' {
		p = "rbX int"
	}
	out := append([]string{}, lines...)
	out[i] = line[:m[1]] + p + line[m[1]:]
	return out, true
}

// removeFunc deletes the function's lines.
func removeFunc(lines []string, s *symbols.Symbol) ([]string, bool) {
	a, b := s.Span.Start.Line-1, s.Span.End.Line
	if a < 0 || b > len(lines) || a >= b {
		return nil, false
	}
	return append(append([]string{}, lines[:a]...), lines[b:]...), true
}

// insertStmt puts code after the line that opens the function's body.
func insertStmt(lines []string, s *symbols.Symbol, code string) ([]string, bool) {
	if lines == nil {
		lines = readLines(s)
	}
	for i := s.Span.Start.Line - 1; i < s.Span.End.Line-1 && i < len(lines); i++ {
		if strings.HasSuffix(strings.TrimSpace(lines[i]), "{") {
			out := append([]string{}, lines[:i+1]...)
			out = append(out, strings.Split(code, "\n")...)
			return append(out, lines[i+1:]...), true
		}
	}
	return nil, false
}

func writeAndIndex(idx *symbols.Index, path string, src []byte) {
	_ = os.WriteFile(path, src, 0o644)
	if m, err := genast.ParseFile(path, src); err == nil && m != nil {
		idx.UpdateFile(path, m)
	}
}

// funcAt names the function holding a line of a file, from the file as it is now.
func funcAt(file string, line int, root string) string {
	src, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	m, err := genast.ParseFile(file, src)
	if err != nil || m == nil {
		return ""
	}
	idx := symbols.NewIndex()
	idx.AddModule(m)
	best := ""
	size := 1 << 30
	for _, s := range idx.AllSymbols() {
		if s.Kind != symbols.SymFunc && s.Kind != symbols.SymMethod {
			continue
		}
		if s.Span.Start.Line <= line && line <= s.Span.End.Line && s.Span.End.Line-s.Span.Start.Line < size {
			size = s.Span.End.Line - s.Span.Start.Line
			best = relSlash(root, file) + ":" + s.QualName
		}
	}
	return best
}

func relSlash(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(p)
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func gitOut(dir string, args ...string) string {
	out, _ := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return string(out)
}

// coverageScore runs the tests with coverage and compares, per function, "covered" with "a test reaches it".
func coverageScore(root string, idx *symbols.Index, j *typeJudge, funcs []*symbols.Symbol) *CoverageScore {
	cs := &CoverageScore{ByHops: map[string]CoverageHop{}}
	prof := filepath.Join(filepath.Dir(root), "cover.out")
	cmd := exec.Command("go", "test", "-count=1", "-coverpkg=./...", "-coverprofile="+prof, "./...")
	cmd.Dir = root
	t := time.Now()
	if out, err := cmd.CombinedOutput(); err != nil {
		cs.TestExit = err.Error()
		_ = os.WriteFile(filepath.Join(filepath.Dir(root), "go-test.log"), out, 0o644)
	}
	cs.TestSeconds = time.Since(t).Seconds()
	cov := exec.Command("go", "tool", "cover", "-func="+prof)
	cov.Dir = root // cover finds the source files through the module it runs in
	out, err := cov.Output()
	if err != nil {
		cs.TestExit += " cover: " + err.Error()
		return cs
	}
	// "module/pkg/file.go:12:\tName\t\t75.0%"
	pct := map[string]float64{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || !strings.HasSuffix(f[len(f)-1], "%") {
			continue
		}
		loc := strings.TrimSuffix(f[0], ":")
		k := strings.LastIndex(loc, ":")
		if k < 0 {
			continue
		}
		file, ln := loc[:k], loc[k+1:]
		slash := strings.LastIndex(file, "/")
		gp := j.pkgs[file[:slash]]
		if gp == nil {
			continue
		}
		v, _ := strconv.ParseFloat(strings.TrimSuffix(f[len(f)-1], "%"), 64)
		pct[filepath.Clean(filepath.Join(gp.Dir, file[slash+1:]))+":"+ln] = v
	}
	reach := map[int]func(string) []review.Ref{}
	for _, h := range []int{1, 3, 6, 10, 99} {
		reach[h] = review.TestReach(idx, root, h)
	}
	hops := map[int]*CoverageHop{1: {}, 3: {}, 6: {}, 10: {}, 99: {}}
	for _, s := range funcs {
		v, ok := pct[filepath.Clean(s.DefinedIn)+":"+strconv.Itoa(s.Span.Start.Line)]
		if !ok {
			continue
		}
		cs.Functions++
		covered := v > 0
		if covered {
			cs.Covered++
		}
		for h, r := range reach {
			untested := len(r(s.ID)) == 0
			if h == 3 && untested == covered {
				kind := "says untested, but covered"
				if !untested {
					kind = "says tested, but not covered"
				}
				cs.Disagree = append(cs.Disagree, fmt.Sprintf("%s|%s|%s:%d|%s", kind, s.QualName, relSlash(root, s.DefinedIn), s.Span.Start.Line, s.Kind))
			}
			c := hops[h]
			switch {
			case untested && !covered:
				c.TP++
			case untested && covered:
				c.FP++
			case !untested && !covered:
				c.FN++
			default:
				c.TN++
			}
		}
	}
	for h, c := range hops {
		if c.TP+c.FP > 0 {
			c.Precision = float64(c.TP) / float64(c.TP+c.FP)
		}
		if c.TP+c.FN > 0 {
			c.Recall = float64(c.TP) / float64(c.TP+c.FN)
		}
		if n := c.TP + c.FP + c.FN + c.TN; n > 0 {
			c.Agreement = float64(c.TP+c.TN) / float64(n)
		}
		cs.ByHops[strconv.Itoa(h)] = *c
	}
	return cs
}

// history reviews the last n first-parent commits, each against its parent.
func history(root string, n, hops int) []HistoryRow {
	var rows []HistoryRow
	for _, line := range strings.Split(strings.TrimSpace(gitOut(root, "log", "--first-parent", "-n", strconv.Itoa(n), "--format=%H %s")), "\n") {
		sp := strings.SplitN(line, " ", 2)
		if len(sp) < 2 || strings.TrimSpace(gitOut(root, "rev-parse", "--verify", "-q", sp[0]+"^")) == "" {
			continue
		}
		if exec.Command("git", "-C", root, "checkout", "-q", "--detach", sp[0]).Run() != nil {
			continue
		}
		t := time.Now()
		ctx := agentctx.LoadContext(root)
		rep, err := review.Run(ctx.Index, review.Options{Root: root, Base: sp[0] + "^", Hops: hops})
		if err != nil {
			continue
		}
		pts := map[string]int{}
		for _, p := range rep.Points {
			pts[p.Level]++
		}
		rows = append(rows, HistoryRow{Commit: sp[0][:7], Subject: sp[1], Files: rep.Files, Functions: len(rep.Functions),
			Points: pts, Seconds: time.Since(t).Seconds()})
	}
	return rows
}

// ReviewBenchMarkdown is the short summary a person reads.
func ReviewBenchMarkdown(r *ReviewBenchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# review_diff benchmark\n\nRepo %s at %s (%s). %d Go functions; loading the index took %.1f s.\n\n",
		filepath.Base(r.Repo), r.Commit[:7], r.Date, r.Functions, r.LoadSeconds)
	b.WriteString("| Edit | Cases | Right | Precision | Recall | Median seconds |\n|---|---|---|---|---|---|\n")
	for _, k := range []string{"signature", "remove", "fs", "network", "db", "control", "branch"} {
		s := r.Scores[k]
		if s == nil {
			continue
		}
		// a control case has nothing to find: precision and recall do not apply, only Right does
		prec, rec := "-", "-"
		if s.TP+s.FP > 0 {
			prec = fmt.Sprintf("%.2f", s.Precision)
		}
		if s.TP+s.FN > 0 {
			rec = fmt.Sprintf("%.2f", s.Recall)
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %s | %s | %.2f |\n", k, s.Cases, s.Pass, prec, rec, s.MedianSec)
	}
	if r.Errors > 0 {
		fmt.Fprintf(&b, "\n%d cases did not run (git failed in the copy, twice); they are not scored.\n", r.Errors)
	}
	if c := r.Coverage; c != nil {
		fmt.Fprintf(&b, "\nTests: %d functions, %d covered by go test (%.0f s).\n\n| Hops | Says untested, really not covered | Says untested, but covered | Says tested, but not covered | Agreement |\n|---|---|---|---|---|\n",
			c.Functions, c.Covered, c.TestSeconds)
		for _, h := range []string{"1", "3", "6", "10", "99"} {
			x := c.ByHops[h]
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %.2f |\n", h, x.TP, x.FP, x.FN, x.Agreement)
		}
	}
	if len(r.History) > 0 {
		b.WriteString("\n| Commit | Files | Functions | break | behavior | test | size | Seconds |\n|---|---|---|---|---|---|---|---|\n")
		for _, h := range r.History {
			fmt.Fprintf(&b, "| %s %s | %d | %d | %d | %d | %d | %d | %.1f |\n", h.Commit, trimTo(h.Subject, 50), h.Files, h.Functions,
				h.Points["break"], h.Points["behavior"], h.Points["test"], h.Points["size"], h.Seconds)
		}
	}
	return b.String()
}

func trimTo(s string, n int) string {
	s = strings.ReplaceAll(s, "|", "/")
	if len(s) > n {
		return s[:n-3] + "..."
	}
	return s
}
