// http_smoke.go, v25.5 (step 4): optional HTTP liveness smoke inside the loop.
//
// "Fully executable" should mean the generated app serves, beyond the check that
// it compiles and its no-panic contract test passes. When GEN_HTTP_SMOKE=1, after
// the validation loop (and Understand-verify) pass, this:
//  1. builds the backend binary,
//  2. boots it on a free-ish port (GEN_SMOKE_PORT, default 18080),
//  3. polls /health until ready (or times out),
//  4. GETs each discovered collection route and asserts non-000 / non-5xx,
//  5. shuts the server down.
//
// It is advisory: it warns and never aborts. The Go backend + contract test remain
// the hard guarantee. It is opt-in (needs a runnable box; off by default so the
// offline static path is unaffected). Read-only GETs only: it never fires mutating
// business-op POSTs (those need payloads and would change state). A collection
// GET returning 200 is the direct "is it alive and serving" signal.
//
// No new dependencies: route discovery is a regex over generated source; the
// probe uses net/http. Lives in the agent (generation) package; understand-server
// never links it, so the archtest boundary is unaffected.
package agent

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// smokeRouteRe matches a registered collection route literal in generated code:
//
//	mux.HandleFunc("/orders", h.collection)
//
// It deliberately ignores the "/orders/" item form (trailing slash) and the
// health route, which are handled separately.
var smokeRouteRe = regexp.MustCompile(`HandleFunc\("(/[a-z0-9\-]+)"\s*,`)

// discoverSmokeRoutes scans the generated backend for collection GET routes.
// Returns a de-duplicated, sorted list of paths like ["/orders", "/products"].
func discoverSmokeRoutes(backendPath string) []string {
	seen := map[string]bool{}
	_ = filepath.Walk(backendPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for _, m := range smokeRouteRe.FindAllStringSubmatch(string(data), -1) {
			p := m[1]
			if p == "/health" || p == "/" {
				continue
			}
			seen[p] = true
		}
		return nil
	})
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// runHTTPSmoke performs the boot-and-probe. It is a no-op unless GEN_HTTP_SMOKE=1.
// backendPath is <root>/backend. All failures are advisory (emit warn), so this
// never returns an error that would abort generation.
func runHTTPSmoke(backendPath string) {
	if os.Getenv("GEN_HTTP_SMOKE") != "1" {
		return
	}
	if _, err := os.Stat(filepath.Join(backendPath, "go.mod")); err != nil {
		// No standalone module here; the backend may build as part of a parent.
		// Skip quietly rather than guess.
		emit("validate", "  [smoke] (no backend/go.mod; skipping HTTP smoke)\n")
		return
	}

	port := os.Getenv("GEN_SMOKE_PORT")
	if port == "" {
		port = "18080"
	}

	emit("validate", "\n🔥 HTTP smoke (GEN_HTTP_SMOKE=1): build → boot → probe collection routes...\n")

	// 1. Build a throwaway binary.
	bin := filepath.Join(backendPath, ".gen-smoke-bin")
	if out, err := runCmd("go", []string{"build", "-o", bin, "./..."}, backendPath); err != nil {
		// Fall back to building the current dir if ./... produced no main.
		if out2, err2 := runCmd("go", []string{"build", "-o", bin, "."}, backendPath); err2 != nil {
			emit("warn", fmt.Sprintf("  [smoke] ⚠️  could not build backend binary:\n%s\n", tail(out+out2)))
			return
		}
	}
	defer os.Remove(bin)

	// 2. Boot it with the chosen port in the environment.
	cmd := exec.Command(bin)
	cmd.Dir = backendPath
	cmd.Env = append(os.Environ(), "PORT="+port)
	if err := cmd.Start(); err != nil {
		emit("warn", fmt.Sprintf("  [smoke] ⚠️  could not start backend: %v\n", err))
		return
	}
	// Always tear the process down.
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()

	base := "http://localhost:" + port
	client := &http.Client{Timeout: 2 * time.Second}

	// 3. Poll /health for readiness (up to ~10s).
	ready := false
	for i := 0; i < 40; i++ {
		resp, err := client.Get(base + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				ready = true
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !ready {
		emit("warn", "  [smoke] ⚠️  backend did not become ready on /health within 10s; skipping probes\n")
		return
	}
	emit("validate", "  [smoke] ✅ backend booted and answered /health\n")

	// 4. Probe each collection route with a GET.
	routes := discoverSmokeRoutes(backendPath)
	if len(routes) == 0 {
		emit("validate", "  [smoke] (no collection routes discovered to probe)\n")
		return
	}
	var failures []string
	for _, r := range routes {
		resp, err := client.Get(base + r)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s → connection error (%v)", r, err))
			continue
		}
		code := resp.StatusCode
		resp.Body.Close()
		if code >= 500 {
			failures = append(failures, fmt.Sprintf("%s → HTTP %d", r, code))
		} else {
			emit("validate", fmt.Sprintf("  [smoke] ✅ GET %s → HTTP %d\n", r, code))
		}
	}
	if len(failures) > 0 {
		emit("warn", fmt.Sprintf("  [smoke] ⚠️  %d route(s) failed liveness:\n    %s\n",
			len(failures), strings.Join(failures, "\n    ")))
		recordLifecycle("smoke", fmt.Sprintf("%d/%d routes failed liveness", len(failures), len(routes)))
	} else {
		emit("validate", fmt.Sprintf("  [smoke] ✅ all %d collection route(s) alive (no 000/5xx)\n", len(routes)))
		recordLifecycle("smoke", fmt.Sprintf("all %d routes alive", len(routes)))
	}
}
