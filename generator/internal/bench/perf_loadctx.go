// generator/internal/bench/perf_loadctx.go — v19.7: LoadContext end-to-end
// latency for the four practical configurations:
//
//	plain:           LoadContext(root)
//	with_copybook:   LoadContextWithOpts(root, {CopybookPath: ...})
//	warm_cache:      second call after first has populated DefaultCache
//	skill_warmup:    LoadContextWithSkill(root, skill) if skill file exists
//
// Reports wall-clock seconds + symbol/file counts so a regression run can
// detect a slowdown that doesn't change correctness.
package bench

import (
	"fmt"
	"os"
	"time"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
)

// LoadContextLatency runs each of the four configurations once and emits
// timing metrics. skillPath may be "" to skip the skill_warmup leg.
func LoadContextLatency(root string, copybookPath []string, skillPath string) Suite {
	t0 := time.Now()
	s := Suite{Name: "loadcontext_latency"}

	bench := func(name string, fn func() agentctx.Context) {
		t1 := time.Now()
		ctx := fn()
		dur := time.Since(t1).Seconds()
		syms := 0
		files := len(ctx.Files)
		if ctx.Index != nil {
			syms = len(ctx.Index.AllSymbols())
		}
		s.Metrics = append(s.Metrics,
			Metric{Name: fmt.Sprintf("seconds_%s", name), Value: dur, Unit: "s"},
			Metric{Name: fmt.Sprintf("symbols_%s", name), Value: float64(syms), Unit: "symbols", HigherIsBetter: true},
			Metric{Name: fmt.Sprintf("files_%s", name), Value: float64(files), Unit: "files", HigherIsBetter: true},
		)
		s.Cases = append(s.Cases, Case{
			Name: fmt.Sprintf("%s_returned_index", name),
			Pass: ctx.Index != nil && syms > 0,
			Got:  fmt.Sprintf("syms=%d files=%d in %.3fs", syms, files, dur),
			Want: "non-nil index, >0 symbols",
		})
	}

	bench("plain", func() agentctx.Context { return agentctx.LoadContext(root) })
	bench("with_copybook", func() agentctx.Context {
		return agentctx.LoadContextWithOpts(root, agentctx.LoadContextOpts{CopybookPath: copybookPath})
	})
	// Run plain again — DefaultCache should make this faster.
	bench("warm_cache", func() agentctx.Context { return agentctx.LoadContext(root) })

	if skillPath != "" {
		if _, err := os.Stat(skillPath); err == nil {
			bench("skill_warmup", func() agentctx.Context {
				return agentctx.LoadContextWithSkill(root, skillPath)
			})
		} else {
			s.Cases = append(s.Cases, Case{
				Name: "skill_file_exists", Pass: false,
				Got: err.Error(), Want: "readable file at " + skillPath,
			})
		}
	}

	s.Duration = time.Since(t0)
	return s
}
