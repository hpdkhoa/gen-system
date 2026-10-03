// context/cobol_runtime.go — v23.33 (COBOL runtime catalog): NEW. COBOL has no
// source standard library to ingest the way Go (GOROOT/src) or Java (a JDK
// signature tree) do. Its "runtime" is two things, neither of which is a source
// tree you can point a loader at:
//
//  1. INTRINSIC FUNCTIONS — FUNCTION NUMVAL, FUNCTION CURRENT-DATE, FUNCTION
//     MAX … defined by the ISO/ANSI COBOL standard. No source exists; they are
//     part of the language.
//  2. RUNTIME / CALLABLE SERVICES — CEE3ABD, CEEMOUT, IGZ* , DSNTIAR … IBM
//     Language Environment and common vendor services, shipped as compiled
//     binaries on the mainframe. No portable source.
//
// Before this, every CALL "CEE3ABD" and FUNCTION NUMVAL surfaced as an
// unresolved (external) call edge — visually identical to a genuinely missing
// program. That is the bulk of COBOL's "external" calls and the reason overall
// recall on a real codebase sits well below internal recall.
//
// This catalog materialises those well-known names as SYNTHETIC ProvStdlib
// symbols so the calls RESOLVE to a recognised runtime symbol instead of
// reading as "unknown program worth investigating". It reuses the exact v23.32
// provenance machinery: the synthetic module is added with ProvStdlib, the
// resolver links CALL/FUNCTION names to it, and the Understand UI hides it by
// default behind the same stdlib toggle. It adds no new source. It makes the
// analysis clearer, since a known runtime service no longer reads as a missing program.
//
// Fidelity: these are SIGNATURE-LEVEL stubs (name + provenance, no body),
// the same fidelity level as the Java JDK signature tree. They mark "this call
// targets a known runtime service", nothing more. The list is curated and
// conservative; an unrecognised CALL still resolves to nothing and stays
// (correctly) external, so the catalog can only improve recall, never inflate
// it with guesses.
package context

import (
	genast "github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// cobolIntrinsics are the ISO/ANSI COBOL intrinsic functions (invoked as
// FUNCTION <name>). Conservative, standard set — not vendor extensions.
var cobolIntrinsics = []string{
	// numeric
	"ABS", "ACOS", "ANNUITY", "ASIN", "ATAN", "COS", "FACTORIAL",
	"INTEGER", "INTEGER-PART", "LOG", "LOG10", "MAX", "MEAN", "MEDIAN",
	"MIDRANGE", "MIN", "MOD", "NUMVAL", "NUMVAL-C", "NUMVAL-F",
	"PRESENT-VALUE", "RANDOM", "RANGE", "REM", "SIN", "SQRT",
	"STANDARD-DEVIATION", "SUM", "TAN", "VARIANCE",
	// character / string
	"CHAR", "LENGTH", "LOWER-CASE", "NUMVAL", "ORD", "ORD-MAX", "ORD-MIN",
	"REVERSE", "TRIM", "UPPER-CASE", "SUBSTITUTE", "SUBSTITUTE-CASE",
	"BYTE-LENGTH", "CONCATENATE", "DISPLAY-OF", "NATIONAL-OF",
	// date / time
	"CURRENT-DATE", "DATE-OF-INTEGER", "DATE-TO-YYYYMMDD",
	"DAY-OF-INTEGER", "DAY-TO-YYYYDDD", "INTEGER-OF-DATE",
	"INTEGER-OF-DAY", "YEAR-TO-YYYY", "WHEN-COMPILED",
	// misc / boolean / bit
	"BOOLEAN-OF-INTEGER", "CHAR-NATIONAL", "COMBINED-DATETIME",
	"EXCEPTION-FILE", "EXCEPTION-LOCATION", "EXCEPTION-STATEMENT",
	"EXCEPTION-STATUS", "FORMATTED-CURRENT-DATE", "FORMATTED-DATE",
	"FORMATTED-DATETIME", "FORMATTED-TIME", "HIGHEST-ALGEBRAIC",
	"LOWEST-ALGEBRAIC", "INTEGER-OF-FORMATTED-DATE", "TEST-NUMVAL",
	"TEST-NUMVAL-C", "TEST-NUMVAL-F", "TEST-DATE-YYYYMMDD",
	"TEST-DAY-YYYYDDD", "PI", "E",
}

// cobolRuntimeServices are common Language-Environment and vendor callable
// services invoked as CALL "<name>". Curated, conservative — the ones that
// actually appear in real z/OS COBOL and would otherwise read as missing
// programs. (Not exhaustive; intentionally so.)
var cobolRuntimeServices = []string{
	// Language Environment (CEE*) — the most common callable services.
	"CEE3ABD", "CEE3DMP", "CEE3GRN", "CEE3GRO", "CEE3PRM", "CEE3SRP",
	"CEEMOUT", "CEEMRCR", "CEEMRCE", "CEEDATM", "CEEDAYS", "CEELOCT",
	"CEESECS", "CEEGMT", "CEEGMTO", "CEEUTC", "CEEFMDA", "CEEFMDT",
	"CEEISEC", "CEEDCOD", "CEESCEN", "CEEGTST", "CEEFRST", "CEECRHP",
	"CEECZST", "CEEDSHP", "CEEHDLR", "CEEHDLU", "CEESGL", "CEEMSG",
	"CEENCOD", "CEEGPID", "CEERAN0",
	// IBM-supplied COBOL runtime helpers (IGZ*).
	"IGZEOPT", "IGZETUN", "IGZBRDGE",
	// DB2 / SQL helpers commonly CALLed from COBOL.
	"DSNTIAR", "DSNTIAC", "DSNALI", "DSNHLI",
	// CICS / sort / misc system utilities frequently CALLed by name.
	"SORT", "MERGE", "ILBOABN0", "ABEND", "SNAP",
}

// loadSyntheticCatalog materialises a synthetic stdlib module for a catalog id.
// Returns the number of catalog symbols added (for trace counting). Currently
// only the "cobol" catalog is defined; an unknown id is a no-op.
func (ctx *Context) loadSyntheticCatalog(root StdlibRoot) int {
	switch root.Path {
	case "cobol":
		return ctx.addCobolRuntimeCatalog()
	default:
		return 0
	}
}

// addCobolRuntimeCatalog builds one synthetic module holding every catalog name
// as a NodeFunction, tags it ProvStdlib via AddModuleWithProvenance, and lets
// the global resolver link COBOL CALL/FUNCTION edges to it. The file path is a
// stable synthetic marker so the symbols have a non-empty DefinedIn and the UI
// can label them.
func (ctx *Context) addCobolRuntimeCatalog() int {
	const synthPath = "<cobol-runtime>"
	m := &genast.Module{
		Language: genast.LangCOBOL,
		FilePath: synthPath,
	}

	add := func(name, note string) {
		m.Nodes = append(m.Nodes, &genast.Node{
			Kind:     genast.NodeFunction,
			Name:     name,
			Language: genast.LangCOBOL,
			Exported: true, // cross-program resolvable, like a callable service
			Span:     genast.Span{Start: genast.Pos{Line: 1, Column: 1}},
			// RawSource carries a short human label for the Understand panel.
			RawSource: note,
		})
	}

	for _, fn := range cobolIntrinsics {
		add(fn, "COBOL intrinsic function (ISO standard)")
	}
	for _, svc := range cobolRuntimeServices {
		add(svc, "COBOL runtime / callable service")
	}

	ctx.Index.AddModuleWithProvenance(m, symbols.ProvStdlib)
	return len(m.Nodes)
}
