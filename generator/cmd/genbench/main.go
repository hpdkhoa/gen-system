// genbench main — v23.32 (stdlib provenance): added `--stdlib` / `--stdlib-lang`
// to the `resolution` subcommand; when set, routes to ResolutionRecallWithStdlib
// which ingests the stdlib read-only and reports a separate with-stdlib recall
// alongside the unchanged honest internal recall. `--stdlib go` expands to
// GOROOT/src. Prior:
// genbench main — v23.25 (task F): `regression` accepts `--baseline` (resolves
// --old to the committed bench/baselines/cobolcraft_v23.24.json) and `--strict`
// (exit non-zero on drift; default is report-only). Enables the frozen COBOL
// baseline + CI-gate workflow without hand-wiring JSON paths.
// genbench main — v23.14 (Phase 6): added `genbench springgen` (Spring Boot project gen check).
// genbench main — v23.13 (Phase 5): added `genbench webgate` (generate web project + tsc/build gate).
// generator/cmd/genbench/main.go — v23.11 (Phase 1): added `postgres`
// subcommand — generates a GEN_BACKEND=postgres app and asserts on the emitted
// migrations + FK-constraint SQL (pure-Go gate; companion verify-postgres.sh
// does the live-DB apply/CRUD/FK-rejection run). Also folded into `all`.
// v19.8b: added `diag` subcommand for resolution-recall debugging. v19.7 base
// unchanged.
//
// genbench is the binary you point at a real codebase (and a running
// Ollama) to produce the four reliability numbers and the four performance
// numbers needed before any incubator demo. Output is a JSON blob plus a
// printed report. Use `genbench regression` to diff two JSON runs.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gen-system/generator/internal/agent"
	"gen-system/generator/internal/bench"
	"gen-system/generator/internal/style"
	agentctx "gen-system/generator/internal/context"
)

