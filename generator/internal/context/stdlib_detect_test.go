// context/stdlib_detect_test.go (v26.5). Pins the two things that were broken
// before: (1) the language must be a property of each root (the old single
// --stdlib-lang could not express a multi-language project at all), and (2)
// auto-detection must key off the languages actually present in the project.
package context

import (
	"os"
	"testing"

	genast "github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func TestParseStdlibSpec_PerRootLanguage_v265(t *testing.T) {
	// The regression this exists for: before v26.5 a single --stdlib-lang was
	// applied to every path, so a Go + Java project was inexpressible. Each entry
	// must now carry its own language.
	roots, auto := ParseStdlibSpec("go,java=/opt/jdk/java.base", "")
	if auto {
		t.Fatalf("explicit spec should not request auto-detection")
	}
	if len(roots) != 2 {
		t.Fatalf("roots = %d, want 2 (%+v)", len(roots), roots)
	}
	var sawGo, sawJava bool
	for _, r := range roots {
		switch r.Lang {
		case genast.LangGo:
			sawGo = true
			if !r.AutoScope {
				t.Errorf("go root should be import-scoped")
			}
		case genast.LangJava:
			sawJava = true
			if r.Path != "/opt/jdk/java.base" {
				t.Errorf("java path = %q, want /opt/jdk/java.base", r.Path)
			}
			if !r.AutoScope {
				t.Errorf("java root should be import-scoped")
			}
		}
	}
	if !sawGo {
		// Only fails when GOROOT/src is genuinely absent from the sandbox; the
		// java half is the part that proves the per-root language.
		t.Logf("go root absent (no GOROOT/src on this machine) — java assertions still apply")
	}
	if !sawJava {
		t.Errorf("java root missing — per-root language did not parse")
	}
}

func TestParseStdlibSpec_AutoAndNone_v265(t *testing.T) {
	if _, auto := ParseStdlibSpec("", ""); !auto {
		t.Errorf("empty spec must default to auto-detection")
	}
	if _, auto := ParseStdlibSpec("auto", ""); !auto {
		t.Errorf("'auto' must request auto-detection")
	}
	roots, auto := ParseStdlibSpec("none", "")
	if auto || len(roots) != 0 {
		t.Errorf("'none' must opt out entirely: roots=%d auto=%v", len(roots), auto)
	}
	// 'none' wins even when combined with other entries: an explicit opt-out always applies.
	roots, auto = ParseStdlibSpec("go,none", "")
	if auto || len(roots) != 0 {
		t.Errorf("'none' must win over other entries: roots=%d auto=%v", len(roots), auto)
	}
}

func TestParseStdlibSpec_CobolSynthetic_v265(t *testing.T) {
	roots, _ := ParseStdlibSpec("cobol", "")
	if len(roots) != 1 {
		t.Fatalf("roots = %d, want 1", len(roots))
	}
	r := roots[0]
	if !r.Synthetic {
		t.Errorf("cobol root must be Synthetic (built-in catalog, no directory walk)")
	}
	if r.AutoScope {
		// A synthetic catalog has no import scope to derive; leaving AutoScope on
		// made an earlier hand-rolled copy in genbench subtly different from the
		// gen-server one. Both now come from stdlibRootFor, so this can't drift.
		t.Errorf("cobol root must not be AutoScope")
	}
	if r.Lang != genast.LangCOBOL {
		t.Errorf("lang = %q, want cobol", r.Lang)
	}
}

func TestParseStdlibSpec_LegacyPathListSeparator_v265(t *testing.T) {
	// gen-server and genbench used filepath.SplitList before v26.5. Those
	// invocations must keep working, so the parser accepts the OS path-list
	// separator as well as the new comma form.
	spec := "cobol" + string(os.PathListSeparator) + "cobol"
	roots, _ := ParseStdlibSpec(spec, "")
	if len(roots) != 2 {
		t.Fatalf("legacy path-list separator not accepted: roots=%d", len(roots))
	}
}

func TestDetectStdlibRoots_KeysOffProjectLanguages_v265(t *testing.T) {
	idx := symbols.NewIndex()

	// A COBOL-only project must get the COBOL catalog and nothing else. COBOL is
	// the one language whose runtime is always available (it is a built-in
	// catalog, not a tree on disk), so this assertion is machine-independent.
	idx.AddModule(&genast.Module{
		FilePath: "PAYROLL.cob",
		Language: genast.LangCOBOL,
		Nodes: []*genast.Node{
			{Kind: genast.NodeFunction, Name: "MAIN-PARA"},
		},
	})

	roots, rep := DetectStdlibRoots(idx)
	if rep.Mode != "auto" {
		t.Errorf("mode = %q, want auto", rep.Mode)
	}
	var sawCobol bool
	for _, r := range roots {
		if r.Lang == genast.LangCOBOL {
			sawCobol = true
			if !r.Synthetic {
				t.Errorf("detected cobol root must be synthetic")
			}
			if !r.autoDetected {
				t.Errorf("detected root must be marked autoDetected (reporting only)")
			}
		}
		if r.Lang == genast.LangJava {
			t.Errorf("java runtime attached to a project with no java symbols")
		}
	}
	if !sawCobol {
		t.Errorf("cobol project did not get the cobol runtime catalog")
	}
}

func TestDetectStdlibRoots_EmptyIndex_v265(t *testing.T) {
	roots, rep := DetectStdlibRoots(symbols.NewIndex())
	if len(roots) != 0 {
		t.Errorf("empty project attached %d roots, want 0", len(roots))
	}
	if len(rep.Roots) != 0 {
		t.Errorf("empty project reported %d roots, want 0", len(rep.Roots))
	}
}

func TestDetectStdlibRoots_UnsupportedLangIsReportedNotGuessed_v265(t *testing.T) {
	// Python has no validated runtime tree, so we must not guess one. We must
	// still say so. That reported line is what tells the user which flag to pass
	// and is the whole point of report-instead-of-prompt.
	idx := symbols.NewIndex()
	idx.AddModule(&genast.Module{
		FilePath: "app.py",
		Language: genast.LangPython,
		Nodes:    []*genast.Node{{Kind: genast.NodeFunction, Name: "main"}},
	})
	roots, rep := DetectStdlibRoots(idx)
	for _, r := range roots {
		if r.Lang == genast.LangPython {
			t.Errorf("python runtime auto-attached — must be explicit only")
		}
	}
	var reported bool
	for _, r := range rep.Roots {
		if r.Lang == string(genast.LangPython) && r.Status == StdlibSkipped && r.Detail != "" {
			reported = true
		}
	}
	if !reported {
		t.Errorf("python was silently ignored; it must be reported as skipped with a reason")
	}
}
