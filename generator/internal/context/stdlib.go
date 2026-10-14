// context/stdlib.go (v23.32, stdlib provenance): NEW file. Language-agnostic
// read-only ingest of standard-library source trees. Parses configured roots
// with the frontend their file extensions select, adds modules tagged
// ProvStdlib, and builds their CFGs/effects so a project call that lands in the
// stdlib resolves and can be walked in Understand. No per-language branching:
// Go's GOROOT/src and a Java signature tree go through the same path; only the
// extensions differ.
//
// Import-scoped by default: ingesting an entire language stdlib (GOROOT/src is
// ~5k files / ~150k symbols) is both slow and irrelevant. A project calls into
// a dozen packages, not all of them, and the flat O(n) resolver plus per-symbol
// CFGs do not scale to the whole tree. So when a root carries an ImportScope
// (the set of import paths the project actually uses), only those subpackages
// are ingested. An empty scope means "whole tree" and is reserved for explicit
// opt-in, not the default path.
package context

import (
	"os"
	"path/filepath"
	"strings"

	genast "github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/flow"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// stdlibProgress is the per-pass counter set returned for trace logging.
type stdlibProgress struct {
	filesSeen         int
	filesParsed       int
	filesParseFail    int
	filesSkipped      int
	pkgsIngested      int
	emptyScopeSkipped int // v24: AutoScope roots that resolved to an empty scope

	// v26.5: one entry per root, so the caller can build a StdlibReport that
	// names what attached and what was skipped (and why) instead of only an
	// aggregate count.
	roots []StdlibRootReport
}

// stdlibMaxFileSize caps the per-file size we will parse from a stdlib tree.
// v26.68: it was the 64 KiB bound of the project walk, which skipped files such
// as net/http/server.go (128 KiB), so a call to http.Error had no definition to
// resolve to. The walk only covers the packages the project imports, so a
// larger bound stays affordable. 512 KiB takes in server.go and also the
// generated net/http/h2_bundle.go (388 KiB).
const stdlibMaxFileSize = 512 * 1024

// loadStdlibRoots walks the configured stdlib roots (scoped to each root's
// ImportScope when set), parses each source file read-only, and adds it to the
// index tagged ProvStdlib. CFGs and effects are built so the Understand
// call-graph/CFG views work on stdlib nodes too. A root whose path does not
// exist is skipped (not an error) so callers can pass an optimistic default.
func (ctx *Context) loadStdlibRoots(roots []StdlibRoot, cobolFE *genast.CobolFrontend) stdlibProgress {
	var p stdlibProgress

	// v26.6: the currently-walked root's language breakdown. ingestFile is shared
	// across roots, so the loop points this at the active root's map before
	// walking. Nil between roots (synthetic roots don't walk files).
	var curLangs map[string]int

	// ingestFile parses one file and adds it. Shared by scoped and full walks.
	ingestFile := func(path string, size int64) {
		ext := strings.ToLower(filepath.Ext(path))
		if !isStdlibSourceFile(ext) {
			return
		}
		if isTestSourcePath(path) {
			p.filesSkipped++
			return
		}
		// v26.6: record the language family seen (before size/parse gating), so the
		// mixed-language-root warning reflects the tree's real contents even if some
		// files are later skipped for size.
		if curLangs != nil {
			if fam := langFamilyForExt(ext); fam != "" {
				curLangs[fam]++
			}
		}
		p.filesSeen++
		if size > stdlibMaxFileSize {
			p.filesSkipped++
			return
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			p.filesParseFail++
			return
		}
		var m *genast.Module
		var perr error
		if cobolFE != nil && isCobolExt(ext) {
			m, perr = cobolFE.Parse(path, data)
		} else {
			m, perr = genast.DefaultCache.ParseOrCache(path)
		}
		if perr != nil {
			p.filesParseFail++
			return
		}
		if m == nil {
			return
		}
		ctx.Index.AddModuleWithProvenance(m, symbols.ProvStdlib)
		p.filesParsed++
		cfgSet := flow.Build(m, ctx.Index)
		ctx.CFGs[path] = cfgSet
		for id, efx := range flow.InferEffects(m, cfgSet) {
			ctx.Effects[id] = efx
		}
	}

	// walkDir ingests every eligible file directly under dir AND its
	// subdirectories. Used for both a scoped package dir and a whole tree.
	walkDir := func(dir string) {
		_ = filepath.Walk(dir, func(path string, fi os.FileInfo, werr error) error {
			if werr != nil || fi.IsDir() {
				return nil
			}
			ingestFile(path, fi.Size())
			return nil
		})
	}

	for _, root := range roots {
		if root.Path == "" {
			continue
		}

		// v26.5: per-root accounting so the StdlibReport can name each root and
		// say exactly what it contributed. We snapshot the aggregate counters and
		// the index size before this root and diff after, which needs no change to
		// the ingest paths themselves.
		beforePkgs := p.pkgsIngested
		beforeSyms := len(ctx.Index.AllSymbols())
		rr := StdlibRootReport{
			Lang: string(root.Lang),
			Path: root.Path,
			Auto: root.autoDetected,
		}

		// v23.33: synthetic catalog roots. COBOL has no source stdlib to walk.
		// Its "runtime" is intrinsic functions (defined by the ISO standard,
		// no source) plus vendor/LE callable services (compiled binaries on the
		// mainframe). So instead of walking a directory we materialise a curated
		// catalog of those names as synthetic ProvStdlib symbols, which lets a
		// CALL "CEE3ABD" or FUNCTION NUMVAL resolve to a known runtime symbol
		// rather than reading as an unresolved (missing-program) call. Reuses
		// the exact provenance machinery; the only difference is the symbol
		// source is a built-in list, not files on disk.
		if root.Synthetic {
			curLangs = nil // synthetic roots walk no files
			n := ctx.loadSyntheticCatalog(root)
			p.filesParsed += n // count catalog "modules" as parsed units
			p.pkgsIngested++
			rr.Status = StdlibAttached
			rr.Packages = p.pkgsIngested - beforePkgs
			rr.Symbols = len(ctx.Index.AllSymbols()) - beforeSyms
			p.roots = append(p.roots, rr)
			continue
		}

		// v26.6: collect this root's language breakdown as it is walked.
		curLangs = map[string]int{}
		rr.LangBreakdown = curLangs

		// Resolve symlinks on the root. Debian/Ubuntu's Go packaging ships
		// GOROOT/src as a symlink, and filepath.Walk does not follow a
		// symlinked root. It lstat's it, sees a non-directory, visits it once
		// and stops, silently ingesting nothing. EvalSymlinks gives the real
		// directory. Symlinks *inside* the tree are still not followed, which
		// is what we want (no escaping the root).
		walkRoot := root.Path
		if resolved, rerr := filepath.EvalSymlinks(root.Path); rerr == nil && resolved != "" {
			walkRoot = resolved
		}
		info, err := os.Stat(walkRoot)
		if err != nil || !info.IsDir() {
			rr.Status = StdlibMissing
			rr.Detail = "path not found or not a directory"
			p.roots = append(p.roots, rr)
			continue
		}

		if len(root.ImportScope) == 0 {
			// v24: only ingest the whole tree when the caller explicitly opted in
			// (AutoScope=false and WholeTree). An AutoScope root that resolved to
			// an empty scope means "the project imports nothing from this root".
			// Ingesting all ~6000 JDK files in that case is the startup-hang bug,
			// so we ingest nothing and note it. Point --stdlib at the right module
			// dir (e.g. <jdk>/java.base) and ensure the project's imports were
			// collected if you expected resolution.
			if root.AutoScope {
				p.emptyScopeSkipped++
				rr.Status = StdlibSkipped
				rr.Detail = "project imports nothing from this root (import-scoped ingest → nothing to do)"
				p.roots = append(p.roots, rr)
				continue
			}
			walkDir(walkRoot) // explicit whole-tree ingest (heavy, opt-in)
			rr.Status = StdlibAttached
			rr.Detail = "whole-tree ingest (explicit opt-in)"
			rr.Packages = p.pkgsIngested - beforePkgs
			rr.Symbols = len(ctx.Index.AllSymbols()) - beforeSyms
			p.roots = append(p.roots, rr)
			continue
		}

		// Scoped ingest: each import path maps to a subdirectory of the root
		// (Go: "net/http" -> <root>/net/http). Only existing dirs are walked.
		// v24: a Java import is a fully-qualified class ("java.util.HashMap"),
		// whose directory is the package ("java/util"). The class name is a
		// file, not a dir. So for each import we try the mapped path as a dir and,
		// if that is not a directory, fall back to its parent (the package dir).
		// This is what makes `--stdlib <jdk>/java.base --stdlib-lang java` ingest
		// only java/util, java/lang, etc., not the whole 6000-file JDK.
		seen := map[string]bool{}
		tryWalk := func(dir string) {
			if dir == "" || seen[dir] {
				return
			}
			si, serr := os.Stat(dir)
			if serr != nil || !si.IsDir() {
				return
			}
			seen[dir] = true
			p.pkgsIngested++
			walkDir(dir)
		}
		for _, imp := range root.ImportScope {
			sub := importPathToSubdir(walkRoot, imp)
			if sub == "" {
				continue
			}
			if si, serr := os.Stat(sub); serr == nil && si.IsDir() {
				tryWalk(sub) // import already names a package dir (Go style)
			} else {
				tryWalk(filepath.Dir(sub)) // class import → walk its package dir
			}
		}
		rr.Packages = p.pkgsIngested - beforePkgs
		rr.Symbols = len(ctx.Index.AllSymbols()) - beforeSyms
		if rr.Packages == 0 {
			rr.Status = StdlibSkipped
			rr.Detail = "no import-scoped package under this root matched the project's imports"
		} else {
			rr.Status = StdlibAttached
		}
		p.roots = append(p.roots, rr)
	}

	return p
}

// resolveStdlibScope fills in ImportScope for any root that asked for
// AutoScope and didn't supply one, deriving it from the project's collected
// import paths (ctx.ImportGraph, populated during the project walk). Roots
// that already carry an explicit ImportScope, or that opt out of AutoScope,
// pass through unchanged.
func (ctx *Context) resolveStdlibScope(roots []StdlibRoot) []StdlibRoot {
	var projectImports []string
	collected := false
	collect := func() []string {
		if collected {
			return projectImports
		}
		collected = true
		seen := map[string]bool{}
		for _, imps := range ctx.ImportGraph {
			for _, imp := range imps {
				imp = strings.TrimSpace(imp)
				if imp == "" || seen[imp] {
					continue
				}
				seen[imp] = true
				projectImports = append(projectImports, imp)
			}
		}
		return projectImports
	}

	out := make([]StdlibRoot, 0, len(roots))
	for _, r := range roots {
		if len(r.ImportScope) == 0 && r.AutoScope {
			r.ImportScope = collect()
		}
		out = append(out, r)
	}
	return out
}

// For Go and Java the path separators in an import map onto filesystem
// separators ("net/http" -> root/net/http; "java/util" if signatures are laid
// out that way). Dots in Java-style imports are treated as separators too, so
// "java.util" and "java/util" both resolve. Defends against path escape.
func importPathToSubdir(root, imp string) string {
	imp = strings.TrimSpace(imp)
	if imp == "" {
		return ""
	}
	imp = strings.ReplaceAll(imp, ".", "/")
	clean := filepath.Clean("/" + imp) // force-absolute then clean to kill ".."
	if clean == "/" {
		return ""
	}
	return filepath.Join(root, clean[1:])
}

// isStdlibSourceFile decides whether an extension is ingested from a stdlib
// tree. Kept separate from isSourceFile so stdlib policy can diverge later.
func isStdlibSourceFile(ext string) bool {
	switch ext {
	case ".go", ".ts", ".tsx", ".py", ".java",
		".cob", ".cbl", ".cpy", ".cobol":
		return true
	}
	return false
}

// isTestSourcePath reports whether path looks like a test file we skip when
// ingesting a stdlib tree (Go *_test.go dominates GOROOT/src).
func isTestSourcePath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return true
	case strings.HasSuffix(base, ".test.ts"), strings.HasSuffix(base, ".test.tsx"):
		return true
	case strings.HasSuffix(base, "_test.py"), strings.HasPrefix(base, "test_"):
		return true
	}
	return false
}
