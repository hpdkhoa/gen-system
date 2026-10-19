// context.go (v25.6.1, freeze fix): shouldSkip now excludes generated sibling
// layer dirs (frontend/, mobile/, db/migrations/, design-manifest/). So loading a
// generated project root no longer walks into the dense generated web/mobile
// TS/JSX trees and parses them synchronously inside /api/project/load, which
// froze the Studio. Backend Go + legacy source at the root still parse.
// v24b-fix: collectModuleImports uses the genast alias (this file binds `ast` to go/ast).
// context.go (v23.32, stdlib provenance): LoadContextOpts gains StdlibRoots;
// after the project walk and before ResolveAll, configured stdlib trees are
// parsed read-only and added with ProvStdlib so project→stdlib call edges
// resolve. Language-agnostic: selection is by file extension, like project
// source. Prior: v23.12 (Phase 4): isSourceFile recognises .java so LoadContext discovers Java sources.
// v19.8 (resolve-coldload-edges): call ctx.Index.ResolveAll() after the
// parse walk completes. Without this every CallEdge produced by the cold
// load had an empty CalleeID, and `genbench resolution` reported 0% recall
// on a codebase the engine was otherwise parsing cleanly. UpdateFile
// (watcher path) already resolved per-call, so the bug only manifested
// on first load; the new method is also exposed publicly as ResolveAll
// for any future incremental-batch builder.
// v21.1: shouldSkip now excludes all of .gen (was only .gen/snapshots + .gen/entities) so the engine never parses its own ast_cache/staging as source. This fixes phantom beliefs from an empty root.
// Patched in Phase A: added structured trace logging in LoadContext.
// Phase markers (load → parse → index → cfg → effects) emit at INFO.
// Per-file parse failures and skipped files emit at TRACE/ERROR.
// No public signatures changed; trace import added.
// Patched in TASK 4 (v19): added LoadContextWithSkill for skill-file-seeded
// context loading. LoadContext itself is unchanged and remains the default.
// Patched in v19.5 (COBOL wiring): added LoadContextOpts and
// LoadContextWithOpts. LoadContext(root) now delegates to it with empty
// opts so all existing callers stay green. COBOL files (.cob/.cbl/.cpy/
// .cobol) are recognised by isSourceFile and parsed with a copybook-aware
// CobolFrontend when opts.CopybookPath is non-empty or the
// GEN_COBOL_COPYBOOK_PATH environment variable is set.
package context

// ─────────────────────────────────────────────────────────────
// context.go: runtime project context (v14 base + v15 extensions)
//
// v15 additions (marked with "v15"):
//   • Context.Index  *symbols.Index        : cross-file symbol database
//   • Context.CFGs   map[string]flow.CFGSet : per-file control-flow graphs
//   • Context.Effects map[string]*flow.FuncEffects : inferred side effects
//   • LoadContext now parses all four supported languages (Go, TS, Python, CA Gen)
//     via ast.DefaultCache.ParseOrCache and builds the symbol index.
//   • buildEntityUsage now queries Index.ByName instead of strings.Contains.
//   • New query methods: FindReferences, ImpactSet.
//
// Phase A additions:
//   • Structured tracing throughout LoadContext for diagnosability.
//
// All v14 fields, methods, and helper signatures are preserved unchanged.
// ─────────────────────────────────────────────────────────────

import (
	stdctx "context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	genast "github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/objgraph"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
	"github.com/hpdkhoa/gen-system/generator/internal/trace"
)

