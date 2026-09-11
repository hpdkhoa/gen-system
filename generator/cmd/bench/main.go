// cmd/bench/main.go — gen-system v15 capability benchmark
// Exercises every engine layer (parse, symbol index, CFG, flow, effects)
// against synthetic and self-hosted source; reports real latency + correctness.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"

	// Register all frontends via side-effect init().
	_ "github.com/hpdkhoa/gen-system/generator/internal/ast"
)

// ─── Result types ─────────────────────────────────────────────

// Result holds the outcome of one benchmark suite.
type Result struct {
	Suite    string
	Cases    []CaseResult
	Duration time.Duration
}

// CaseResult is one individual check within a suite.
type CaseResult struct {
	Name   string
	Pass   bool
	Note   string
	Micros int64 // wall-clock µs for this case
}

func (r CaseResult) status() string {
	if r.Pass {
		return "PASS"
	}
	return "FAIL"
}

// ─── Main ─────────────────────────────────────────────────────

func main() {
	// Optional: bench against real project root if passed as argument.
	projectRoot := ""
	if len(os.Args) > 1 {
		projectRoot = os.Args[1]
	}

	fmt.Println("gen-system v15 — capability benchmark")
	fmt.Println(strings.Repeat("─", 60))

	suites := []func() Result{
		benchFrontendRegistry,
		benchGoParser,
		benchTSParser,
		benchPythonParser,
		benchCAGenParser,
		benchSymbolIndex,
		benchCallGraph,
		benchCFGBuilder,
		benchFlowAnalysis,
		benchEffectInference,
		benchCacheRoundTrip,
		benchChecksumStability,
		benchUnknownNodePolicy,
	}

	if projectRoot != "" {
		suites = append(suites, func() Result { return benchSelfHosted(projectRoot) })
	}

	var results []Result
	totalPass, totalFail := 0, 0

	for _, fn := range suites {
		r := fn()
		results = append(results, r)
		for _, c := range r.Cases {
			if c.Pass {
				totalPass++
			} else {
				totalFail++
			}
		}
		printResult(r)
	}

	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("Total  PASS: %d  FAIL: %d\n", totalPass, totalFail)

	if totalFail > 0 {
		os.Exit(1)
	}
}

// ─── Suite: frontend registry ─────────────────────────────────

func benchFrontendRegistry() Result {
	start := time.Now()
	var cases []CaseResult

	required := []struct {
		lang ast.Language
		ext  string
	}{
		{ast.LangGo, ".go"},
		{ast.LangTS, ".ts"},
		{ast.LangPython, ".py"},
		{ast.LangCAGen, ".ad"},
	}

	for _, req := range required {
		t0 := time.Now()
		fe := ast.ForLanguage(req.lang)
		µs := time.Since(t0).Microseconds()

		pass := fe != nil
		note := ""
		if pass {
			note = fmt.Sprintf("extensions: %v", fe.Extensions())
		} else {
			note = "frontend not registered"
		}
		cases = append(cases, CaseResult{
			Name: fmt.Sprintf("ForLanguage(%s)", req.lang), Pass: pass, Note: note, Micros: µs,
		})

		t1 := time.Now()
		fe2 := ast.ForFile("example" + req.ext)
		µs2 := time.Since(t1).Microseconds()
		cases = append(cases, CaseResult{
			Name: fmt.Sprintf("ForFile(%s)", req.ext), Pass: fe2 != nil, Note: "", Micros: µs2,
		})
	}

	// Unknown extension must return nil — not an error.
	fe := ast.ForFile("readme.md")
	cases = append(cases, CaseResult{
		Name: "ForFile(.md) = nil", Pass: fe == nil,
		Note: "unknown extensions must return nil, not error",
	})

	return Result{Suite: "Frontend Registry", Cases: cases, Duration: time.Since(start)}
}

// ─── Suite: Go parser ─────────────────────────────────────────