const usage = `genbench — reliability and performance bench for gen-system

USAGE
  genbench <subcommand> [flags]

SUBCOMMANDS
  model         Doctor: is Ollama up, is the model loaded, does it stay resident?
  resolution    Symbol-resolution recall on the project (% of call edges resolved)
  cfg           CFG structural correctness (entry/exit/orphan/successor rules)
  effects       Effect-inference precision/recall against a labels file
  diff          Schema diff regression bench (v19 ghost-entry fix locked in)
  postgres      GEN_BACKEND=postgres generation gate (migrations + FK SQL)
  parse         Cold parse throughput per language
  loadctx       LoadContext end-to-end latency (plain / copybook / warm / skill)
  incremental   Watcher: file-change → event → index update latency
  gen           LLM generation latency (tokens/sec, eval ms)
  stream        TTFT + total latency, stream on vs off, same prompt (v26.24)
  genpipe       End-to-end Generate pipeline: idea → plan → generate → heal →
                verify; reports ops_total/ops_stubbed/repairs/heals/go_test_pass
                (v26.24, WP1). GEN_LOGIC=ollama for live model, replay otherwise.
  localize      Localization recall on any repo: are the files a gold patch touched
                inside the call graph neighbourhood of seed identifiers? Needs
                --seeds-file and --gold-file; see bench/swebench/ (v26.31).
  stylecheck    Team style gate on a Go tree: findings per rule, errors, warnings
                (v26.33). --profile <gen-style.json>, default the built-in profile.
                genpipe also takes --style and --style-mode to run the gate
                inside the pipeline and report ops_style_* metrics.
  determinism   Same prompt N times, diff outputs (temp=0 seed=42 must be stable)
  all           Run every suite and write one JSON
  regression    Diff a new JSON against an older JSON or the committed baseline
  diag          v19.8b: print raw resolution state for debugging 0% recall

COMMON FLAGS
  --root        project root for codebase suites (default ".")
  --copybooks   colon-separated COBOL copybook dirs (also GEN_COBOL_COPYBOOK_PATH)
  --skill       path to a .skill file for the loadctx skill_warmup leg
  --model       Ollama model name (default qwen2.5-coder:14b)
  --host        Ollama base URL (default http://localhost:11434)
  --out         JSON output path (default bench-YYYYMMDD-HHMMSS.json)

EXAMPLES
  # one-shot reliability + perf, single JSON, no AI suites
  genbench all --root /path/to/repo --copybooks /path/to/copybooks --skip-ai

  # everything including the LLM
  genbench all --root /path/to/repo --model qwen2.5-coder:14b

  # compare a fresh run against last week's
  genbench regression --old bench-20260601.json --new bench-20260608.json --tolerance 0.05

  # compare a fresh COBOL run against the frozen baseline (report-only)
  genbench regression --baseline --new bench-fresh.json

  # same, but fail the build (exit 1) if any metric drifts beyond tolerance
  genbench regression --baseline --new bench-fresh.json --strict --tolerance 0.02

See BENCH_GUIDE.md for the runbook.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	sub := os.Args[1]
	args := os.Args[2:]
	switch sub {
	case "model":
		cmdModel(args)
	case "resolution":
		cmdResolution(args)
	case "cfg":
		cmdCFG(args)
	case "effects":
		cmdEffects(args)
	case "diff":
		cmdDiff(args)
	case "postgres":
		cmdPostgres(args)
	case "webgate":
		cmdWebGate(args)
	case "springgen":
		cmdSpringGen(args)
	case "parse":
		cmdParse(args)
	case "loadctx":
		cmdLoadCtx(args)
	case "incremental":
		cmdIncremental(args)
	case "gen":
		cmdGen(args)
	case "stream":
		cmdStream(args)
	case "genpipe":
		cmdGenPipe(args)
	case "localize":
		cmdLocalize(args)
	case "stylecheck":
		cmdStyleCheck(args)
	case "determinism":
		cmdDeterminism(args)
	case "all":
		cmdAll(args)
	case "regression":
		cmdRegression(args)
	case "diag":
		cmdDiag(args)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", sub)
		fmt.Print(usage)
		os.Exit(2)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────

// v23.35: default bench model matches install.sh's code model (the one that
// writes service logic), not the retired deepseek default.
const defaultModel = "qwen2.5-coder:14b"
const defaultHost = "http://localhost:11434"

type commonFlags struct {
	root      string
	copybooks string
	skill     string
	model     string
	host      string
	out       string
	skipAI    bool
	tolerance float64
	old       string
	newPath   string
	baseline  bool
	strict    bool
	labels    string
	runs      int
	threshold float64
	stdlib    string // v23.32: stdlib roots, colon-separated; "go" expands to GOROOT/src
	stdlibLang string // v23.32: advisory language label for the stdlib roots
}

func parseCommon(fs *flag.FlagSet, args []string) commonFlags {
	cf := commonFlags{}
	fs.StringVar(&cf.root, "root", ".", "project root")
	fs.StringVar(&cf.copybooks, "copybooks", os.Getenv("GEN_COBOL_COPYBOOK_PATH"), "colon-separated COBOL copybook dirs")
	fs.StringVar(&cf.skill, "skill", "", "path to .skill file (loadctx skill_warmup leg)")
	fs.StringVar(&cf.model, "model", defaultModel, "Ollama model name")
	fs.StringVar(&cf.host, "host", defaultHost, "Ollama base URL")
	fs.StringVar(&cf.out, "out", "", "JSON output path (default bench-<ts>.json)")
	fs.BoolVar(&cf.skipAI, "skip-ai", false, "skip suites that require Ollama")
	fs.Float64Var(&cf.tolerance, "tolerance", 0.05, "regression tolerance (relative, e.g. 0.05 = 5%)")
	fs.StringVar(&cf.old, "old", "", "old run JSON for regression")
	fs.StringVar(&cf.newPath, "new", "", "new run JSON for regression")
	fs.BoolVar(&cf.baseline, "baseline", false, "regression: use the committed COBOL baseline as --old")
	fs.BoolVar(&cf.strict, "strict", false, "regression: exit non-zero when a metric regresses beyond tolerance")
	fs.StringVar(&cf.labels, "labels", "", "effect-labels JSON path")
	fs.IntVar(&cf.runs, "runs", 5, "number of runs (gen/determinism)")
	fs.Float64Var(&cf.threshold, "threshold", 0.90, "resolution-recall pass threshold (0..1)")
	fs.StringVar(&cf.stdlib, "stdlib", "", "v26.5: comma-separated stdlib roots, language PER ROOT. Shorthands: 'go' → GOROOT/src, 'cobol' → built-in runtime catalog. Explicit: 'java=/opt/jdk/java.base'. Mixable: 'go,java=/opt/jdk/java.base'. Empty = no stdlib (the honest project-only benchmark). 'auto' is REJECTED here: a benchmark must be reproducible.")
	fs.StringVar(&cf.stdlibLang, "stdlib-lang", "", "DEPRECATED (v26.5): legacy single language label for bare --stdlib paths. Prefer the per-root 'lang=path' form.")
	_ = fs.Parse(args)
	return cf
}

// stdlibRootsFor turns the --stdlib flag into concrete StdlibRoot entries.
//
// v26.5: delegates to the SHARED context.ParseStdlibSpec — this was the third
// hand-rolled copy of the same "go"/"cobol" expansion (gen-server had two more)
// and it had already drifted: its synthetic COBOL root omitted AutoScope:false,
// and like the others it applied ONE --stdlib-lang to EVERY path, which made a
// multi-language spec impossible to express. The shared parser takes the
// language per root ("java=/opt/jdk/java.base").
//
// NOTE the deliberate difference from the servers: genbench does NOT support
// auto-detection. Its whole purpose is the honest internal-recall number, and a
// benchmark must measure exactly what it was told to measure — a root set that
// varies with the machine's GOROOT/JAVA_HOME would make results irreproducible.
// So `--stdlib auto` is REJECTED here (loudly, rather than silently ingesting
// nothing), and an empty --stdlib means no stdlib at all.
func stdlibRootsFor(spec, langLabel string) []agentctx.StdlibRoot {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil // benchmark default: measure the project alone
	}
	if strings.EqualFold(spec, agentctx.StdlibSpecAuto) {
		fmt.Fprintln(os.Stderr,
			"genbench: --stdlib auto is not supported: a benchmark must be reproducible, "+
				"and detected roots vary with the machine (GOROOT / JAVA_HOME). "+
				"Name the roots explicitly, e.g. --stdlib go  or  --stdlib go,java=/opt/jdk/java.base")
		os.Exit(2)
	}
	roots, _ := agentctx.ParseStdlibSpec(spec, langLabel)
	return roots
}

func splitCopybooks(s string) []string {
	if s == "" {
		return nil
	}
	sep := string(os.PathListSeparator)
	parts := strings.Split(s, sep)
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func newRun(model, host, root string) bench.Run {
	return bench.Run{
		Schema:    "genbench/v1",
		StartedAt: time.Now(),
		HostInfo: bench.HostInfo{
			OS: runtime.GOOS, Arch: runtime.GOARCH,
			NumCPU: runtime.NumCPU(), GoVersion: runtime.Version(),
		},
		ModelInfo:   bench.ModelInfo{Name: model, Endpoint: host},
		ProjectRoot: root,
	}
}

func finish(r bench.Run, outPath string) {
	r.FinishedAt = time.Now()
	if outPath == "" {
		outPath = fmt.Sprintf("bench-%s.json", r.StartedAt.Format("20060102-150405"))
	}
	bench.RenderTerminal(os.Stdout, r)
	if err := bench.WriteJSON(r, outPath); err != nil {
		fmt.Fprintln(os.Stderr, "write json:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "\nWrote %s\n", outPath)

	// Exit non-zero if any case failed.
	for _, s := range r.Suites {
		_, fail := s.PassFailSummary()
		if fail > 0 {
			os.Exit(1)
		}
	}
}

// ─── subcommand: model ───────────────────────────────────────────────

func cmdModel(args []string) {
	fs := flag.NewFlagSet("model", flag.ExitOnError)
	cf := parseCommon(fs, args)
	r := newRun(cf.model, cf.host, cf.root)
	r.Suites = []bench.Suite{bench.OllamaDoctor(cf.host, cf.model)}
	finish(r, cf.out)
}

// ─── subcommand: resolution ──────────────────────────────────────────

func cmdResolution(args []string) {
	fs := flag.NewFlagSet("resolution", flag.ExitOnError)
	cf := parseCommon(fs, args)
	abs, _ := filepath.Abs(cf.root)
	r := newRun(cf.model, cf.host, abs)
	if roots := stdlibRootsFor(cf.stdlib, cf.stdlibLang); len(roots) > 0 {
		// v23.32: stdlib-inclusive resolution. Reports honest internal recall
		// (project symbols only) AND a separate with-stdlib number.
		r.Suites = []bench.Suite{
			bench.ResolutionRecallWithStdlib(abs, splitCopybooks(cf.copybooks), roots, cf.threshold),
		}
	} else {
		r.Suites = []bench.Suite{bench.ResolutionRecall(abs, splitCopybooks(cf.copybooks), cf.threshold)}
	}
	finish(r, cf.out)
}

// ─── subcommand: cfg ─────────────────────────────────────────────────

func cmdCFG(args []string) {
	fs := flag.NewFlagSet("cfg", flag.ExitOnError)
	cf := parseCommon(fs, args)
	abs, _ := filepath.Abs(cf.root)
	r := newRun(cf.model, cf.host, abs)
	r.Suites = []bench.Suite{bench.CFGCorrectness(abs, splitCopybooks(cf.copybooks))}
	finish(r, cf.out)
}

// ─── subcommand: effects ─────────────────────────────────────────────

func cmdEffects(args []string) {
	fs := flag.NewFlagSet("effects", flag.ExitOnError)
	cf := parseCommon(fs, args)
	if cf.labels == "" {
		fmt.Fprintln(os.Stderr, "effects: --labels <path> is required")
		os.Exit(2)
	}
	abs, _ := filepath.Abs(cf.root)
	r := newRun(cf.model, cf.host, abs)
	r.Suites = []bench.Suite{bench.EffectPrecisionRecall(abs, splitCopybooks(cf.copybooks), cf.labels)}
	finish(r, cf.out)
}

// ─── subcommand: diff ────────────────────────────────────────────────

func cmdDiff(args []string) {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	cf := parseCommon(fs, args)
	r := newRun(cf.model, cf.host, cf.root)
	r.Suites = []bench.Suite{bench.DiffCorrectness()}
	finish(r, cf.out)
}

// ─── subcommand: postgres ────────────────────────────────────────────
// Pure-Go generation gate for the Postgres layer. Generates a
// GEN_BACKEND=postgres app from a fixed two-entity-with-FK fixture and asserts
// the migrations + 900_foreign_keys.sql are emitted correctly. The live-DB
// apply / CRUD / FK-rejection run is verify-postgres.sh's job.

func cmdPostgres(args []string) {
	fs := flag.NewFlagSet("postgres", flag.ExitOnError)
	cf := parseCommon(fs, args)
	r := newRun(cf.model, cf.host, cf.root)
	r.Suites = []bench.Suite{bench.PostgresGeneration()}
	finish(r, cf.out)
}

// ─── subcommand: webgate (Phase 5) ───────────────────────────────────

func cmdWebGate(args []string) {
	fs := flag.NewFlagSet("webgate", flag.ExitOnError)
	cf := parseCommon(fs, args)
	r := newRun(cf.model, cf.host, cf.root)
	r.Suites = []bench.Suite{bench.WebGate()}
	finish(r, cf.out)
}

// âââ subcommand: springgen (Phase 6) âââ

func cmdSpringGen(args []string) {
	fs := flag.NewFlagSet("springgen", flag.ExitOnError)
	cf := parseCommon(fs, args)
	r := newRun(cf.model, cf.host, cf.root)
	r.Suites = []bench.Suite{bench.SpringGeneration()}
	finish(r, cf.out)
}

// ─── subcommand: parse ───────────────────────────────────────────────

func cmdParse(args []string) {
	fs := flag.NewFlagSet("parse", flag.ExitOnError)
	cf := parseCommon(fs, args)
	abs, _ := filepath.Abs(cf.root)
	r := newRun(cf.model, cf.host, abs)
	r.Suites = []bench.Suite{bench.ParseThroughput(abs)}
	finish(r, cf.out)
}

// ─── subcommand: loadctx ─────────────────────────────────────────────

func cmdLoadCtx(args []string) {
	fs := flag.NewFlagSet("loadctx", flag.ExitOnError)
	cf := parseCommon(fs, args)
	abs, _ := filepath.Abs(cf.root)
	r := newRun(cf.model, cf.host, abs)
	r.Suites = []bench.Suite{bench.LoadContextLatency(abs, splitCopybooks(cf.copybooks), cf.skill)}
	finish(r, cf.out)
}

// ─── subcommand: incremental ─────────────────────────────────────────

func cmdIncremental(args []string) {
	fs := flag.NewFlagSet("incremental", flag.ExitOnError)
	cf := parseCommon(fs, args)
	r := newRun(cf.model, cf.host, cf.root)
	r.Suites = []bench.Suite{bench.IncrementalUpdate()}
	finish(r, cf.out)
}

// ─── subcommand: gen ─────────────────────────────────────────────────

func cmdGen(args []string) {
	fs := flag.NewFlagSet("gen", flag.ExitOnError)
	cf := parseCommon(fs, args)
	r := newRun(cf.model, cf.host, cf.root)
	r.Suites = []bench.Suite{bench.GenerationLatency(cf.host, cf.model, cf.runs)}
	finish(r, cf.out)
}

// ─── subcommand: stream (v26.24, WP2) ────────────────────────────────

func cmdStream(args []string) {
	fs := flag.NewFlagSet("stream", flag.ExitOnError)
	cf := parseCommon(fs, args)
	r := newRun(cf.model, cf.host, cf.root)
	r.Suites = []bench.Suite{bench.StreamingLatency(cf.host, cf.model, cf.runs)}
	finish(r, cf.out)
}

// ─── subcommand: genpipe (v26.24, WP1) ───────────────────────────────
// End-to-end Generate pipeline benchmark. Ideas come from --ideas (a JSON file
// with a "generate_ideas" array, e.g. bench/tasks.json) or the built-in frozen
// set. The suite uses the production entry points; GEN_LOGIC / OLLAMA_MODEL_*
// select the provider and models exactly as they do for `gen idea`.

func cmdGenPipe(args []string) {
	fs := flag.NewFlagSet("genpipe", flag.ExitOnError)
	ideasPath := fs.String("ideas", "", "JSON file with a generate_ideas array (default: built-in frozen set)")
	stylePath := fs.String("style", os.Getenv("GEN_STYLE"), "v26.33: team style profile (gen-style.json); also GEN_STYLE")
	styleMode := fs.String("style-mode", os.Getenv("GEN_STYLE_MODE"), "v26.33: off|report|gate|autofix; empty = the profile's own mode; also GEN_STYLE_MODE")
	cf := parseCommon(fs, args)
	ideas := loadIdeas(*ideasPath)
	if *stylePath != "" {
		p, err := style.Load(*stylePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "genpipe: --style:", err)
			os.Exit(2)
		}
		agent.SetStyle(p, style.Mode(*styleMode))
		defer agent.SetStyle(nil, "")
	}
	r := newRun(cf.model, cf.host, cf.root)
	r.Suites = []bench.Suite{bench.GenPipeline(ideas)}
	finish(r, cf.out)
}

// ─── subcommand: stylecheck (v26.33) ─────────────────────────────────
// The team style gate on an existing Go tree. Deterministic, no model.

func cmdStyleCheck(args []string) {
	fs := flag.NewFlagSet("stylecheck", flag.ExitOnError)
	profilePath := fs.String("profile", "", "gen-style.json (default: the built-in default profile)")
	cf := parseCommon(fs, args)
	p := style.Default()
	if *profilePath != "" {
		loaded, err := style.Load(*profilePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "stylecheck: --profile:", err)
			os.Exit(2)
		}
		p = loaded
	}
	abs, _ := filepath.Abs(cf.root)
	r := newRun(cf.model, cf.host, abs)
	r.Suites = []bench.Suite{bench.StyleGate(abs, p)}
	finish(r, cf.out)
}

// loadIdeas reads {"generate_ideas": ["...", ...]} from path; empty path or a
// missing/invalid file falls back to the built-in set (loudly).
func loadIdeas(path string) []string {
	if path == "" {
		return nil // GenPipeline falls back to bench.BuiltinGenIdeas
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "genpipe: cannot read --ideas %s (%v); using built-in set\n", path, err)
		return nil
	}
	var doc struct {
		GenerateIdeas []string `json:"generate_ideas"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc.GenerateIdeas) == 0 {
		fmt.Fprintf(os.Stderr, "genpipe: %s has no generate_ideas array; using built-in set\n", path)
		return nil
	}
	return doc.GenerateIdeas
}

