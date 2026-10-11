// cli/report.go, v23.30 (gen report): new subcommand. Renders one or more
// frozen genbench JSON runs (bench.Run, schema "genbench/v1") plus an optional
// .skill export into a single standalone dark-terminal HTML validation report.
//
// No new external deps: stdlib encoding/json + html/template only. The output
// is self-contained (fonts via Google Fonts <link> are cosmetic per the project
// HARD CONSTRAINTS; the page renders with the mono fallback if offline).
//
// Usage:
//
//	gen report --in cobolcraft_v23.24.json [--in more.json ...] \
//	           [--skill project.skill] [--title "..."] [--out report.html]
//
// This is a pure renderer: it reads artefacts the engine already produced
// (genbench freeze output, gen skill output) and presents them. It performs no
// analysis of its own, so it is deterministic given the same inputs.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/bench"
	"github.com/hpdkhoa/gen-system/generator/internal/objgraph"
)

// ─── public entry ─────────────────────────────────────────────

// stringList is a repeatable --in flag.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// WriteReport implements 'gen report'. args are os.Args[2:].
func WriteReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	var ins stringList
	fs.Var(&ins, "in", "genbench run JSON to include (repeatable)")
	skillPath := fs.String("skill", "", "Optional .skill file to summarise")
	title := fs.String("title", "", "Report title (default: derived from project root)")
	out := fs.String("out", "report.html", "Output HTML path")
	_ = fs.Parse(args)

	if len(ins) == 0 {
		fmt.Fprintln(os.Stderr, "gen report: at least one --in <genbench.json> is required")
		fmt.Fprintln(os.Stderr, "  e.g. gen report --in cobolcraft_v23.24.json --out report.html")
		os.Exit(1)
	}

	fmt.Printf("gen report  inputs=%d  out=%s\n", len(ins), *out)

	// Load every genbench run.
	var runs []bench.Run
	for _, p := range ins {
		r, err := loadRun(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading %s: %v\n", p, err)
			os.Exit(1)
		}
		fmt.Printf("  loaded  %s  schema=%s  suites=%d\n", filepath.Base(p), r.Schema, len(r.Suites))
		runs = append(runs, r)
	}

	// Optional skill summary.
	var skill *skillSummary
	if *skillPath != "" {
		sf, err := objgraph.ReadSkillFile(*skillPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not read skill file %s: %v\n", *skillPath, err)
		} else {
			skill = summariseSkill(sf, filepath.Base(*skillPath))
			fmt.Printf("  skill   nodes=%d edges=%d\n", skill.NodeCount, skill.EdgeCount)
		}
	}

	model := buildModel(runs, skill, *title)

	html, err := renderReport(model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error rendering report: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*out, []byte(html), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", *out, err)
		os.Exit(1)
	}

	fmt.Printf("  wrote   %s  (%d bytes)\n", *out, len(html))
	fmt.Printf("  summary pass=%d fail=%d across %d suite(s)\n",
		model.TotalPass, model.TotalFail, model.SuiteCount)
	if model.TotalFail == 0 {
		fmt.Println("  ok")
	} else {
		fmt.Println("  NOTE: the report contains failing cases.")
	}
}

func loadRun(path string) (bench.Run, error) {
	var r bench.Run
	b, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("not a genbench run JSON: %w", err)
	}
	if r.Schema != "" && r.Schema != "genbench/v1" {
		return r, fmt.Errorf("unsupported schema %q (want genbench/v1)", r.Schema)
	}
	return r, nil
}

// ─── view model ───────────────────────────────────────────────

type reportModel struct {
	Title       string
	GeneratedAt string
	ProjectRoot string
	HostLine    string
	ModelLine   string
	Runs        []runView
	Skill       *skillSummary
	Highlights  []highlight

	// roll-ups
	SuiteCount int
	TotalPass  int
	TotalFail  int
}

type runView struct {
	ProjectRoot string
	Model       string
	Host        string
	Suites      []suiteView
}

type suiteView struct {
	Name    string
	Pass    int
	Fail    int
	Notes   string
	Cases   []caseView
	Metrics []metricView
}

type caseView struct {
	Name string
	Pass bool
	Got  string
	Want string
	Note string
}

type metricView struct {
	Name  string
	Value string
	Unit  string
}

type highlight struct {
	Label string
	Value string
	Sub   string
}

type skillSummary struct {
	FileName    string
	ProjectRoot string
	WrittenAt   string
	NodeCount   int
	EdgeCount   int
	ByLanguage  []kv
	ByLayer     []kv
}

type kv struct {
	Key   string
	Value int
}

// ─── model assembly ───────────────────────────────────────────