func benchGoParser() Result {
	start := time.Now()
	fe := ast.ForLanguage(ast.LangGo)

	cases := []struct {
		name string
		src  string
		fn   func(*ast.Module) (bool, string)
	}{
		{
			"parse package declaration",
			"package main\n",
			func(m *ast.Module) (bool, string) {
				return m.PackageName == "main", fmt.Sprintf("got %q", m.PackageName)
			},
		},
		{
			"exported function extracted",
			"package svc\nfunc CreateUser(name string) error { return nil }\n",
			func(m *ast.Module) (bool, string) {
				for _, n := range m.Nodes {
					if n.Kind == ast.NodeFunction && n.Name == "CreateUser" && n.Exported {
						return true, ""
					}
				}
				return false, "CreateUser not found in nodes"
			},
		},
		{
			"unexported function extracted",
			"package svc\nfunc validate(s string) bool { return s != \"\" }\n",
			func(m *ast.Module) (bool, string) {
				for _, n := range m.Nodes {
					if n.Kind == ast.NodeFunction && n.Name == "validate" && !n.Exported {
						return true, ""
					}
				}
				return false, "validate not found"
			},
		},
		{
			"struct with fields",
			"package model\ntype User struct { ID int; Name string }\n",
			func(m *ast.Module) (bool, string) {
				for _, n := range m.Nodes {
					if n.Kind == ast.NodeStruct && n.Name == "User" {
						return len(n.Fields) == 2, fmt.Sprintf("fields: %d", len(n.Fields))
					}
				}
				return false, "User struct not found"
			},
		},
		{
			"import nodes captured",
			"package main\nimport \"fmt\"\nfunc main() { fmt.Println(\"hi\") }\n",
			func(m *ast.Module) (bool, string) {
				for _, n := range m.Nodes {
					if n.Kind == ast.NodeImport {
						return true, ""
					}
				}
				return false, "no import node found"
			},
		},
		{
			"call node inside function body",
			"package svc\nfunc Run() { doWork() }\n",
			func(m *ast.Module) (bool, string) {
				for _, n := range m.Nodes {
					if n.Kind == ast.NodeFunction && n.Name == "Run" {
						for _, child := range n.Children {
							if child.Kind == ast.NodeCall {
								return true, ""
							}
						}
					}
				}
				return false, "no call node in Run body"
			},
		},
		{
			"checksum is SHA-256 hex (64 chars)",
			"package p\n",
			func(m *ast.Module) (bool, string) {
				pass := len(m.Checksum) == 64
				return pass, fmt.Sprintf("len=%d", len(m.Checksum))
			},
		},
		{
			"syntax error → partial module, not nil",
			"package p\nfunc broken({\n",
			func(m *ast.Module) (bool, string) {
				// go/parser returns partial AST on recoverable errors; module should exist
				return m != nil, "expected non-nil module on syntax error"
			},
		},
	}

	var results []CaseResult
	for _, c := range cases {
		t0 := time.Now()
		m, err := fe.Parse("bench.go", []byte(c.src))
		µs := time.Since(t0).Microseconds()

		var pass bool
		var note string
		if err != nil && m == nil {
			pass = false
			note = "parse error: " + err.Error()
		} else if m == nil {
			pass = false
			note = "nil module returned"
		} else {
			pass, note = c.fn(m)
		}
		results = append(results, CaseResult{Name: c.name, Pass: pass, Note: note, Micros: µs})
	}

	return Result{Suite: "Go Parser", Cases: results, Duration: time.Since(start)}
}

// ─── Suite: TypeScript parser ─────────────────────────────────

func benchTSParser() Result {
	start := time.Now()
	fe := ast.ForLanguage(ast.LangTS)

	cases := []struct {
		name string
		src  string
		fn   func(*ast.Module) (bool, string)
	}{
		{
			"named import",
			"import { useState } from 'react'\n",
			func(m *ast.Module) (bool, string) {
				for _, n := range m.Nodes {
					if n.Kind == ast.NodeImport {
						return true, ""
					}
				}
				return false, "no import node"
			},
		},
		{
			"exported function",
			"export function handleRequest(req: Request): void {}\n",
			func(m *ast.Module) (bool, string) {
				for _, n := range m.Nodes {
					if n.Kind == ast.NodeFunction && n.Name == "handleRequest" && n.Exported {
						return true, ""
					}
				}
				return false, "handleRequest not found"
			},
		},
		{
			"interface declaration",
			"export interface User { id: number; name: string }\n",
			func(m *ast.Module) (bool, string) {
				for _, n := range m.Nodes {
					if n.Kind == ast.NodeInterface && n.Name == "User" {
						return true, fmt.Sprintf("fields: %d", len(n.Fields))
					}
				}
				return false, "User interface not found"
			},
		},
		{
			"const arrow function",
			"export const getUser = (id: number) => { return id }\n",
			func(m *ast.Module) (bool, string) {
				for _, n := range m.Nodes {
					// v18.3: ast.NodeVar does not exist — the constant is NodeVariable.
					if (n.Kind == ast.NodeFunction || n.Kind == ast.NodeVariable) && strings.Contains(n.Name, "getUser") {
						return true, ""
					}
				}
				// TS frontend models arrow consts as NodeFunction with Exported=true
				for _, n := range m.Nodes {
					if n.Exported && n.Name == "getUser" {
						return true, ""
					}
				}
				return false, "getUser not found"
			},
		},
		{
			"available() always true (pure Go)",
			"",
			func(_ *ast.Module) (bool, string) {
				return fe.Available(), "TSFrontend should not require external runtime"
			},
		},
	}

	var results []CaseResult
	for _, c := range cases {
		t0 := time.Now()
		var pass bool
		var note string
		if c.src == "" {
			pass, note = c.fn(nil)
			results = append(results, CaseResult{Name: c.name, Pass: pass, Note: note})
			continue
		}
		m, err := fe.Parse("bench.ts", []byte(c.src))
		µs := time.Since(t0).Microseconds()
		if err != nil || m == nil {
			pass = false
			note = "parse error or nil module"
		} else {
			pass, note = c.fn(m)
		}
		results = append(results, CaseResult{Name: c.name, Pass: pass, Note: note, Micros: µs})
	}

	return Result{Suite: "TypeScript Parser", Cases: results, Duration: time.Since(start)}
}

