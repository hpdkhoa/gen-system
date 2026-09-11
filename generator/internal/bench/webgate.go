// generator/internal/bench/webgate.go — v23.13 (Phase 5): a benchmark suite for
// the generated WEB layer's build gate. It generates the Vite+React+TS project
// for a fixed schema, then asserts structural correctness (the buildable files
// exist) and, IF a Node toolchain is present, runs `tsc --noEmit` as the real
// type-check gate. Where Node is absent (e.g. an air-gapped bench box) the
// toolchain cases are recorded as skipped notes rather than hard failures —
// same graceful-degradation posture as the postgres_live suite when no DB is up.
package bench

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/engine"
	"github.com/hpdkhoa/gen-system/generator/internal/frontend"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// WebGate generates a web project and gates it with tsc (and npm build if deps
// can be installed). Returns a Suite with pass/fail cases + a metric.
func WebGate() Suite {
	t0 := time.Now()
	s := Suite{Name: "web_gate"}

	tmp, err := os.MkdirTemp("", "genbench-web-*")
	if err != nil {
		s.Cases = append(s.Cases, Case{Name: "mktemp", Pass: false, Note: err.Error()})
		s.Duration = time.Since(t0)
		return s
	}
	defer os.RemoveAll(tmp)

	entities := []schema.Entity{
		{Name: "Order", Fields: []schema.Field{
			{Name: "total", Type: "float"},
			{Name: "status", Type: "string"},
		}},
		{Name: "Product", Fields: []schema.Field{
			{Name: "name", Type: "string"},
			{Name: "stock", Type: "int"},
		}},
	}

	// Generate the TS types + barrel (which also emits the typed client +
	// tsconfig), then the Phase 5 buildable web project scaffold.
	for _, e := range entities {
		frontend.GenerateTypeScript(e, tmp)
	}
	frontend.GenerateTypesBarrel(entities, tmp)
	engine.GenerateWebProject(entities, tmp)

	web := filepath.Join(tmp, "frontend")

	// ── structural cases: the buildable project exists ──────────────
	mustExist := []struct{ name, rel string }{
		{"package.json", "package.json"},
		{"vite.config.ts", "vite.config.ts"},
		{"index.html", "index.html"},
		{"entry main.tsx", "src/main.tsx"},
		{"App.tsx", "src/App.tsx"},
		{"typed client", "src/api/client.ts"},
		{"types barrel", "src/types/index.ts"},
		{"tsconfig.json", "tsconfig.json"},
	}
	for _, m := range mustExist {
		_, statErr := os.Stat(filepath.Join(web, m.rel))
		s.Cases = append(s.Cases, Case{
			Name: "exists: " + m.name,
			Pass: statErr == nil,
			Want: "present",
			Got:  boolWord(statErr == nil, "present", "missing"),
		})
	}

	// App.tsx should consume the typed client (orderApi.list()) — proves the
	// web layer is wired to the contract, not orphaned fragments.
	if app, e := os.ReadFile(filepath.Join(web, "src", "App.tsx")); e == nil {
		wired := strings.Contains(string(app), "orderApi.list()") &&
			strings.Contains(string(app), `from "./api/client"`)
		s.Cases = append(s.Cases, Case{
			Name: "App wired to typed client",
			Pass: wired,
			Want: "imports + calls orderApi.list()",
			Got:  boolWord(wired, "wired", "not wired"),
		})
	}

	// ── toolchain gate: tsc --noEmit (only if Node present) ─────────
	npm, npmErr := exec.LookPath("npm")
	if npmErr != nil {
		s.Notes = "npm not found — tsc/build gate skipped (verify on a box with Node). Structural cases above still apply."
		s.Duration = time.Since(t0)
		return s
	}

	// npm ci/install then tsc --noEmit. Network may be unavailable on an
	// air-gapped bench box; if install fails, record a skip note (not a fail).
	t1 := time.Now()
	if out, e := runIn(web, npm, "install", "--no-audit", "--no-fund"); e != nil {
		s.Cases = append(s.Cases, Case{
			Name: "npm install",
			Pass: false,
			Note: "install failed (offline bench box?): " + firstLine(out),
		})
		s.Notes = "dependency install failed — tsc gate could not run here; verify on a connected box."
		s.Duration = time.Since(t0)
		return s
	}
	installSec := time.Since(t1).Seconds()

	tscOut, tscErr := runIn(web, npm, "run", "typecheck")
	s.Cases = append(s.Cases, Case{
		Name: "tsc --noEmit (strict type-check)",
		Pass: tscErr == nil,
		Want: "0 type errors",
		Got:  boolWord(tscErr == nil, "clean", firstLine(tscOut)),
		Note: noteErr(tscErr),
	})

	buildOut, buildErr := runIn(web, npm, "run", "build")
	s.Cases = append(s.Cases, Case{
		Name: "npm run build (tsc + vite build)",
		Pass: buildErr == nil,
		Want: "production bundle",
		Got:  boolWord(buildErr == nil, "built", firstLine(buildOut)),
		Note: noteErr(buildErr),
	})

	s.Metrics = append(s.Metrics, Metric{
		Name: "web_npm_install_seconds", Value: round1(installSec), Unit: "s",
	})

	s.Duration = time.Since(t0)
	return s
}

func runIn(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func boolWord(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}