// Context captures the full runtime picture of the project.
// v3: deep cross-file awareness: import graph, entity usage,
//
//	dependency map, and architecture summary for AI prompts.
//
// v15: adds symbol index, CFG sets, and inferred effect maps.
type Context struct {
	Root      string
	Files     map[string]string // relative path → file contents
	IR        compiler.SchemaIR // last compiled IR snapshot
	OutputDir string

	// ── v3 fields (unchanged) ──────────────────────────────
	ImportGraph map[string][]string // file → imports it declares
	EntityUsage map[string][]string // entity name → files that reference it
	PackageMap  map[string]string   // package name → directory path
	ArchSummary string              // compact AI-ready architecture description

	// ── v15 additions ──────────────────────────────────────
	// Index is the authoritative cross-file symbol database.
	// All structural queries should go through Index rather than
	// scanning the Files map with strings.Contains.
	Index *symbols.Index

	// CFGs holds control-flow graphs keyed by file path.
	CFGs map[string]flow.CFGSet

	// Effects maps symbolID → inferred side effects.
	Effects map[string]*flow.FuncEffects

	// ── v26.5 additions ────────────────────────────────────
	// StdlibReport records what the stdlib pass attached, what it skipped, and
	// why. Populated by LoadContextWithOpts (empty when no stdlib was asked
	// for). The CLIs print it and the Understand Architecture page renders it,
	// so the edge-resolution % is self-diagnosing: an unresolved-heavy project
	// can see "java skipped: JAVA_HOME not set" instead of guessing.
	StdlibReport StdlibReport
}

// ─────────────────────────────────────────────────────────────
// LoadContext
// ─────────────────────────────────────────────────────────────

// LoadContextOpts configures the project context loader.
// Zero value preserves the original LoadContext behaviour.
//
// CopybookPath is the ordered list of directories searched when resolving
// COBOL COPY <name>. directives. Recognised copybook extensions default
// to {.cpy, .cbl, .cob}; override with CopybookExtensions when needed.
//
// If CopybookPath is empty, the GEN_COBOL_COPYBOOK_PATH environment
// variable is consulted: it is split on the OS path-list separator
// (':' on Unix, ';' on Windows). This lets the gen and gen-server
// binaries pick up copybook locations without changing their flags.
//
// Added in v19.5.
type LoadContextOpts struct {
	CopybookPath       []string
	CopybookExtensions []string

	// ── v23.32 (stdlib provenance) ────────────────────────────
	// StdlibRoots are additional directories parsed read-only and tagged
	// ProvStdlib. They are not walked as project source: their symbols are
	// resolvable call targets (so a project call into the stdlib links), but
	// they never count toward "internal" recall and the Understand UI hides
	// them by default. Each root is parsed with the frontend its file
	// extensions select, exactly like project source. Language-agnostic:
	// the loader does not care whether a root is Go's GOROOT/src or a Java
	// signature tree. Only the file extensions matter.
	StdlibRoots []StdlibRoot

	// ── v26.5 (multi-language stdlib) ─────────────────────────
	// StdlibAuto asks the loader to detect the stdlib roots from the project
	// itself (see DetectStdlibRoots): every language actually present in the
	// index gets its runtime attached, if one can be located. Detected roots
	// are appended to any explicit StdlibRoots. Explicit always wins, auto
	// only fills gaps. Safe without a confirmation prompt because every
	// detected root is AutoScope: the loader ingests only the packages the
	// project genuinely imports, so a root the project turns out not to use
	// costs nothing. The outcome (what attached, what was skipped and why) is
	// recorded in Context.StdlibReport for the caller to print.
	StdlibAuto bool
}

// StdlibRoot is one standard-library source tree to ingest read-only.
type StdlibRoot struct {
	Path string          // directory to walk
	Lang genast.Language // advisory label for reporting; parsing goes by extension

	// ImportScope restricts ingest to the subpackages the project actually
	// imports (each entry maps to a subdirectory of Path). Empty means
	// whole-tree ingest, which is heavy and explicit opt-in only. The loader populates
	// this from the project's import graph when the caller leaves it nil and
	// AutoScope is true.
	ImportScope []string

	// AutoScope asks the loader to derive ImportScope from the project's
	// collected imports (ctx.ImportGraph) when ImportScope is empty. This is
	// the default for the CLI/genbench Go path: ingest only what's used.
	AutoScope bool

	// Synthetic marks a root whose symbols come from a built-in catalog rather
	// than files on disk. Used for COBOL, whose "stdlib" is intrinsic functions
	// + vendor runtime services that have no portable source tree. When true,
	// Path is a catalog id ("cobol") and the loader materialises the catalog
	// instead of walking a directory.
	Synthetic bool

	// autoDetected marks a root that came from DetectStdlibRoots rather than an
	// explicit --stdlib entry. Unexported: it is only a reporting flag, never a
	// behaviour switch. An auto root and an explicit root with the same
	// Path/Lang are ingested identically.
	autoDetected bool
}

