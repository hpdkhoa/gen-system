// context/stdlib_java_test.go — v23.32 (stdlib provenance): the SECOND-LANGUAGE
// PROOF. Everything the Go stdlib path exercises (provenance tagging, the
// import-scoped loader, cross-boundary resolution) must work for Java through
// the SAME shared machinery, with zero Java-specific code in symbols/context/
// flow. If this passes, the stdlib design is genuinely language-agnostic — which
// is the whole point of building the pure-Go Java reader before any sidecar.
//
// Java's stdlib ships as bytecode (no src.zip on a headless JDK), so this uses a
// signature-level fixture under testdata/java_stdlib laid out exactly as a real
// JDK signature tree would be (java/util/ArrayList.java). Bodies are empty, so
// the frontend records them as NodeUnknown — the honest representation of
// "declaration known, implementation not modelled". The sidecar, when it lands,
// would supply the bodies; this proves the resolution boundary works without it.
package context

import (
	"path/filepath"
	"testing"

	genast "gen-system/generator/internal/ast"
	"gen-system/generator/internal/symbols"
)

func TestStdlibProvenance_Java_SecondLanguageProof(t *testing.T) {
	proj := filepath.Join("testdata", "java_proj")
	stdlib := filepath.Join("testdata", "java_stdlib")

	opts := LoadContextOpts{
		StdlibRoots: []StdlibRoot{{
			Path:      stdlib,
			Lang:      genast.LangJava,
			AutoScope: true, // scope to the project's imports (java.util)
		}},
	}
	ctx := LoadContextWithOpts(proj, opts)
	if ctx.Index == nil {
		t.Fatal("nil index")
	}

	// 1) Stdlib symbols were ingested and tagged ProvStdlib — through the same
	//    AddModuleWithProvenance path Go uses, with no Java-specific code.
	var stdlibJava, projectJava int
	var sawArrayListAdd bool
	for _, s := range ctx.Index.AllSymbols() {
		if s.Language != genast.LangJava {
			continue
		}
		if s.Provenance == symbols.ProvStdlib {
			stdlibJava++
			if s.Name == "add" {
				sawArrayListAdd = true
			}
		} else {
			projectJava++
		}
	}
	if stdlibJava == 0 {
		t.Error("no Java stdlib symbols tagged ProvStdlib — loader did not ingest the signature tree")
	}
	if projectJava == 0 {
		t.Error("no Java project symbols — project parse failed")
	}
	if !sawArrayListAdd {
		t.Error("expected the stdlib ArrayList.add signature to be present as a stdlib symbol")
	}

	// 2) Cross-boundary resolution: at least one project-caller edge resolves
	//    INTO a stdlib symbol. This is the Java equivalent of "resolve into the
	//    runtime", proving the shared resolver tie-break is language-neutral.
	intoStdlib := 0
	for _, e := range ctx.Index.AllCallEdges() {
		caller, ok := ctx.Index.ByID(e.CallerID)
		if !ok || caller == nil || caller.Provenance.IsExternal() {
			continue
		}
		if e.CalleeID == "" {
			continue
		}
		callee, ok := ctx.Index.ByID(e.CalleeID)
		if ok && callee != nil && callee.Provenance == symbols.ProvStdlib {
			intoStdlib++
		}
	}
	if intoStdlib == 0 {
		t.Error("no project→stdlib Java call edges resolved; cross-boundary resolution failed for Java")
	}
	t.Logf("Java proof: stdlib_syms=%d project_syms=%d edges_into_stdlib=%d",
		stdlibJava, projectJava, intoStdlib)
}

// v24 — collectModuleImports must surface a Java module's imports (emitted as
// NodeImport children with ImportPath) so stdlib AutoScope can scope the JDK
// ingest to referenced packages instead of walking the whole tree.
func TestCollectModuleImports_Java_v24(t *testing.T) {
	fe := &genast.JavaFrontend{}
	src := []byte(`package com.example;
import java.util.HashMap;
import java.util.Map;
class C { void f(){ Map<String,Integer> m = new HashMap<>(); } }`)
	m, err := fe.Parse("C.java", src)
	if err != nil {
		t.Fatal(err)
	}
	imps := collectModuleImports(m)
	want := map[string]bool{"java.util.HashMap": false, "java.util.Map": false}
	for _, p := range imps {
		if _, ok := want[p]; ok {
			want[p] = true
		}
	}
	for p, seen := range want {
		if !seen {
			t.Errorf("import %q not collected (got %v)", p, imps)
		}
	}
}