// ─── subcommand: localize (v26.31) ───────────────────────────────────
// Localization recall on an arbitrary repository. NOT a solve rate: see
// bench/swebench/README.md for the boundary. Seeds and gold files come from
// files so a 500 task driver never hits argv limits.

func cmdLocalize(args []string) {
	fs := flag.NewFlagSet("localize", flag.ExitOnError)
	seedsFile := fs.String("seeds-file", "", "file with one seed identifier per line (required)")
	goldFile := fs.String("gold-file", "", "file with one gold file path per line, relative to --root (required)")
	hops := fs.Int("hops", 2, "maximum call graph hops from the seeds")
	cf := parseCommon(fs, args)
	if *seedsFile == "" || *goldFile == "" {
		fmt.Fprintln(os.Stderr, "localize: --seeds-file and --gold-file are required")
		os.Exit(2)
	}
	seeds, err := bench.ReadLines(*seedsFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "localize: seeds:", err)
		os.Exit(2)
	}
	gold, err := bench.ReadLines(*goldFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "localize: gold:", err)
		os.Exit(2)
	}
	abs, _ := filepath.Abs(cf.root)
	r := newRun(cf.model, cf.host, abs)
	r.Suites = []bench.Suite{bench.LocalizationRecall(abs, seeds, gold, *hops)}
	finish(r, cf.out)
}

