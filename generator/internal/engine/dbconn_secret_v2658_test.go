// generator/internal/engine/dbconn_secret_v2658_test.go — v26.58 (D028): the
// generated db.Open formatted the ping error with the full DSN, so a database
// that did not answer put the user name and password from DATABASE_URL into the
// process log through log.Fatal. The message now names host and database only.
package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// openErrTest runs inside the generated db package. The sslmode value is
// invalid, so lib/pq fails in Ping before it dials: no network is needed.
const openErrTest = `package db

import (
	"strings"
	"testing"
)

func TestOpenErrorHidesPassword(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://alice:s3cretpw@127.0.0.1:5432/shop?sslmode=bogus")
	_, err := Open()
	if err == nil {
		t.Fatal("Open should fail with an invalid sslmode")
	}
	msg := err.Error()
	if strings.Contains(msg, "s3cretpw") || strings.Contains(msg, "alice") {
		t.Fatalf("Open error shows the credentials: %s", msg)
	}
	if !strings.Contains(msg, "127.0.0.1:5432") || !strings.Contains(msg, "shop") {
		t.Fatalf("Open error should still name host and database: %s", msg)
	}
}
`

func TestDBOpen_ErrorHidesPassword(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	t.Setenv("GEN_BACKEND", "postgres")
	root := t.TempDir()
	generateBackend(t, []schema.Entity{{Name: "Order", Fields: []schema.Field{{Name: "Total", Type: "int"}}}}, root)
	if err := os.WriteFile(filepath.Join(root, "backend/internal/db/open_v2658_test.go"), []byte(openErrTest), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "./backend/internal/db/")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated db test failed: %v\n%s", err, out)
	}
}