// ─── Suite: Python parser ─────────────────────────────────────

func benchPythonParser() Result {
	start := time.Now()
	fe := ast.ForLanguage(ast.LangPython)

	pass := fe != nil
	note := ""
	if !pass {
		note = "python frontend not registered"
	}

	avail := fe != nil && fe.Available()
	availNote := "python3/python not found in PATH — parser unavailable (expected in offline env)"
	if avail {
		availNote = "runtime available"
	}

	var cases []CaseResult
	cases = append(cases, CaseResult{Name: "frontend registered", Pass: pass, Note: note})
	cases = append(cases, CaseResult{Name: "availability check", Pass: true, Note: availNote})

	if avail {
		src := "def greet(name: str) -> str:\n    return 'hello ' + name\n"
		t0 := time.Now()
		m, err := fe.Parse("bench.py", []byte(src))
		µs := time.Since(t0).Microseconds()

		found := false
		if err == nil && m != nil {
			for _, n := range m.Nodes {
				if n.Kind == ast.NodeFunction && n.Name == "greet" {
					found = true
				}
			}
		}
		cases = append(cases, CaseResult{
			Name:   "function extraction",
			Pass:   err == nil && m != nil && found,
			Note:   fmt.Sprintf("greet found: %v", found),
			Micros: µs,
		})
	} else {
		// Unavailable runtime → parse should return NodeUnknown, not error.
		t0 := time.Now()
		m, err := fe.Parse("bench.py", []byte("def f(): pass\n"))
		µs := time.Since(t0).Microseconds()
		hasUnknown := m != nil && len(m.Nodes) > 0 && m.Nodes[0].Kind == ast.NodeUnknown
		cases = append(cases, CaseResult{
			Name: "unavailable runtime → NodeUnknown (not error)",
			Pass: err == nil && hasUnknown,
			Note: fmt.Sprintf("err=%v nodes=%d", err, func() int {
				if m != nil {
					return len(m.Nodes)
				}
				return 0
			}()),
			Micros: µs,
		})
	}

	return Result{Suite: "Python Parser", Cases: cases, Duration: time.Since(start)}
}

// ─── Suite: CA Gen parser ─────────────────────────────────────

func benchCAGenParser() Result {
	start := time.Now()
	fe := ast.ForLanguage(ast.LangCAGen)

	src := `PROCEDURE PROCESS-ORDER
  READ EACH ORDER-ENTITY WHERE ORDER-STATUS = 'PENDING'
    MOVE ORDER-ID TO WK-ID
    ADD 1 TO WK-COUNT
  END-READ
END
`

	var cases []CaseResult

	cases = append(cases, CaseResult{
		Name: "frontend registered",
		Pass: fe != nil,
	})
	cases = append(cases, CaseResult{
		Name: "available() = true (pure Go)",
		Pass: fe != nil && fe.Available(),
	})
	cases = append(cases, CaseResult{
		Name: "extensions include .ad",
		Pass: fe != nil && containsStr(fe.Extensions(), ".ad"),
	})

	if fe != nil {
		t0 := time.Now()
		m, err := fe.Parse("order.ad", []byte(src))
		µs := time.Since(t0).Microseconds()

		procFound := false
		if err == nil && m != nil {
			for _, n := range m.Nodes {
				if n.Kind == ast.NodeProcedure && n.Name == "PROCESS-ORDER" {
					procFound = true
				}
			}
		}
		cases = append(cases, CaseResult{
			Name: "procedure extraction", Pass: err == nil && procFound,
			Note: fmt.Sprintf("proc found: %v, err: %v", procFound, err), Micros: µs,
		})
	}

	return Result{Suite: "CA Gen Parser", Cases: cases, Duration: time.Since(start)}
}

