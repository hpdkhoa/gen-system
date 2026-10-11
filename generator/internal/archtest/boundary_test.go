// generator/internal/archtest/boundary_test.go (v23.9, Phase 1 + Phase 3
// acceptance). Architecture guardrail tests that make the two-product split
// permanent: a future edit that re-couples the halves fails CI, so it never reaches a demo.
//
//	TestCoreHasNoGenerationDeps:          the shared core packages (ast, symbols,
//	                                      flow, compiler, objgraph, context,
//	                                      trace) must not import any generate
//	                                      package.
//	TestUnderstandServerHasNoGenDeps:     cmd/understand-server (the enterprise
//	                                      binary) must link no generation
//	                                      package. This is the acceptance test
//	                                      for the split: the air-gapped claim is
//	                                      only structural if the linker agrees.
//
// Both shell out to `go list -deps`. If the go toolchain is unavailable (some
// sandboxes), the tests skip rather than fail. They are guardrails for CI/dev
// boxes where go is present.
package archtest

import (
	"os/exec"
	"strings"
	"testing"
)

// generationPackages are the import-path suffixes that pull in the model,
// network egress, or code generation: the things that must never ship in the
// air-gapped enterprise binary. Note: `schema` and `compiler` are deliberately
// not here. They are pure shared leaves (type definitions + parsing / build
// passes) with zero reach into ai/engine/planner/network, verified by
// `go list -deps`. Treating them as "generation" would be wrong. They are
// shared core, used by both products.
var generationPackages = []string{
	"generator/internal/ai",         // Ollama client, the model. Must never ship.
	"generator/internal/enrich",     // LLM belief enrichment; imports ai (v26.22).
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
	"generator/internal/github", // PR/push: network egress.
}

// coreUnderstandPackages are the shared core packages the enterprise product is
// built from. They must stay free of generation imports.
var coreUnderstandPackages = []string{
	"github.com/hpdkhoa/gen-system/generator/internal/ast",
	"github.com/hpdkhoa/gen-system/generator/internal/symbols",
	"github.com/hpdkhoa/gen-system/generator/internal/flow",
	"github.com/hpdkhoa/gen-system/generator/internal/compiler",
	"github.com/hpdkhoa/gen-system/generator/internal/objgraph",
	"github.com/hpdkhoa/gen-system/generator/internal/context",
	"github.com/hpdkhoa/gen-system/generator/internal/trace",
	"github.com/hpdkhoa/gen-system/generator/internal/presentation",
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
		t.Run(strings.TrimPrefix(pkg, "github.com/hpdkhoa/gen-system/generator/internal/"), func(t *testing.T) {
			assertNoGenerationDeps(t, pkg)
		})
	}
}

func TestUnderstandServerHasNoGenDeps(t *testing.T) {
	assertNoGenerationDeps(t, "github.com/hpdkhoa/gen-system/generator/cmd/understand-server")
}

// TestUnderstandMCPHasNoGenDeps: cmd/understand-mcp (the Understand half as an
// MCP server) and its protocol package hold the same boundary as
// understand-server. An agent that calls it must not be able to reach a model
// or the generator through it.
func TestUnderstandMCPHasNoGenDeps(t *testing.T) {
	assertNoGenerationDeps(t, "github.com/hpdkhoa/gen-system/generator/cmd/understand-mcp")
	assertNoGenerationDeps(t, "github.com/hpdkhoa/gen-system/generator/internal/mcp")
}
