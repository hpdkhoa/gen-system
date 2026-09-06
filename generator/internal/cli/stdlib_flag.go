// stdlib_flag.go — v26.34: `--stdlib <spec>` (or GEN_STDLIB) for generation
// runs, the same spec understand-server takes: empty or "auto" detects a
// runtime for every language in the project (the default), "none" opts out,
// "go" is GOROOT/src, "java=/opt/jdk/java.base" is explicit, entries are
// comma separated and mixable. The result lands in agent.LoadOpts, which every
// LoadContext call on the generation path uses, including the Understand
// verify pass over the generated backend.
package cli

import (
	"fmt"
	"os"
	"strings"

	"gen-system/generator/internal/agent"
	agentctx "gen-system/generator/internal/context"
)

var stdlibConfigured bool

// SetupStdlib reads --stdlib or GEN_STDLIB once and configures agent.LoadOpts.
func SetupStdlib() {
	if stdlibConfigured {
		return
	}
	stdlibConfigured = true
	spec, ok := flagValue("--stdlib")
	if !ok {
		spec = os.Getenv("GEN_STDLIB")
	}
	roots, auto := agentctx.ParseStdlibSpec(spec, "")
	agent.LoadOpts.StdlibRoots = roots
	agent.LoadOpts.StdlibAuto = auto
	switch {
	case auto && len(roots) == 0:
		fmt.Println("📚 stdlib: auto (a runtime is attached for every language found in the project; --stdlib none to opt out)")
	case auto:
		fmt.Printf("📚 stdlib: auto plus %d explicit root(s)\n", len(roots))
	case len(roots) == 0:
		fmt.Println("📚 stdlib: none (calls into the standard library stay external)")
	default:
		var names []string
		for _, r := range roots {
			names = append(names, string(r.Lang)+"="+r.Path)
		}
		fmt.Printf("📚 stdlib: %s\n", strings.Join(names, ", "))
	}
}

// loadOpts configures the stdlib once and returns the agent's load options.
func loadOpts() agentctx.LoadContextOpts {
	SetupStdlib()
	return agent.LoadOpts
}