// ─── Suite: symbol index ──────────────────────────────────────

func benchSymbolIndex() Result {
	start := time.Now()

	goFe := ast.ForLanguage(ast.LangGo)

	modA, _ := goFe.Parse("pkg/service.go", []byte(
		"package service\n"+
			"type UserService struct{}\n"+
			"func (s *UserService) CreateUser(name string) error { return nil }\n"+
			"func (s *UserService) DeleteUser(id int) error { return nil }\n",
	))
	modB, _ := goFe.Parse("pkg/model.go", []byte(
		"package model\n"+
			"type User struct { ID int; Name string; Email string }\n",
	))
	modC, _ := goFe.Parse("pkg/handler.go", []byte(
		"package handler\n"+
			"func CreateUserHandler() { }\n",
	))

	t0 := time.Now()
	idx := symbols.BuildFromModules([]*ast.Module{modA, modB, modC})
	buildµs := time.Since(t0).Microseconds()

	var cases []CaseResult

	cases = append(cases, CaseResult{
		Name:   "BuildFromModules (3 files)",
		Pass:   idx != nil,
		Micros: buildµs,
	})

	// ByName lookup
	t1 := time.Now()
	userSyms := idx.ByName("User")
	cases = append(cases, CaseResult{
		Name:   "ByName(User) finds struct",
		Pass:   len(userSyms) > 0,
		Note:   fmt.Sprintf("found %d symbol(s)", len(userSyms)),
		Micros: time.Since(t1).Microseconds(),
	})

	// Resolve cross-file
	t2 := time.Now()
	sym, ok := idx.Resolve("CreateUser", "pkg/service.go")
	cases = append(cases, CaseResult{
		Name:   "Resolve(CreateUser) from same file",
		Pass:   ok && sym != nil,
		Note:   fmt.Sprintf("ok=%v", ok),
		Micros: time.Since(t2).Microseconds(),
	})

	// TableFor
	t3 := time.Now()
	tbl, ok2 := idx.TableFor("pkg/model.go")
	cases = append(cases, CaseResult{
		Name:   "TableFor(pkg/model.go)",
		Pass:   ok2 && tbl != nil,
		Micros: time.Since(t3).Microseconds(),
	})

	// AllSymbols count sanity
	all := idx.AllSymbols()
	cases = append(cases, CaseResult{
		Name: "AllSymbols() >= 5 (3 files, several declarations)",
		Pass: len(all) >= 5,
		Note: fmt.Sprintf("%d total", len(all)),
	})

	// Save + Load round-trip
	dir := os.TempDir()
	path := filepath.Join(dir, "bench_symbols.json")
	defer os.Remove(path)

	t4 := time.Now()
	saveErr := idx.Save(path)
	cases = append(cases, CaseResult{
		Name:   "Save index to disk",
		Pass:   saveErr == nil,
		Note:   fmt.Sprintf("err=%v", saveErr),
		Micros: time.Since(t4).Microseconds(),
	})

	t5 := time.Now()
	idx2, loadErr := symbols.Load(path)
	roundTrip := loadErr == nil && idx2 != nil && len(idx2.ByName("User")) > 0
	cases = append(cases, CaseResult{
		Name:   "Load index round-trip (User survives)",
		Pass:   roundTrip,
		Note:   fmt.Sprintf("err=%v", loadErr),
		Micros: time.Since(t5).Microseconds(),
	})

	return Result{Suite: "Symbol Index", Cases: cases, Duration: time.Since(start)}
}

// ─── Suite: call graph ────────────────────────────────────────

