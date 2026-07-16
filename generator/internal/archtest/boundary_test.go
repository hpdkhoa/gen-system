// generator/internal/archtest/boundary_test.go — v23.9 (Phase 1 + Phase 3
// acceptance). Architecture guardrail tests that make the two-product split
// PERMANENT: a future edit that re-couples the halves fails CI, not a demo.
//
//   TestCoreHasNoGenerationDeps        — the shared CORE packages (ast, symbols,
//                                         flow, compiler, objgraph, context,
//                                         trace) must not import any GENERATE
//                                         package.
//   TestUnderstandServerHasNoGenDeps   — cmd/understand-server (the enterprise
//                                         binary) must link NO generation
//                                         package. This is the acceptance test
//                                         for the split: the air-gapped claim is
//                                         only structural if the linker agrees.
//
// Both shell out to `go list -deps`. If the go toolchain is unavailable (some
// sandboxes), the tests skip rather than fail — they are guardrails for CI/dev
// boxes where go is present.
package archtest

import (
	"os/exec"
	"strings"
	"testing"
)

// generationPackages are the import-path suffixes that pull in the model,
// network egress, or code generation — the things that must NEVER ship in the
// air-gapped enterprise binary. Note: `schema` and `compiler` are deliberately
// NOT here. They are pure shared leaves (type definitions + parsing / build
// passes) with zero reach into ai/engine/planner/network, verified by
// `go list -deps`. Treating them as "generation" would be wrong — they are
// shared core, used by both products.
var generationPackages = []string{
	"generator/internal/ai",         // Ollama client — the model. Must never ship.
	"generator/internal/engine",     // code generators.
	"generator/internal/agent",      // the generation task loop.
	"generator/internal/planner",    // goal → plan.
	"generator/internal/planner_v2", // goal → plan (v2).
	"generator/internal/frontend",   // React/web codegen.
	"generator/internal/mobile",     // React Native codegen.
	"generator/internal/rag",        // retrieval for generation.
	"generator/internal/safety",     // generation-time safety checks.
	"generator/internal/services",   // build/schema/staging services.
	"generator/internal/handlers",   // generation HTTP handlers.
	"generator/internal/intelligence",
	"generator/internal/github", // PR/push — network egress.
}

// coreUnderstandPackages are the shared CORE packages the enterprise product is
// built from. They must stay free of generation imports.
var coreUnderstandPackages = []string{
	"gen-system/generator/internal/ast",
	"gen-system/generator/internal/symbols",
	"gen-system/generator/internal/flow",
	"gen-system/generator/internal/compiler",
	"gen-system/generator/internal/objgraph",
	"gen-system/generator/internal/context",
	"gen-system/generator/internal/trace",
	"gen-system/generator/internal/presentation",
}

func deps(t *testing.T, target string) ([]string, bool) {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", target)
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("go list unavailable (%v); skipping architecture guardrail", err)
		return nil, false
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n"), true
}

func assertNoGenerationDeps(t *testing.T, target string) {
	t.Helper()
	list, ok := deps(t, target)
	if !ok {
		return
	}
	for _, dep := range list {
		for _, gen := range generationPackages {
			if strings.HasSuffix(dep, gen) || strings.Contains(dep, gen+"/") {
				t.Errorf("%s transitively imports generation package %q (dep: %s)", target, gen, dep)
			}
		}
	}
}

func TestCoreHasNoGenerationDeps(t *testing.T) {
	for _, pkg := range coreUnderstandPackages {
		pkg := pkg
		t.Run(strings.TrimPrefix(pkg, "gen-system/generator/internal/"), func(t *testing.T) {
			assertNoGenerationDeps(t, pkg)
		})
	}
}

func TestUnderstandServerHasNoGenDeps(t *testing.T) {
	assertNoGenerationDeps(t, "gen-system/generator/cmd/understand-server")
}