// ─── subcommand: determinism ─────────────────────────────────────────

func cmdDeterminism(args []string) {
	fs := flag.NewFlagSet("determinism", flag.ExitOnError)
	cf := parseCommon(fs, args)
	r := newRun(cf.model, cf.host, cf.root)
	r.Suites = []bench.Suite{bench.DeterminismCheck(cf.host, cf.model, cf.runs)}
	finish(r, cf.out)
}

// ─── subcommand: all ─────────────────────────────────────────────────

func cmdAll(args []string) {
	fs := flag.NewFlagSet("all", flag.ExitOnError)
	cf := parseCommon(fs, args)
	abs, _ := filepath.Abs(cf.root)
	r := newRun(cf.model, cf.host, abs)

	cb := splitCopybooks(cf.copybooks)

	// Order matters: cheap/local first, AI last.
	r.Suites = append(r.Suites, bench.DiffCorrectness())
	r.Suites = append(r.Suites, bench.PostgresGeneration())
	r.Suites = append(r.Suites, bench.ParseThroughput(abs))
	r.Suites = append(r.Suites, bench.ResolutionRecall(abs, cb, cf.threshold))
	r.Suites = append(r.Suites, bench.CFGCorrectness(abs, cb))
	r.Suites = append(r.Suites, bench.LoadContextLatency(abs, cb, cf.skill))
	r.Suites = append(r.Suites, bench.IncrementalUpdate())
	if cf.labels != "" {
		r.Suites = append(r.Suites, bench.EffectPrecisionRecall(abs, cb, cf.labels))
	}
	if !cf.skipAI {
		r.Suites = append(r.Suites, bench.OllamaDoctor(cf.host, cf.model))
		r.Suites = append(r.Suites, bench.GenerationLatency(cf.host, cf.model, cf.runs))
		r.Suites = append(r.Suites, bench.DeterminismCheck(cf.host, cf.model, cf.runs))
		// v26.24 (WP1/WP2): the end-to-end pipeline + streaming suites join
		// `all` on the AI side only, so offline `all --skip-ai` runs (and the
		// frozen baselines diffed against them) are byte-identical to before.
		r.Suites = append(r.Suites, bench.StreamingLatency(cf.host, cf.model, cf.runs))
		r.Suites = append(r.Suites, bench.GenPipeline(nil))
	}
	finish(r, cf.out)
}