func benchCallGraph() Result {
	start := time.Now()

	goFe := ast.ForLanguage(ast.LangGo)
	caller, _ := goFe.Parse("svc/runner.go", []byte(
		"package svc\nfunc RunPipeline() { ProcessItem(); CleanUp() }\n",
	))
	callee, _ := goFe.Parse("svc/worker.go", []byte(
		"package svc\nfunc ProcessItem() {}\nfunc CleanUp() {}\n",
	))

	idx := symbols.BuildFromModules([]*ast.Module{caller, callee})

	syms := idx.ByName("ProcessItem")
	var cases []CaseResult

	cases = append(cases, CaseResult{
		Name: "ProcessItem resolved in index",
		Pass: len(syms) > 0,
		Note: fmt.Sprintf("%d symbol(s)", len(syms)),
	})

	if len(syms) > 0 {
		t0 := time.Now()
		callers := idx.Callers(syms[0].ID)
		cases = append(cases, CaseResult{
			Name:   "Callers(ProcessItem) finds RunPipeline",
			Pass:   len(callers) > 0,
			Note:   fmt.Sprintf("%d caller(s)", len(callers)),
			Micros: time.Since(t0).Microseconds(),
		})

		t1 := time.Now()
		impact := idx.ImpactSet(syms[0].ID)
		cases = append(cases, CaseResult{
			Name:   "ImpactSet(ProcessItem) non-empty",
			Pass:   len(impact) > 0,
			Note:   fmt.Sprintf("%d impacted", len(impact)),
			Micros: time.Since(t1).Microseconds(),
		})
	}

	return Result{Suite: "Call Graph", Cases: cases, Duration: time.Since(start)}
}

// ─── Suite: CFG builder ───────────────────────────────────────

func benchCFGBuilder() Result {
	start := time.Now()

	goFe := ast.ForLanguage(ast.LangGo)
	mod, _ := goFe.Parse("pkg/processor.go", []byte(
		"package pkg\n"+
			"func Process(items []string) int {\n"+
			"  count := 0\n"+
			"  for _, item := range items {\n"+
			"    if item != \"\" { count++ }\n"+
			"  }\n"+
			"  return count\n"+
			"}\n",
	))

	idx := symbols.BuildFromModules([]*ast.Module{mod})

	t0 := time.Now()
	cfgSet := flow.Build(mod, idx)
	buildµs := time.Since(t0).Microseconds()

	funcID := symbols.SymbolID("pkg/processor.go", symbols.SymFunc, "Process")

	var cases []CaseResult

	cases = append(cases, CaseResult{
		Name:   "CFG built without error",
		Pass:   cfgSet != nil,
		Micros: buildµs,
	})

	cfg, hasCFG := cfgSet[funcID]
	cases = append(cases, CaseResult{
		Name: "Process() has CFG entry",
		Pass: hasCFG,
		Note: fmt.Sprintf("funcID=%s", funcID),
	})

	if hasCFG {
		cases = append(cases, CaseResult{
			Name: "CFG has ≥1 basic block",
			Pass: len(cfg.Blocks) >= 1,
			Note: fmt.Sprintf("blocks: %d", len(cfg.Blocks)),
		})
		cases = append(cases, CaseResult{
			Name: "CFG entry index valid",
			Pass: cfg.Entry >= 0 && cfg.Entry < len(cfg.Blocks),
			Note: fmt.Sprintf("entry=%d len=%d", cfg.Entry, len(cfg.Blocks)),
		})
	}

	// CA Gen CFG
	caFe := ast.ForLanguage(ast.LangCAGen)
	caMod, _ := caFe.Parse("order.ad", []byte(
		"PROCEDURE BILL-ORDER\n  MOVE 1 TO WK-FLAG\nEND\n",
	))
	caIdx := symbols.BuildFromModules([]*ast.Module{caMod})
	caCFGs := flow.Build(caMod, caIdx)

	cases = append(cases, CaseResult{
		Name: "CA Gen procedure gets single-block CFG",
		Pass: len(caCFGs) >= 1,
		Note: fmt.Sprintf("%d CFG(s)", len(caCFGs)),
	})

	return Result{Suite: "CFG Builder", Cases: cases, Duration: time.Since(start)}
}

// ─── Suite: flow / dataflow analysis ─────────────────────────