// LoadContext reads all relevant source files and builds deep
// cross-file awareness: import graph, entity usage, dependency map.
// v15: also builds the symbol index, CFG sets, and effect maps.
// Phase A: structured trace logging at INFO for each phase boundary.
// v19.5: delegates to LoadContextWithOpts with empty opts; signature
// preserved so every existing caller works unchanged.
func LoadContext(root string) Context {
	return LoadContextWithOpts(root, LoadContextOpts{})
}

// maxSourceFileSize is the largest source file the project walk parses. v26.71:
// raised from 64 KB, which dropped real modules without a word (a 245 KB Python
// file, a 109 KB Blender script); a file past it is now counted in the scan log
// as too_large and logged at info level.
const maxSourceFileSize = 512 * 1024

// LoadContextWithOpts is the explicit variant accepting LoadContextOpts.
// It performs the same full-tree parse as LoadContext and additionally
// honours COBOL copybook resolution when opts.CopybookPath (or the
// GEN_COBOL_COPYBOOK_PATH env var) is set.
//
// Added in v19.5.
func LoadContextWithOpts(root string, opts LoadContextOpts) Context {
	tctx := trace.WithPhase(stdctx.Background(), "load")
	timer := trace.Start(tctx, "LoadContext")

	ctx := Context{
		Root:        root,
		Files:       map[string]string{},
		ImportGraph: map[string][]string{},
		EntityUsage: map[string][]string{},
		PackageMap:  map[string]string{},
		OutputDir:   filepath.Join(root, "output"),
		Index:       symbols.NewIndex(),
		CFGs:        map[string]flow.CFGSet{},
		Effects:     map[string]*flow.FuncEffects{},
	}

	// Resolve copybook configuration (opts > env > none).
	cobolFE := cobolFrontendFor(root, opts)

	// Load IR snapshot
	ctx.IR, _ = compiler.LoadPreviousIR()
	trace.Info(tctx, "IR snapshot loaded", "entities", len(ctx.IR.Entities))

	// Counters for phase summary.
	var (
		filesSeen       int
		filesParsed     int
		filesParseFail  int
		filesNoFrontend int
		filesTooLarge   int
	)
	parseCtx := trace.WithPhase(tctx, "parse")

	// Walk and collect source files
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		if shouldSkip(rel) {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if !isSourceFile(ext) {
			return nil
		}
		filesSeen++

		if info.Size() > maxSourceFileSize {
			filesTooLarge++
			trace.Info(parseCtx, "skip oversized", "file", rel, "size", info.Size(), "max", maxSourceFileSize)
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			trace.Error(parseCtx, "read failed", "file", rel, "err", err)
			return nil
		}
		content := string(data)
		ctx.Files[rel] = content

		// ── v14: parse Go imports (kept for backward compat) ──
		if ext == ".go" {
			ctx.analyzeGoFile(path, rel, content)
		}

		// ── v15: full structural parse via unified frontend ──
		// v19.5: COBOL files take the configured copybook-aware path so
		// COPY directives resolve against opts.CopybookPath.
		var m *genast.Module
		var parseErr error
		if cobolFE != nil && isCobolExt(ext) {
			m, parseErr = cobolFE.Parse(path, data)
		} else {
			m, parseErr = genast.DefaultCache.ParseOrCache(path)
		}
		if parseErr != nil {
			filesParseFail++
			trace.Error(parseCtx, "parse failed", "file", rel, "lang", genast.DetectLanguage(path), "err", parseErr)
			return nil
		}
		if m == nil {
			// No frontend for this extension (e.g. .sql, .json). That is fine.
			filesNoFrontend++
			return nil
		}

		ctx.Index.AddModule(m)
		filesParsed++
		// v24: populate ImportGraph from the unified Module's NodeImport children
		// so every language contributes its imports. The Go-specific go/ast path
		// (buildImportGraph) only covered Go. Without this, Java/TS/Python imports
		// never reached ImportGraph, so stdlib AutoScope collected nothing and
		// fell back to walking the entire stdlib tree (the "gen-server never
		// starts, parses all ~6000 JDK files" hang). Import paths feed
		// resolveStdlibScope → only the referenced packages are ingested.
		if imps := collectModuleImports(m); len(imps) > 0 {
			rel := relForImportGraph(ctx.Root, m.FilePath)
			ctx.ImportGraph[rel] = imps
		}
		trace.Trace(parseCtx, "parsed", "file", rel, "lang", m.Language, "nodes", len(m.Nodes), "unknowns", countUnknown(m))

		cfgSet := flow.Build(m, ctx.Index)
		ctx.CFGs[path] = cfgSet

		for id, efx := range flow.InferEffects(m, cfgSet) {
			ctx.Effects[id] = efx
		}

		return nil
	})

	trace.Info(tctx, "scan complete",
		"files_seen", filesSeen,
		"parsed", filesParsed,
		"parse_failed", filesParseFail,
		"no_frontend", filesNoFrontend,
		"too_large", filesTooLarge,
	)

	// ── v23.32: read-only stdlib ingest ───────────────────────
	// Parse each configured stdlib root and add its modules tagged
	// ProvStdlib. Done after the project walk (so project symbols win on
	// any ID collision via removeFileLocked semantics, though stdlib paths
	// never collide with project paths in practice) and before ResolveAll
	// (so the global sweep links project→stdlib call edges).
	//
	// v26.5: the root set may now be detected from the project rather than
	// spelled out. Detection runs here, after the project walk, precisely
	// because it keys off the languages present in the index, which only
	// exist once the walk is done. Explicit roots always win: a detected root
	// is dropped when an explicit root already covers the same language, so
	// `--stdlib java=/my/jdk` is never second-guessed by the detector.
	stdlibRoots := opts.StdlibRoots
	report := StdlibReport{Mode: "explicit"}
	if opts.StdlibAuto {
		detected, detRep := DetectStdlibRoots(ctx.Index)
		report = detRep
		if len(opts.StdlibRoots) > 0 {
			report.Mode = "auto+explicit"
		}
		explicitLang := map[genast.Language]bool{}
		for _, r := range opts.StdlibRoots {
			explicitLang[r.Lang] = true
		}
		for _, d := range detected {
			if explicitLang[d.Lang] {
				continue // an explicit root already covers this language
			}
			stdlibRoots = append(stdlibRoots, d)
		}
	} else if len(opts.StdlibRoots) == 0 {
		report.Mode = "none"
	}

	if len(stdlibRoots) > 0 {
		stdlibCtx := trace.WithPhase(tctx, "stdlib")
		roots := ctx.resolveStdlibScope(stdlibRoots)
		sp := ctx.loadStdlibRoots(roots, cobolFE)
		// Per-root outcomes from the loader, appended to any "could not even
		// locate a tree" entries the detector already recorded.
		report.Roots = append(report.Roots, sp.roots...)
		// v26.6: warn about a root that physically contains more than one
		// language. One root maps the project's imports with one convention
		// against one directory, so a mixed tree resolves only the language that
		// matches. The rest silently miss. Give the exact split-into-per-root
		// fix. Detected roots are single-language by construction, so this only
		// ever fires on an explicit --stdlib path.
		report.Warnings = append(report.Warnings, mixedLanguageWarnings(sp.roots)...)
		trace.Info(stdlibCtx, "stdlib ingest complete",
			"roots", len(roots),
			"pkgs", sp.pkgsIngested,
			"files_parsed", sp.filesParsed,
			"parse_failed", sp.filesParseFail,
		)
	}
	ctx.StdlibReport = report

	// v19.8: AddModule appends CallEdges with empty CalleeID. Call
	// targets defined in files parsed later can only be resolved once
	// the full symbol set is in. Do the global sweep now, after every
	// file has contributed its symbols.
	ctx.Index.ResolveAll()

	// ── v14: build entity usage (now Index-backed, backward compat) ──
	indexCtx := trace.WithPhase(tctx, "index")
	ctx.buildEntityUsage()
	trace.Info(indexCtx, "entity usage built", "entities", len(ctx.EntityUsage), "symbols", len(ctx.Index.AllSymbols()))

	// ── Build architecture summary ──
	ctx.ArchSummary = ctx.buildArchSummary()

	timer.End("files_parsed", filesParsed)
	return ctx
}

