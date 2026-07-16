// context/stdlib_detect.go — v26.5 (NEW): multi-language stdlib resolution.
//
// WHY THIS EXISTS
// Before v26.5 the stdlib surface was a single `--stdlib <paths>` plus ONE
// `--stdlib-lang <lang>` applied to EVERY path in that list. A genuinely
// multi-language project therefore could not be expressed at all: the language
// was a property of the FLAG, not of the ROOT, so `--stdlib go:/opt/jdk/java.base
// --stdlib-lang java` mislabelled the Go root (only the literal "go" and "cobol"
// words self-corrected, because they were special-cased inline in each binary).
// That inline special-casing was also duplicated in gen-server, understand-server
// and genbench, so the three could drift.
//
// This file fixes both problems with one shared entry point:
//
//  1. ParseStdlibSpec — the language is now PER ROOT. Each comma-separated entry
//     may carry its own optional "lang=" prefix:
//
//     --stdlib go                              → Go, GOROOT/src, import-scoped
//     --stdlib go,cobol                        → Go tree + COBOL synthetic catalog
//     --stdlib java=/opt/jdk/java.base         → explicit language for that root
//     --stdlib go,java=/opt/jdk/java.base,/vendor/pq
//     --stdlib auto                            → detect from the project (default)
//     --stdlib none                            → ingest nothing
//
//  2. DetectStdlibRoots — "auto" resolves the roots from the PROJECT ITSELF: the
//     languages actually present in the index decide which runtimes to attach.
//     This is safe (not a guess we have to gate on) because every non-synthetic
//     root is AutoScope: the loader restricts ingest to the packages the project
//     genuinely imports, so attaching a root the project doesn't use costs one
//     emptyScopeSkipped++ and nothing else. A wrong-but-harmless guess is FREE,
//     which is precisely why auto-detection does not need a confirmation prompt.
//
// REPORTING, NOT PROMPTING
// The cost of a wrong stdlib decision is never destructive — worst case is "we
// ingested nothing and recall stays low", which is recoverable and observable.
// So instead of a blocking review step (hostile to CI, scripting and the
// air-gapped single-binary story), the loader records a StdlibReport: what was
// attached, what was skipped, and WHY. The caller prints it and the Understand
// Architecture page surfaces it, turning the edge-resolution % into a
// self-diagnosing number ("java skipped: JAVA_HOME not set" tells the user
// exactly what to pass). The one operation that DOES deserve a gate is whole-tree
// ingest (AutoScope=false), which can OOM the flat resolver — that stays opt-in.
package context

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	genast "gen-system/generator/internal/ast"
	"gen-system/generator/internal/symbols"
)

// Spec keywords accepted by ParseStdlibSpec in place of a path.
const (
	StdlibSpecAuto = "auto" // detect roots from the project's languages
	StdlibSpecNone = "none" // ingest nothing (explicit opt-out)
)

// StdlibRootStatus is the outcome of one root in the report.
type StdlibRootStatus string

const (
	StdlibAttached StdlibRootStatus = "attached" // ingested (or catalog materialised)
	StdlibSkipped  StdlibRootStatus = "skipped"  // deliberately not ingested
	StdlibMissing  StdlibRootStatus = "missing"  // wanted, but the tree wasn't found
)

// StdlibRootReport describes what happened to one stdlib root. It is the unit the
// CLI prints and the Architecture page renders.
type StdlibRootReport struct {
	Lang     string           `json:"lang"`
	Path     string           `json:"path"`   // directory, or a catalog id for synthetic roots
	Status   StdlibRootStatus `json:"status"` // attached | skipped | missing
	Detail   string           `json:"detail"` // human-readable reason (esp. for skipped/missing)
	Packages int              `json:"packages"`
	Symbols  int              `json:"symbols"`
	Auto     bool             `json:"auto"` // true when this root came from detection, not an explicit flag
}

// StdlibReport is the whole stdlib outcome for one LoadContext.
type StdlibReport struct {
	Mode  string             `json:"mode"` // "auto" | "explicit" | "none"
	Roots []StdlibRootReport `json:"roots"`
}

// Attached reports whether any root actually contributed symbols.
func (r StdlibReport) Attached() bool {
	for _, rt := range r.Roots {
		if rt.Status == StdlibAttached {
			return true
		}
	}
	return false
}