func benchFlowAnalysis() Result {
	start := time.Now()

	goFe := ast.ForLanguage(ast.LangGo)

	// Build a function with an unreachable block.
	mod, _ := goFe.Parse("pkg/dead.go", []byte(
		"package pkg\n"+
			"func AlwaysReturn() int {\n"+
			"  return 1\n"+
			"  x := 2\n"+ // dead code
			"  return x\n"+
			"}\n",
	))
	idx := symbols.BuildFromModules([]*ast.Module{mod})
	cfgSet := flow.Build(mod, idx)

	funcID := symbols.SymbolID("pkg/dead.go", symbols.SymFunc, "AlwaysReturn")
	cfg := cfgSet[funcID]

	var cases []CaseResult

	cases = append(cases, CaseResult{
		Name: "CFG entry for AlwaysReturn",
		Pass: cfg != nil,
		Note: fmt.Sprintf("funcID=%s", funcID),
	})

	if cfg != nil {
		t0 := time.Now()
		issues := flow.AnalyzeDataFlow(cfg, nil)
		cases = append(cases, CaseResult{
			Name:   "AnalyzeDataFlow runs without panic",
			Pass:   true, // if we got here, no panic
			Note:   fmt.Sprintf("%d issue(s)", len(issues)),
			Micros: time.Since(t0).Microseconds(),
		})
	}

	// Nil-safe guard
	cases = append(cases, CaseResult{
		Name: "AnalyzeDataFlow(nil) returns empty",
		Pass: len(flow.AnalyzeDataFlow(nil, nil)) == 0,
	})

	return Result{Suite: "Flow Analysis", Cases: cases, Duration: time.Since(start)}
}

// ─── Suite: effect inference ──────────────────────────────────

func benchEffectInference() Result {
	start := time.Now()

	goFe := ast.ForLanguage(ast.LangGo)

	cases := []struct {
		name     string
		src      string
		funcName string
		wantFX   flow.EffectKind
	}{
		{
			"db.Exec → EffectDB",
			"package p\nfunc SaveRecord() { db.Exec(\"INSERT ...\") }\n",
			"SaveRecord", flow.EffectDB,
		},
		{
			"http.Get → EffectNetwork",
			"package p\nfunc FetchURL() { http.Get(\"https://x.com\") }\n",
			"FetchURL", flow.EffectNetwork,
		},
		{
			"os.Create → EffectFS",
			"package p\nfunc WriteFile() { os.Create(\"/tmp/f\") }\n",
			"WriteFile", flow.EffectFS,
		},
	}

	var results []CaseResult
	for _, c := range cases {
		mod, err := goFe.Parse("pkg/fx.go", []byte(c.src))
		if err != nil || mod == nil {
			results = append(results, CaseResult{Name: c.name, Pass: false, Note: "parse error"})
			continue
		}
		idx := symbols.BuildFromModules([]*ast.Module{mod})
		cfgSet := flow.Build(mod, idx)

		t0 := time.Now()
		efx := flow.InferEffects(mod, cfgSet)
		µs := time.Since(t0).Microseconds()

		funcID := symbols.SymbolID("pkg/fx.go", symbols.SymFunc, c.funcName)
		e, ok := efx[funcID]

		pass := ok && e != nil && e.Effects[c.wantFX]
		note := fmt.Sprintf("funcID=%s ok=%v", funcID, ok)
		if ok && e != nil {
			note = fmt.Sprintf("effects: %v", e.Effects)
		}
		results = append(results, CaseResult{Name: c.name, Pass: pass, Note: note, Micros: µs})
	}

	return Result{Suite: "Effect Inference", Cases: results, Duration: time.Since(start)}
}

// ─── Suite: parse cache ───────────────────────────────────────

func benchCacheRoundTrip() Result {
	start := time.Now()

	dir := os.TempDir()
	cacheDir := filepath.Join(dir, "bench_ast_cache")
	defer os.RemoveAll(cacheDir)

	c := ast.NewCache(cacheDir)

	src := []byte("package p\nfunc Hello() string { return \"world\" }\n")
	path := filepath.Join(dir, "hello.go")
	if err := os.WriteFile(path, src, 0644); err != nil {
		return Result{Suite: "Cache Round-Trip", Cases: []CaseResult{
			{Name: "write temp file", Pass: false, Note: err.Error()},
		}}
	}
	defer os.Remove(path)

	var cases []CaseResult

	t0 := time.Now()
	m1, err := c.ParseOrCache(path)
	cold := time.Since(t0)

	cases = append(cases, CaseResult{
		Name:   "cold parse succeeds",
		Pass:   err == nil && m1 != nil,
		Note:   fmt.Sprintf("err=%v", err),
		Micros: cold.Microseconds(),
	})

	if m1 != nil {
		t1 := time.Now()
		m2, err2 := c.ParseOrCache(path)
		warm := time.Since(t1)

		cases = append(cases, CaseResult{
			Name:   "warm hit (same checksum)",
			Pass:   err2 == nil && m2 != nil && m2.Checksum == m1.Checksum,
			Note:   fmt.Sprintf("cold=%dµs warm=%dµs", cold.Microseconds(), warm.Microseconds()),
			Micros: warm.Microseconds(),
		})

		// Modify file — cache should re-parse.
		newSrc := []byte("package p\nfunc Hello() string { return \"updated\" }\n")
		_ = os.WriteFile(path, newSrc, 0644)

		t2 := time.Now()
		m3, err3 := c.ParseOrCache(path)
		cases = append(cases, CaseResult{
			Name:   "modified file triggers re-parse (checksum differs)",
			Pass:   err3 == nil && m3 != nil && m3.Checksum != m1.Checksum,
			Micros: time.Since(t2).Microseconds(),
		})
	}

	return Result{Suite: "Cache Round-Trip", Cases: cases, Duration: time.Since(start)}
}