// ─────────────────────────────────────────────────────────────
// LoadContextWithSkill: skill-file-seeded context (TASK 4 v19)
// ─────────────────────────────────────────────────────────────

// LoadContextWithSkill loads a Context seeded from a .skill file rather
// than a full source walk.
//
// When skillPath is empty or the file does not exist, it falls back to
// LoadContext(root) so callers can always use this function without
// conditional logic.
//
// When skillPath points to a valid .skill file the returned Context has:
//   - Index populated from the skill file's symbol nodes (no raw AST).
//   - Effects populated from the skill file's effect lists.
//   - CFGs is empty (no source walk means no CFG construction).
//   - Files is empty (source text is not loaded).
//   - ArchSummary is built from the Index as usual.
//
// Round trip: a .skill file written from project state P
// and loaded here will reproduce the same Index symbols as parsing P
// from source (same IDs, names, layers, effects). CFG topology is not
// round-tripped; that requires a source parse.
func LoadContextWithSkill(root, skillPath string) Context {
	if skillPath == "" {
		return LoadContext(root)
	}

	sf, err := objgraph.ReadSkillFile(skillPath)
	if err != nil {
		// File missing or malformed: fall back to full parse.
		return LoadContext(root)
	}

	ctx := Context{
		Root:        root,
		Files:       map[string]string{},
		ImportGraph: map[string][]string{},
		EntityUsage: map[string][]string{},
		PackageMap:  map[string]string{},
		OutputDir:   filepath.Join(root, "output"),
		Index:       objgraph.IndexFromSkillFile(sf),
		CFGs:        map[string]flow.CFGSet{},
		Effects:     objgraph.EffectsFromSkillFile(sf),
	}

	// Load the IR snapshot for migration tooling (not from skill file).
	ctx.IR, _ = compiler.LoadPreviousIR()

	// Build entity usage and arch summary from the seeded index.
	ctx.buildEntityUsage()
	ctx.ArchSummary = ctx.buildArchSummary()

	return ctx
}