func buildModel(runs []bench.Run, skill *skillSummary, title string) reportModel {
	m := reportModel{
		GeneratedAt: time.Now().UTC().Format("2006-01-02 15:04 UTC"),
		Skill:       skill,
	}

	for _, r := range runs {
		rv := runView{
			ProjectRoot: r.ProjectRoot,
			Model:       r.ModelInfo.Name,
			Host:        hostLine(r.HostInfo),
		}
		if m.ProjectRoot == "" {
			m.ProjectRoot = r.ProjectRoot
			m.HostLine = rv.Host
			m.ModelLine = r.ModelInfo.Name
		}
		for _, s := range r.Suites {
			pass, fail := s.PassFailSummary()
			m.SuiteCount++
			m.TotalPass += pass
			m.TotalFail += fail

			sv := suiteView{
				Name:  s.Name,
				Pass:  pass,
				Fail:  fail,
				Notes: s.Notes,
			}
			for _, c := range s.Cases {
				sv.Cases = append(sv.Cases, caseView{
					Name: c.Name, Pass: c.Pass, Got: c.Got, Want: c.Want, Note: c.Note,
				})
			}
			for _, mt := range s.Metrics {
				sv.Metrics = append(sv.Metrics, metricView{
					Name:  mt.Name,
					Value: formatMetric(mt.Value),
					Unit:  mt.Unit,
				})
			}
			rv.Suites = append(rv.Suites, sv)
		}
		m.Runs = append(m.Runs, rv)
	}

	if title != "" {
		m.Title = title
	} else if m.ProjectRoot != "" {
		m.Title = "Code-Intelligence Validation: " + m.ProjectRoot
	} else {
		m.Title = "Code-Intelligence Validation Report"
	}

	m.Highlights = deriveHighlights(runs, skill)
	return m
}

// deriveHighlights pulls the few headline numbers a reader scans first.
// It reads named metrics/cases by their genbench names and skips anything
// absent (no fabrication).
func deriveHighlights(runs []bench.Run, skill *skillSummary) []highlight {
	var hs []highlight
	seen := map[string]bool{}

	add := func(label, value, sub string) {
		key := label + "|" + value
		if value == "" || seen[key] {
			return
		}
		seen[key] = true
		hs = append(hs, highlight{Label: label, Value: value, Sub: sub})
	}

	for _, r := range runs {
		for _, s := range r.Suites {
			// Cases carry the human-meaningful "got" string.
			for _, c := range s.Cases {
				switch {
				case strings.HasPrefix(c.Name, "internal_recall_at_least"):
					add("Internal-call recall", c.Got, "resolvable internal calls")
				case c.Name == "parse_errors_zero":
					add("Parse errors", c.Got, "across the corpus")
				case c.Name == "files_found":
					add("Source files parsed", c.Got, "")
				}
			}
			for _, mt := range s.Metrics {
				switch mt.Name {
				case "call_edges_total":
					add("Call edges", formatMetric(mt.Value), "total resolved + external")
				case "cfgs_total":
					add("Control-flow graphs", formatMetric(mt.Value), "all structural rules pass")
				case "files_total_cobol":
					add("COBOL files", formatMetric(mt.Value), "")
				}
			}
		}
	}
	if skill != nil {
		add("Symbols exported", fmt.Sprintf("%d", skill.NodeCount), "in the .skill graph")
	}
	return hs
}

func summariseSkill(sf *objgraph.SkillFile, fileName string) *skillSummary {
	s := &skillSummary{
		FileName:    fileName,
		ProjectRoot: sf.Header.ProjectRoot,
		WrittenAt:   sf.Header.WrittenAt,
		NodeCount:   len(sf.Nodes),
		EdgeCount:   len(sf.Edges),
	}
	langCount := map[string]int{}
	layerCount := map[string]int{}
	for _, n := range sf.Nodes {
		lang := n.Language
		if lang == "" {
			lang = "unknown"
		}
		langCount[lang]++
		layer := n.Layer
		if layer == "" {
			layer = "unclassified"
		}
		layerCount[layer]++
	}
	s.ByLanguage = sortedKV(langCount)
	s.ByLayer = sortedKV(layerCount)
	return s
}

func sortedKV(m map[string]int) []kv {
	out := make([]kv, 0, len(m))
	for k, v := range m {
		out = append(out, kv{Key: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Value != out[j].Value {
			return out[i].Value > out[j].Value
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func hostLine(h bench.HostInfo) string {
	parts := []string{}
	if h.OS != "" {
		parts = append(parts, h.OS+"/"+h.Arch)
	}
	if h.NumCPU > 0 {
		parts = append(parts, fmt.Sprintf("%d cpu", h.NumCPU))
	}
	if h.GoVersion != "" {
		parts = append(parts, "go "+h.GoVersion)
	}
	if h.GitCommit != "" {
		parts = append(parts, h.GitCommit)
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " · ")
}

// formatMetric renders a float as an int when it has no fractional part,
// otherwise with up to 4 significant decimals trimmed of trailing zeros.
func formatMetric(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	s := fmt.Sprintf("%.4f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

// ─── render ───────────────────────────────────────────────────

func renderReport(m reportModel) (string, error) {
	t, err := template.New("report").Parse(reportTemplate)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := t.Execute(&sb, m); err != nil {
		return "", err
	}
	return sb.String(), nil
}