// ParseStdlibSpec turns a --stdlib spec into roots.
//
// The spec is a comma-separated list. Each entry is one of:
//
//	auto            → the caller should call DetectStdlibRoots (returns auto=true)
//	none            → ingest nothing (returns no roots, auto=false)
//	go              → GOROOT/src, LangGo, import-scoped
//	cobol           → the built-in synthetic COBOL runtime catalog
//	<lang>=<path>   → that path, tagged with that language
//	<path>          → that path; language falls back to fallbackLang (the legacy
//	                  --stdlib-lang flag), else it is left empty and the loader
//	                  goes purely by file extension (which is what it does anyway)
//
// An empty spec returns (nil, true): auto is the DEFAULT, so a caller that passes
// no flag gets detection. Pass "none" to opt out.
func ParseStdlibSpec(spec string, fallbackLang string) (roots []StdlibRoot, auto bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, true // default: auto-detect
	}
	// Accept BOTH the new comma form and the legacy OS path-list separator
	// (':' on Unix, ';' on Windows) that gen-server/genbench used, so existing
	// invocations keep working. A Windows drive letter ("C:\jdk") would be split
	// by ';' there, not ':', so this is safe on both platforms.
	sep := string(os.PathListSeparator)
	normalised := strings.ReplaceAll(spec, sep, ",")
	for _, raw := range strings.Split(normalised, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		switch strings.ToLower(entry) {
		case StdlibSpecNone:
			return nil, false // explicit opt-out wins outright
		case StdlibSpecAuto:
			auto = true
			continue
		}
		// optional "lang=" prefix — the language is a property of THIS root.
		lang := fallbackLang
		path := entry
		if i := strings.Index(entry, "="); i > 0 {
			lang = strings.TrimSpace(entry[:i])
			path = strings.TrimSpace(entry[i+1:])
		}
		if r, ok := stdlibRootFor(lang, path); ok {
			roots = append(roots, r)
		}
	}
	return roots, auto
}

// stdlibRootFor builds one StdlibRoot from a (lang, path) pair, expanding the
// well-known shorthands. Shared by the spec parser and the detector so the two
// can never disagree about what "go" or "cobol" means.
func stdlibRootFor(lang, path string) (StdlibRoot, bool) {
	lower := strings.ToLower(path)
	switch lower {
	case "go":
		src := gorootSrc()
		if src == "" {
			return StdlibRoot{}, false
		}
		return StdlibRoot{Path: src, Lang: genast.LangGo, AutoScope: true}, true
	case "cobol":
		// Synthetic: a built-in catalog of ISO intrinsics + runtime services.
		// No directory is walked, so it is essentially free and never scoped.
		return StdlibRoot{Path: "cobol", Lang: genast.LangCOBOL, Synthetic: true, AutoScope: false}, true
	}
	if path == "" {
		return StdlibRoot{}, false
	}
	return StdlibRoot{Path: path, Lang: genast.Language(lang), AutoScope: true}, true
}

// gorootSrc locates GOROOT/src.
//
// Deliberately does NOT shell out to `go env GOROOT`. understand-server is the
// air-gapped enterprise binary; spawning a subprocess from it to answer a
// configuration question is the kind of thing that has to be justified to a bank's
// security review, and it buys little: runtime.GOROOT() is the compiled-in path
// and $GOROOT covers the override case. If both miss (e.g. a relocated toolchain),
// the caller reports "GOROOT not found" and the user passes --stdlib go=<path>,
// which is explicit and auditable.
func gorootSrc() string {
	if gr := runtime.GOROOT(); gr != "" {
		if p := filepath.Join(gr, "src"); isDir(p) {
			return p
		}
	}
	if gr := strings.TrimSpace(os.Getenv("GOROOT")); gr != "" {
		if p := filepath.Join(gr, "src"); isDir(p) {
			return p
		}
	}
	return ""
}