// countUnknown returns how many NodeUnknown entries are in a Module.
// Used for trace diagnostics only.
func countUnknown(m *genast.Module) int {
	if m == nil {
		return 0
	}
	n := 0
	var walk func(node *genast.Node)
	walk = func(node *genast.Node) {
		if node == nil {
			return
		}
		if node.Kind == genast.NodeUnknown {
			n++
		}
		for _, c := range node.Children {
			walk(c)
		}
	}
	for _, root := range m.Nodes {
		walk(root)
	}
	return n
}

// ─────────────────────────────────────────────────────────────
// v14 analysis methods (unchanged)
// ─────────────────────────────────────────────────────────────

// analyzeGoFile extracts import declarations and package name.
func (c *Context) analyzeGoFile(absPath, relPath, content string) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, absPath, content, parser.ImportsOnly)
	if err != nil {
		return
	}

	// Silence unused variable warning
	_ = ast.File{}

	dir := filepath.Dir(relPath)
	if node.Name != nil {
		c.PackageMap[node.Name.Name] = dir
	}

	var imports []string
	for _, imp := range node.Imports {
		if imp.Path != nil {
			p := strings.Trim(imp.Path.Value, `"`)
			imports = append(imports, p)
		}
	}
	if len(imports) > 0 {
		c.ImportGraph[relPath] = imports
	}
}

// buildEntityUsage populates EntityUsage.
// v15: when the symbol index is populated, uses exact lookups;
// falls back to strings.Contains for any entity not found via index.
func (c *Context) buildEntityUsage() {
	for _, e := range c.IR.Entities {
		name := e.Name

		// v15: prefer index lookup (zero false positives)
		if c.Index != nil {
			syms := c.Index.ByName(name)
			seen := map[string]bool{}
			for _, s := range syms {
				rel, _ := filepath.Rel(c.Root, s.DefinedIn)
				if !seen[rel] {
					seen[rel] = true
					c.EntityUsage[name] = appendUnique(c.EntityUsage[name], rel)
				}
			}
			if len(c.EntityUsage[name]) > 0 {
				continue
			}
		}

		// v14 fallback: string scan
		for path, content := range c.Files {
			if strings.Contains(content, name) {
				c.EntityUsage[name] = appendUnique(c.EntityUsage[name], path)
			}
		}
	}
}