// ─── subcommand: regression ──────────────────────────────────────────

func cmdRegression(args []string) {
	fs := flag.NewFlagSet("regression", flag.ExitOnError)
	cf := parseCommon(fs, args)

	oldPath := cf.old
	if cf.baseline {
		if oldPath != "" {
			fmt.Fprintln(os.Stderr, "regression: pass either --baseline or --old, not both")
			os.Exit(2)
		}
		p, err := findBaseline(cf.root)
		if err != nil {
			fmt.Fprintln(os.Stderr, "regression:", err)
			os.Exit(2)
		}
		oldPath = p
	}
	if oldPath == "" || cf.newPath == "" {
		fmt.Fprintln(os.Stderr, "regression: --old <path> (or --baseline) and --new <path> required")
		os.Exit(2)
	}
	oldRun, err := bench.ReadJSON(oldPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read old:", err)
		os.Exit(2)
	}
	newRun, err := bench.ReadJSON(cf.newPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read new:", err)
		os.Exit(2)
	}
	lines, ok := bench.Regression(oldRun, newRun, cf.tolerance)
	gate := "report-only"
	if cf.strict {
		gate = "strict"
	}
	fmt.Printf("regression [%s]: tolerance=%.0f%%  %s → %s\n",
		gate,
		cf.tolerance*100,
		oldRun.StartedAt.Format(time.RFC3339),
		newRun.StartedAt.Format(time.RFC3339))
	if cf.baseline {
		fmt.Printf("baseline: %s\n", oldPath)
	}
	fmt.Println(strings.Repeat("─", 70))
	for _, l := range lines {
		fmt.Println(l)
	}
	fmt.Println(strings.Repeat("─", 70))
	if !ok {
		fmt.Println("DRIFT — at least one metric regressed beyond tolerance")
		if cf.strict {
			os.Exit(1)
		}
		return
	}
	fmt.Println("OK — no metric regressed beyond tolerance")
}

// findBaseline locates the committed COBOL baseline JSON. It walks up from the
// given root (and from the current working dir as a fallback) looking for
// bench/baselines/cobolcraft_v23.24.json, so `--baseline` works whether genbench
// is invoked from the repo root or a subdir.
func findBaseline(root string) (string, error) {
	const rel = "generator/internal/bench/baselines/cobolcraft_v23.24.json"
	starts := []string{}
	if root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			starts = append(starts, abs)
		}
	}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	for _, start := range starts {
		dir := start
		for {
			cand := filepath.Join(dir, rel)
			if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
				return cand, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", fmt.Errorf("baseline not found (looked for %s walking up from root/cwd); "+
		"pass --old <path> explicitly or run from inside the repo", rel)
}

// ─── subcommand: diag ────────────────────────────────────────────────

func cmdDiag(args []string) {
	fs := flag.NewFlagSet("diag", flag.ExitOnError)
	cf := parseCommon(fs, args)
	abs, _ := filepath.Abs(cf.root)
	r := newRun(cf.model, cf.host, abs)
	r.Suites = []bench.Suite{bench.ResolutionDiagnostic(abs, splitCopybooks(cf.copybooks), 10)}
	finish(r, cf.out)
}
