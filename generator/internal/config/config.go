// generator/internal/config/config.go (v23.21): central runtime configuration.
//
// WHAT: a tiny, dependency-free .env loader plus a place that documents every
// runtime knob. WHY: the product is configured entirely through environment
// variables (OLLAMA_MODEL_PLAN/_CODE, GEN_LOGIC, OLLAMA_HOST, timeouts, DB URL,
// …). Spreading those across shell exports makes per-environment launches
// (dev / staging / production) fragile. This package loads a .env file once at
// startup so a single file fully describes an environment, while preserving the
// rule that a real, already-exported environment variable always wins (so CI or
// an operator can override the file without editing it).
//
// Precedence (highest first):
//  1. A variable already present in the process environment.
//  2. A value in the selected .env file.
//  3. The built-in default (defined at each use site, not here).
//
// Profile selection (which file to load), highest first:
//  1. --env <path>            (CLI flag, handled by the caller via Load(path))
//  2. GEN_ENV=<name>          → loads .env.<name>   (e.g. GEN_ENV=staging)
//  3. ./.env                  (default)
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Load reads a .env file and sets any variables it defines that are not already
// present in the process environment. An explicit path wins; otherwise the file
// is chosen from GEN_ENV (.env.<name>) or defaults to ./.env. A missing file is
// not an error. The product runs fine on real env vars / defaults alone.
//
// Returns the path actually loaded ("" if none) so the caller can report it.
func Load(explicitPath string) (string, error) {
	path := resolvePath(explicitPath)
	if path == "" {
		return "", nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // absent file is fine
		}
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		key, val, ok := parseLine(sc.Text())
		if !ok {
			continue
		}
		// Env precedence: never clobber an already-set variable.
		if _, present := os.LookupEnv(key); present {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return path, fmt.Errorf("%s:%d: set %s: %w", path, lineNo, key, err)
		}
	}
	if err := sc.Err(); err != nil {
		return path, err
	}
	return path, nil
}

// resolvePath picks which env file to load. See profile-selection order above.
func resolvePath(explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	if name := strings.TrimSpace(os.Getenv("GEN_ENV")); name != "" {
		// GEN_ENV=staging → .env.staging
		return ".env." + name
	}
	return ".env"
}

// parseLine parses one .env line into (key, value, ok). Supports:
//   - blank lines and # comments (ignored)
//   - optional leading "export "
//   - KEY=VALUE with optional surrounding single/double quotes on VALUE
//   - inline trailing comments only when value is unquoted
func parseLine(raw string) (string, string, bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	eq := strings.IndexByte(line, '=')
	if eq <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:eq])
	val := strings.TrimSpace(line[eq+1:])
	if key == "" {
		return "", "", false
	}

	if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
		// Quoted: take content verbatim (no inline-comment stripping).
		val = val[1 : len(val)-1]
	} else {
		// Unquoted: strip an inline comment if present.
		if i := strings.IndexByte(val, '#'); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
	}
	return key, val, true
}

// ── Documented knobs (read at their use sites via os.Getenv) ───────────────
// This list is the single reference for what a .env may contain. The constants
// are the variable names, so callers and tests share one spelling.
const (
	EnvLogic         = "GEN_LOGIC"         // "ollama" → live model logic; else templates
	EnvModel         = "OLLAMA_MODEL"      // legacy single model (fallback for both roles)
	EnvModelPlan     = "OLLAMA_MODEL_PLAN" // planner model (analysis/architecture/entities)
	EnvModelCode     = "OLLAMA_MODEL_CODE" // code model (service-logic bodies)
	EnvOllamaHost    = "OLLAMA_HOST"       // e.g. http://127.0.0.1:11434
	EnvOllamaTimeout = "OLLAMA_TIMEOUT"    // seconds; per-request HTTP timeout
	EnvKeepAlive     = "OLLAMA_KEEP_ALIVE" // e.g. "30m"; how long Ollama keeps a model warm
	EnvBackend       = "GEN_BACKEND"       // "postgres" → generate PG store; else in-memory
	EnvDBURL         = "DATABASE_URL"      // postgres DSN (DB_URL accepted as alias)
	EnvProfile       = "GEN_ENV"           // selects .env.<name>
	EnvOutDir        = "GEN_OUT"           // optional default output dir for generators
)

// Summary returns a redacted, human-readable view of the active config for
// startup logging. Secrets (DSN) are masked.
func Summary() string {
	get := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	dsn := os.Getenv(EnvDBURL)
	if dsn == "" {
		dsn = os.Getenv("DB_URL")
	}
	if dsn != "" {
		dsn = maskDSN(dsn)
	} else {
		dsn = "(unset; in-memory)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "logic=%s ", get(EnvLogic, "templates"))
	// v23.35: default display matches install.sh (qwen split), not deepseek.
	fmt.Fprintf(&b, "plan=%s ", get(EnvModelPlan, get(EnvModel, "qwen3:14b")))
	fmt.Fprintf(&b, "code=%s ", get(EnvModelCode, get(EnvModel, "qwen2.5-coder:14b")))
	fmt.Fprintf(&b, "host=%s ", get(EnvOllamaHost, "http://localhost:11434"))
	fmt.Fprintf(&b, "timeout=%ss ", get(EnvOllamaTimeout, "600"))
	fmt.Fprintf(&b, "keep_alive=%s ", get(EnvKeepAlive, "30m"))
	fmt.Fprintf(&b, "db=%s", dsn)
	return b.String()
}

func maskDSN(dsn string) string {
	// postgres://user:pass@host/db → postgres://user:***@host/db
	at := strings.IndexByte(dsn, '@')
	col := strings.LastIndexByte(dsn[:atOr0(at)], ':')
	if at > 0 && col > 0 && col < at {
		return dsn[:col+1] + "***" + dsn[at:]
	}
	return dsn
}

func atOr0(i int) int {
	if i < 0 {
		return 0
	}
	return i
}

// LoadedFrom is set by Load via the caller for diagnostics; kept simple as a
// package var the CLI can print. (Optional convenience.)
var LoadedFrom string

// MustLoad is a convenience wrapper used by main(): it loads the env file,
// records the path, and never fails hard on a missing file.
func MustLoad(explicitPath string) {
	p, err := Load(explicitPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  config: %v\n", err)
		return
	}
	LoadedFrom = p
	if p != "" {
		abs, _ := filepath.Abs(p)
		fmt.Printf("⚙️  Loaded config from %s\n", abs)
	}
}