// buildArchSummary produces a compact text summary for AI repair prompts.
func (c *Context) buildArchSummary() string {
	var b strings.Builder
	b.WriteString("# Project Architecture Summary\n\n")

	b.WriteString("## Packages:\n")
	for pkg, dir := range c.PackageMap {
		b.WriteString(fmt.Sprintf("  %s → %s\n", pkg, dir))
	}

	b.WriteString("\n## IR Entities:\n")
	for _, e := range c.IR.Entities {
		fields := make([]string, 0, len(e.Fields))
		for _, f := range e.Fields {
			fields = append(fields, f.Name+":"+f.Type)
		}
		b.WriteString(fmt.Sprintf("  %s { %s }\n", e.Name, strings.Join(fields, ", ")))
	}

	b.WriteString("\n## Entity cross-references:\n")
	for entity, files := range c.EntityUsage {
		b.WriteString(fmt.Sprintf("  %s ← referenced in %d file(s)\n", entity, len(files)))
	}

	// v15: add symbol counts when index is available
	if c.Index != nil {
		counts := map[symbols.SymbolKind]int{}
		for _, s := range c.Index.AllSymbols() {
			counts[s.Kind]++
		}
		b.WriteString("\n## Symbol index (v15):\n")
		for kind, n := range counts {
			b.WriteString(fmt.Sprintf("  %s: %d\n", kind, n))
		}
	}

	b.WriteString(fmt.Sprintf("\n## Source files loaded: %d\n", len(c.Files)))
	return b.String()
}

// ─────────────────────────────────────────────────────────────
// v14 accessor methods (unchanged)
// ─────────────────────────────────────────────────────────────

// FileContent returns a file's content by relative path.
func (c *Context) FileContent(rel string) string {
	return c.Files[rel]
}

// FilesReferencingEntity returns all files that mention an entity name.
// Used by the healer to build a targeted cross-file repair scope.
func (c *Context) FilesReferencingEntity(entityName string) []string {
	return c.EntityUsage[entityName]
}

// Summary returns the full architecture summary string for AI prompts.
func (c *Context) Summary() string {
	return c.ArchSummary
}

// ─────────────────────────────────────────────────────────────
// v15 query methods
// ─────────────────────────────────────────────────────────────

// FindReferences returns every file that contains a reference to the
// named symbol, using exact symbol index lookups (zero false positives).
func (c *Context) FindReferences(name string) []string {
	if c.Index == nil {
		return nil
	}
	seen := map[string]bool{}
	var files []string
	for _, s := range c.Index.ByName(name) {
		for _, ref := range c.Index.References(s.ID) {
			if !seen[ref.In] {
				seen[ref.In] = true
				rel, _ := filepath.Rel(c.Root, ref.In)
				files = append(files, rel)
			}
		}
		for _, caller := range c.Index.Callers(s.ID) {
			rel, _ := filepath.Rel(c.Root, caller.DefinedIn)
			if !seen[rel] {
				seen[rel] = true
				files = append(files, rel)
			}
		}
	}
	return files
}

// ImpactSet returns the transitive set of files that could break if
// the named symbol changes, i.e. all files containing its callers.
func (c *Context) ImpactSet(symbolID string) []string {
	if c.Index == nil {
		return nil
	}
	seen := map[string]bool{}
	var files []string
	for _, s := range c.Index.ImpactSet(symbolID) {
		rel, _ := filepath.Rel(c.Root, s.DefinedIn)
		if !seen[rel] {
			seen[rel] = true
			files = append(files, rel)
		}
	}
	return files
}

// ─────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────