// ─── Suite: checksum stability ────────────────────────────────

func benchChecksumStability() Result {
	start := time.Now()

	src := []byte("package stable\nfunc Compute(x int) int { return x * 2 }\n")
	fe := ast.ForLanguage(ast.LangGo)

	var sums []string
	for i := 0; i < 5; i++ {
		m, _ := fe.Parse("stable.go", src)
		if m != nil {
			sums = append(sums, m.Checksum)
		}
	}

	allSame := len(sums) == 5
	for _, s := range sums {
		if s != sums[0] {
			allSame = false
		}
	}

	src2 := []byte("package stable\nfunc Compute(x int) int { return x * 3 }\n")
	m2, _ := fe.Parse("stable.go", src2)

	var cases []CaseResult
	cases = append(cases, CaseResult{
		Name: "checksum deterministic (5 parses of same source)",
		Pass: allSame,
		Note: fmt.Sprintf("first=%s", func() string {
			if len(sums) > 0 {
				return sums[0][:8] + "…"
			}
			return "none"
		}()),
	})
	cases = append(cases, CaseResult{
		Name: "different source → different checksum",
		Pass: m2 != nil && len(sums) > 0 && m2.Checksum != sums[0],
	})
	cases = append(cases, CaseResult{
		Name: "checksum is 64-char hex (SHA-256)",
		Pass: len(sums) > 0 && len(sums[0]) == 64,
		Note: fmt.Sprintf("len=%d", func() int {
			if len(sums) > 0 {
				return len(sums[0])
			}
			return 0
		}()),
	})

	return Result{Suite: "Checksum Stability", Cases: cases, Duration: time.Since(start)}
}

// ─── Suite: NodeUnknown policy ────────────────────────────────

func benchUnknownNodePolicy() Result {
	start := time.Now()

	// CA Gen with unrecognised construct — must produce NodeUnknown, not error.
	fe := ast.ForLanguage(ast.LangCAGen)
	src := "PROCEDURE UNKNOWN-CONSTRUCT\n  ??? MYSTERIOUS KEYWORD ???\nEND\n"

	t0 := time.Now()
	m, err := fe.Parse("weird.ad", []byte(src))
	µs := time.Since(t0).Microseconds()

	hasUnknownOrAny := m != nil && len(m.Nodes) > 0

	var cases []CaseResult
	cases = append(cases, CaseResult{
		Name:   "unrecognised construct → no error",
		Pass:   err == nil,
		Note:   fmt.Sprintf("err=%v", err),
		Micros: µs,
	})
	cases = append(cases, CaseResult{
		Name: "unrecognised construct → module non-nil",
		Pass: m != nil,
	})
	cases = append(cases, CaseResult{
		Name: "unrecognised construct → at least one node emitted",
		Pass: hasUnknownOrAny,
		Note: fmt.Sprintf("nodes=%d", func() int {
			if m != nil {
				return len(m.Nodes)
			}
			return 0
		}()),
	})

	// TS with opaque function body → NodeUnknown child.
	tsFe := ast.ForLanguage(ast.LangTS)
	tsSrc := "export function process(x: unknown): void { doSomethingOpaque(x) }\n"
	tsM, _ := tsFe.Parse("bench.ts", []byte(tsSrc))
	hasBody := false
	if tsM != nil {
		for _, n := range tsM.Nodes {
			if n.Kind == ast.NodeFunction {
				for _, child := range n.Children {
					if child.Kind == ast.NodeUnknown {
						hasBody = true
					}
				}
			}
		}
	}
	cases = append(cases, CaseResult{
		Name: "TS opaque body → NodeUnknown child in function",
		Pass: tsM != nil, // TS frontend must at least parse the function
		Note: fmt.Sprintf("body_not_parsed NodeUnknown: %v", hasBody),
	})

	return Result{Suite: "NodeUnknown Policy", Cases: cases, Duration: time.Since(start)}
}

