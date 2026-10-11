// generator/internal/config/config_test.go (v23.21): lock the .env loader
// semantics: real-env precedence, profile selection, quoting/comments, masking.
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnv(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_SetsUnsetVars(t *testing.T) {
	dir := t.TempDir()
	p := writeEnv(t, dir, ".env", "OLLAMA_MODEL_PLAN=qwen3:14b\nGEN_LOGIC=ollama\n")
	os.Unsetenv("OLLAMA_MODEL_PLAN")
	os.Unsetenv("GEN_LOGIC")

	got, err := Load(p)
	if err != nil || got != p {
		t.Fatalf("Load: got=%q err=%v", got, err)
	}
	if v := os.Getenv("OLLAMA_MODEL_PLAN"); v != "qwen3:14b" {
		t.Errorf("plan: got %q", v)
	}
	if v := os.Getenv("GEN_LOGIC"); v != "ollama" {
		t.Errorf("logic: got %q", v)
	}
}

func TestLoad_RealEnvWins(t *testing.T) {
	dir := t.TempDir()
	p := writeEnv(t, dir, ".env", "OLLAMA_MODEL_CODE=from-file\n")
	t.Setenv("OLLAMA_MODEL_CODE", "from-real-env")

	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	// The already-exported value must not be overwritten by the file.
	if v := os.Getenv("OLLAMA_MODEL_CODE"); v != "from-real-env" {
		t.Errorf("real env should win: got %q", v)
	}
}

func TestLoad_QuotingAndComments(t *testing.T) {
	dir := t.TempDir()
	body := `
# a comment line
export OLLAMA_HOST="http://127.0.0.1:11434"
OLLAMA_KEEP_ALIVE='45m'
OLLAMA_TIMEOUT=900   # inline comment stripped on unquoted values
EMPTYLINE_OK=

`
	p := writeEnv(t, dir, ".env", body)
	for _, k := range []string{"OLLAMA_HOST", "OLLAMA_KEEP_ALIVE", "OLLAMA_TIMEOUT", "EMPTYLINE_OK"} {
		os.Unsetenv(k)
	}
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	if v := os.Getenv("OLLAMA_HOST"); v != "http://127.0.0.1:11434" {
		t.Errorf("host quoting: got %q", v)
	}
	if v := os.Getenv("OLLAMA_KEEP_ALIVE"); v != "45m" {
		t.Errorf("keep_alive quoting: got %q", v)
	}
	if v := os.Getenv("OLLAMA_TIMEOUT"); v != "900" {
		t.Errorf("timeout inline-comment strip: got %q", v)
	}
}

func TestResolvePath_Profile(t *testing.T) {
	t.Setenv("GEN_ENV", "staging")
	if got := resolvePath(""); got != ".env.staging" {
		t.Errorf("GEN_ENV=staging should select .env.staging, got %q", got)
	}
	// Explicit path beats GEN_ENV.
	if got := resolvePath("/tmp/custom.env"); got != "/tmp/custom.env" {
		t.Errorf("explicit path should win, got %q", got)
	}
	os.Unsetenv("GEN_ENV")
	if got := resolvePath(""); got != ".env" {
		t.Errorf("default should be .env, got %q", got)
	}
}

func TestLoad_MissingFileIsNotError(t *testing.T) {
	got, err := Load("/no/such/file/.env")
	if err != nil {
		t.Errorf("missing file should not error, got %v", err)
	}
	if got != "" {
		t.Errorf("missing file should report empty path, got %q", got)
	}
}

func TestMaskDSN(t *testing.T) {
	in := "postgres://app:secretpw@db.internal:5432/prod"
	out := maskDSN(in)
	if out == in || !contains(out, "***") || contains(out, "secretpw") {
		t.Errorf("DSN not masked: %q", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