func shouldSkip(rel string) bool {
	// Always-skip: engine state, VCS, dependency + build output trees.
	blocked := []string{
		".git", ".gen", "node_modules", "vendor",
		"output",
		// v25.6.1: a generated app writes these sibling layer dirs next to the
		// backend. Loading a *generated* project root previously walked into the
		// dense generated frontend/mobile TS/JSX trees and parsed them
		// synchronously inside the /api/project/load handler (with no timeout),
		// which froze the Studio. These are generated artifacts, not legacy
		// source the Understand track exists to analyse, so skip them. Backend
		// Go + any legacy languages at the root are still parsed.
		"frontend", "mobile",
		"design-manifest", // v25.6 manifest folder (HTML/JSON, not source)
	}
	for _, b := range blocked {
		if rel == b || strings.HasPrefix(rel, b+string(filepath.Separator)) || strings.HasPrefix(rel, b+"/") {
			return true
		}
	}
	// db/migrations is generated SQL churn (one file per migration); skip the
	// migrations subtree but keep any hand-written db/*.sql at the db root.
	if rel == filepath.Join("db", "migrations") ||
		strings.HasPrefix(rel, "db"+string(filepath.Separator)+"migrations"+string(filepath.Separator)) ||
		strings.HasPrefix(rel, "db/migrations/") {
		return true
	}
	return false
}

// cobolFrontendFor returns a *CobolFrontend configured with copybook
// resolution settings, or nil when no copybook configuration is found.
// Resolution order: opts.CopybookPath → GEN_COBOL_COPYBOOK_PATH env var.
// Relative directories in either source are resolved against root.
//
// Added in v19.5.
func cobolFrontendFor(root string, opts LoadContextOpts) *genast.CobolFrontend {
	paths := opts.CopybookPath
	if len(paths) == 0 {
		if env := os.Getenv("GEN_COBOL_COPYBOOK_PATH"); env != "" {
			paths = filepath.SplitList(env)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	resolved := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		resolved = append(resolved, p)
	}
	if len(resolved) == 0 {
		return nil
	}
	return &genast.CobolFrontend{
		CopybookPath:       resolved,
		CopybookExtensions: opts.CopybookExtensions,
	}
}

// isCobolExt returns true when ext is one of the recognised COBOL file
// extensions. ext is expected to include the leading dot and be lower-cased
// (as produced by strings.ToLower(filepath.Ext(...))).
func isCobolExt(ext string) bool {
	switch ext {
	case ".cob", ".cbl", ".cpy", ".cobol":
		return true
	}
	return false
}

func isSourceFile(ext string) bool {
	switch ext {
	case ".go", ".jsx", ".js", ".ts", ".tsx", ".sql", ".json", ".tmpl",
		".py", ".pyw", // Python (v15)
		".ad", ".cagen", ".gel", // CA Gen (v15)
		".cob", ".cbl", ".cpy", ".cobol", // COBOL (v19.5)
		".java": // Java 17 (v23.12 Phase 4)
		return true
	}
	return false
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// collectModuleImports returns the import paths declared by a unified Module,
// reading NodeImport children (ImportPath). v24: language-agnostic. Java/TS/
// Python frontends emit imports as NodeImport nodes, which the Go-specific
// go/ast import collector never saw. Feeds ImportGraph so stdlib AutoScope only
// ingests referenced packages (fixes the whole-JDK-tree ingest hang).
func collectModuleImports(m *genast.Module) []string {
	if m == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	var walk func(ns []*genast.Node)
	walk = func(ns []*genast.Node) {
		for _, n := range ns {
			if n == nil {
				continue
			}
			if n.Kind == genast.NodeImport {
				p := strings.TrimSpace(n.ImportPath)
				if p != "" && !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
			if len(n.Children) > 0 {
				walk(n.Children)
			}
		}
	}
	walk(m.Nodes)
	return out
}

// relForImportGraph returns a stable key for a file in ImportGraph. The scope
// resolver reads only the values (import lists), so the key form is not
// load-bearing; use the path relative to root when possible, else the abs path.
func relForImportGraph(root, file string) string {
	if rel, err := filepath.Rel(root, file); err == nil {
		return rel
	}
	return file
}