// javaStdlibTree locates a Java signature/source tree to ingest, or "" with a
// reason. We deliberately do NOT guess wildly: an unlabelled directory that
// happens to contain .java files is not necessarily a JDK.
func javaStdlibTree() (path string, detail string) {
	home := strings.TrimSpace(os.Getenv("JAVA_HOME"))
	if home == "" {
		return "", "JAVA_HOME not set — pass --stdlib java=<jdk signature tree> to resolve Java calls"
	}
	// A signature tree laid out as <home>/java.base (the JDK module layout the
	// v23.32 Java proof used) is the ingestible shape.
	if p := filepath.Join(home, "java.base"); isDir(p) {
		return p, ""
	}
	// Some distributions ship sources under <home>/lib/src.zip — an archive we
	// cannot walk. Report it precisely rather than silently ingesting nothing.
	if isFile(filepath.Join(home, "lib", "src.zip")) {
		return "", "JAVA_HOME has lib/src.zip (archive, not walkable) — extract it and pass --stdlib java=<dir>"
	}
	return "", "no java.base signature tree under JAVA_HOME — pass --stdlib java=<dir>"
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// projectLanguages returns the distinct languages of the PROJECT's own symbols
// (stdlib/third-party tiers excluded — they are what we are about to attach, so
// counting them would be circular).
func projectLanguages(idx *symbols.Index) map[genast.Language]int {
	out := map[genast.Language]int{}
	if idx == nil {
		return out
	}
	for _, s := range idx.AllSymbols() {
		if s == nil || s.Provenance.IsExternal() {
			continue
		}
		if s.Language == "" {
			continue
		}
		out[s.Language]++
	}
	return out
}

// DetectStdlibRoots derives the stdlib roots from the project itself: for every
// language actually present in the index, attach that language's runtime if we
// can locate one. Returns the roots to ingest plus a pre-filled report carrying
// the roots we could NOT attach (with the reason), so the caller can tell the
// user exactly what to pass to close the gap.
//
// Safe by construction: every returned non-synthetic root is AutoScope, so the
// loader ingests only the packages the project actually imports. Attaching a
// runtime the project turns out not to use costs one skipped-scope counter.
//
// Languages we deliberately do NOT auto-attach:
//   - python / typescript — the loader handles their extensions, but no root has
//     been validated for them (see the v23.32 follow-ups); auto-attaching an
//     unvalidated tree would be the one case where a wrong guess is expensive.
//   - cagen — no external runtime; CA Gen action diagrams are self-contained.
//
// Both remain reachable EXPLICITLY via `--stdlib python=<dir>` etc.
func DetectStdlibRoots(idx *symbols.Index) ([]StdlibRoot, StdlibReport) {
	rep := StdlibReport{Mode: "auto"}
	langs := projectLanguages(idx)
	if len(langs) == 0 {
		return nil, rep
	}

	// Deterministic order so the report and the ingest are reproducible.
	names := make([]string, 0, len(langs))
	for l := range langs {
		names = append(names, string(l))
	}
	sort.Strings(names)

	var roots []StdlibRoot
	for _, name := range names {
		switch genast.Language(name) {
		case genast.LangGo:
			src := gorootSrc()
			if src == "" {
				rep.Roots = append(rep.Roots, StdlibRootReport{
					Lang: name, Status: StdlibMissing, Auto: true,
					Detail: "GOROOT not found — install Go or pass --stdlib go=<GOROOT/src>",
				})
				continue
			}
			roots = append(roots, StdlibRoot{Path: src, Lang: genast.LangGo, AutoScope: true, autoDetected: true})

		case genast.LangCOBOL:
			// Free: a built-in catalog, no disk walk, no scoping.
			roots = append(roots, StdlibRoot{
				Path: "cobol", Lang: genast.LangCOBOL, Synthetic: true, AutoScope: false,
				autoDetected: true,
			})

		case genast.LangJava:
			p, why := javaStdlibTree()
			if p == "" {
				rep.Roots = append(rep.Roots, StdlibRootReport{
					Lang: name, Status: StdlibMissing, Auto: true, Detail: why,
				})
				continue
			}
			roots = append(roots, StdlibRoot{Path: p, Lang: genast.LangJava, AutoScope: true, autoDetected: true})

		default:
			// python / typescript / cagen and anything new: not auto-attached.
			rep.Roots = append(rep.Roots, StdlibRootReport{
				Lang: name, Status: StdlibSkipped, Auto: true,
				Detail: "no auto-detected runtime for this language — pass --stdlib " + name + "=<dir> to attach one",
			})
		}
	}
	return roots, rep
}

// FormatStdlibReport renders the report as the lines a CLI should print. Kept
// here (not in each binary) so gen-server, understand-server and genbench cannot
// drift in how they describe the same outcome.
func FormatStdlibReport(rep StdlibReport) []string {
	if len(rep.Roots) == 0 {
		return []string{"   Stdlib: none (" + rep.Mode + ")"}
	}
	lines := []string{"   Stdlib (" + rep.Mode + "):"}
	for _, r := range rep.Roots {
		lang := r.Lang
		if lang == "" {
			lang = "?"
		}
		switch r.Status {
		case StdlibAttached:
			where := r.Path
			if where == "cobol" {
				where = "<built-in catalog>"
			}
			lines = append(lines, "     "+pad(lang, 8)+pad(where, 40)+
				" "+itoa(r.Packages)+" pkgs  "+itoa(r.Symbols)+" syms  ✓")
		default:
			lines = append(lines, "     "+pad(lang, 8)+pad("— "+string(r.Status), 40)+" "+r.Detail)
		}
	}
	return lines
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// itoa avoids pulling strconv in for one call site in a formatting helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