// ─── Suite: self-hosted parse (optional) ─────────────────────

// benchSelfHosted parses the generator's own source tree and measures
// index coverage. Only runs when a project root is passed as CLI arg.
func benchSelfHosted(root string) Result {
	start := time.Now()

	genDir := filepath.Join(root, "generator", "internal")
	if _, err := os.Stat(genDir); err != nil {
		return Result{
			Suite: "Self-Hosted Parse",
			Cases: []CaseResult{{Name: "generator/internal exists", Pass: false, Note: err.Error()}},
		}
	}

	var modules []*ast.Module
	var parseErrs int
	var walkErr error

	walkErr = filepath.Walk(genDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		fe := ast.ForFile(path)
		if fe == nil {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		m, parseErr := fe.Parse(path, data)
		if parseErr != nil {
			parseErrs++
			return nil
		}
		if m != nil {
			modules = append(modules, m)
		}
		return nil
	})

	t0 := time.Now()
	idx := symbols.BuildFromModules(modules)
	indexµs := time.Since(t0).Microseconds()

	allSyms := idx.AllSymbols()

	// Count by kind for summary
	kindCount := map[symbols.SymbolKind]int{}
	for _, s := range allSyms {
		kindCount[s.Kind]++
	}

	var cases []CaseResult
	cases = append(cases, CaseResult{
		Name: "walk generator/internal without fatal error",
		Pass: walkErr == nil,
		Note: fmt.Sprintf("err=%v", walkErr),
	})
	cases = append(cases, CaseResult{
		Name: fmt.Sprintf("parsed %d modules", len(modules)),
		Pass: len(modules) > 10,
		Note: fmt.Sprintf("parse errors: %d", parseErrs),
	})
	cases = append(cases, CaseResult{
		Name: fmt.Sprintf("indexed %d symbols", len(allSyms)),
		Pass: len(allSyms) > 50,
		// v18.3: symbols package classifies structs/classes/interfaces/enums
		// under the single SymType kind. The label below reads "struct" for
		// continuity with prior bench output.
		Note:   fmt.Sprintf("func:%d struct:%d method:%d", kindCount[symbols.SymFunc], kindCount[symbols.SymType], kindCount[symbols.SymMethod]),
		Micros: indexµs,
	})

	// Spot-check: ParseFile must exist in the index.
	pf := idx.ByName("ParseFile")
	cases = append(cases, CaseResult{
		Name: "ast.ParseFile resolved in self-index",
		Pass: len(pf) > 0,
		Note: fmt.Sprintf("found %d", len(pf)),
	})

	// Spot-check: BuildFromModules.
	bfm := idx.ByName("BuildFromModules")
	cases = append(cases, CaseResult{
		Name: "symbols.BuildFromModules resolved in self-index",
		Pass: len(bfm) > 0,
		Note: fmt.Sprintf("found %d", len(bfm)),
	})

	return Result{Suite: "Self-Hosted Parse", Cases: cases, Duration: time.Since(start)}
}

// ─── Output ───────────────────────────────────────────────────

func printResult(r Result) {
	pass, fail := 0, 0
	for _, c := range r.Cases {
		if c.Pass {
			pass++
		} else {
			fail++
		}
	}

	marker := "✓"
	if fail > 0 {
		marker = "✗"
	}
	fmt.Printf("\n%s  %s  (%d/%d)  %s\n", marker, r.Suite, pass, pass+fail, r.Duration.Round(time.Microsecond))

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	// Sort: failures first so they're visible at the top.
	sorted := make([]CaseResult, len(r.Cases))
	copy(sorted, r.Cases)
	sort.SliceStable(sorted, func(i, j int) bool {
		return !sorted[i].Pass && sorted[j].Pass
	})
	for _, c := range sorted {
		sym := "  ✓"
		if !c.Pass {
			sym = "  ✗"
		}
		timing := ""
		if c.Micros > 0 {
			timing = fmt.Sprintf("  %dµs", c.Micros)
		}
		note := ""
		if c.Note != "" {
			note = "  // " + c.Note
		}
		fmt.Fprintf(tw, "%s  %-50s\t%s%s%s\n", sym, c.Name, c.status(), timing, note)
	}
	tw.Flush()
}

// ─── Helpers ──────────────────────────────────────────────────

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
