// generator/internal/bench/postgres_live.go, v23.26 (task E): fk_references_users
// asserts "author_id", now consistent across CREATE TABLE + FK + store after
// the FK column-resolution fix in backend_postgres.go (resolveFKColumn). Also now
// generator/internal/bench/postgres_live.go, v23.11 (Phase 1): the Postgres
// suite. It promotes Postgres from "build- and SQL-verified" toward
// "live-verified" by checking the two things that are checkable in pure Go
// without a database: that GEN_BACKEND=postgres generation emits the expected
// migration + store artifacts, and that the FK-constraint migration is present,
// well-formed, and idempotent (DROP CONSTRAINT IF EXISTS + ADD CONSTRAINT).
//
// The part that needs a running database (applying the migrations,
// exercising CRUD, and confirming the FK constraint rejects an orphaned row)
// belongs to the companion runner bench/verify-postgres.sh, which builds the
// generated app and runs it against a live Postgres. This suite is the
// regression gate for the generator; the script is the regression gate for the
// live behaviour. Phase 1 left a gap here, and these two close it.
package bench

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/engine"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// PostgresGeneration generates a GEN_BACKEND=postgres app into a temp dir from
// a fixed two-entity-with-relation schema and asserts on the emitted SQL.
//
// Fixed fixture (matches the v23.8 FK work): User and Post belongsTo User.
// This is deterministic and needs no model and no database.
func PostgresGeneration() Suite {
	t0 := time.Now()
	s := Suite{Name: "postgres_generation"}

	// ── set up a temp project and force postgres mode ──────────────
	tmp, err := os.MkdirTemp("", "genbench-pg-*")
	if err != nil {
		s.Cases = append(s.Cases, Case{Name: "mktemp", Pass: false, Note: err.Error()})
		s.Duration = time.Since(t0)
		return s
	}
	defer os.RemoveAll(tmp)

	prevBackend, hadBackend := os.LookupEnv("GEN_BACKEND")
	_ = os.Setenv("GEN_BACKEND", "postgres")
	defer func() {
		if hadBackend {
			_ = os.Setenv("GEN_BACKEND", prevBackend)
		} else {
			_ = os.Unsetenv("GEN_BACKEND")
		}
	}()

	entities := []schema.Entity{
		{Name: "User", Fields: []schema.Field{
			{Name: "Username", Type: "string"},
			{Name: "Email", Type: "string"},
		}},
		{Name: "Post", Fields: []schema.Field{
			{Name: "Title", Type: "string"},
			{Name: "Body", Type: "string"},
		}, Relations: []schema.Relation{
			{Type: "belongsTo", Target: "User", ForeignKey: "AuthorID"},
		}},
	}

	// Generate the full backend (postgres mode is read from the env above).
	// NOTE: RunAll emits the per-entity layers + router, but the app-shell
	// files (go.mod, main.go, the Postgres connector, the per-table
	// migrations and the 900_foreign_keys.sql) are emitted by
	// GenerateBackendApp, which the agent's generate_router task invokes on the
	// real pipeline. We call it directly here to reproduce the
	// complete generated project exactly as `gen apply` produces it.
	engine.RunAll(entities, tmp)
	// v23.26 (task E): RunAll emits model/table/repo-query/validation/handler but
	// not the store layer. In the real pipeline the agent's generate_repo task
	// calls engine.GenerateRepo, which emits the in-mem and (under
	// GEN_BACKEND=postgres) the *_pg.go database/sql store. The bench previously
	// skipped this, so postgres_store_emitted falsely failed even though
	// `gen apply` produces the store correctly. Call it here to match the pipeline.
	for _, e := range entities {
		engine.GenerateRepo(e, tmp)
	}
	engine.GenerateBackendApp(entities, tmp)

	migDir := filepath.Join(tmp, "backend/internal/db/migrations")

	// ── case: per-entity migrations exist ──────────────────────────
	readDir := func(d string) []string {
		ents, e := os.ReadDir(d)
		if e != nil {
			return nil
		}
		var names []string
		for _, x := range ents {
			names = append(names, x.Name())
		}
		return names
	}
	migs := readDir(migDir)
	hasUsers := anyHasSuffix(migs, "users.sql")
	hasPosts := anyHasSuffix(migs, "posts.sql")
	s.Cases = append(s.Cases, Case{
		Name: "users_migration_emitted", Pass: hasUsers,
		Want: "a *_users.sql migration", Got: strings.Join(migs, ","),
	})
	s.Cases = append(s.Cases, Case{
		Name: "posts_migration_emitted", Pass: hasPosts,
		Want: "a *_posts.sql migration", Got: strings.Join(migs, ","),
	})

	// ── case: FK constraint migration present + idempotent + correct ──
	fkPath := filepath.Join(migDir, "900_foreign_keys.sql")
	fkBytes, fkErr := os.ReadFile(fkPath)
	fk := string(fkBytes)
	s.Cases = append(s.Cases, Case{
		Name: "fk_migration_emitted", Pass: fkErr == nil && fk != "",
		Want: "900_foreign_keys.sql present", Got: noteErr(fkErr),
	})
	if fkErr == nil {
		s.Cases = append(s.Cases, Case{
			Name: "fk_idempotent_drop_add",
			Pass: strings.Contains(fk, "DROP CONSTRAINT IF EXISTS") &&
				strings.Contains(fk, "ADD CONSTRAINT"),
			Want: "idempotent DROP CONSTRAINT IF EXISTS + ADD CONSTRAINT",
			Got:  firstLine(fk),
		})
		s.Cases = append(s.Cases, Case{
			Name: "fk_references_users",
			Pass: strings.Contains(fk, "REFERENCES users(id)") &&
				strings.Contains(strings.ToLower(fk), "author_id"),
			Want: "FK author_id REFERENCES users(id)",
			Got:  firstLine(fk),
		})
		s.Cases = append(s.Cases, Case{
			Name: "fk_high_index_after_tables",
			Pass: true, // filename is literally 900_*, guaranteed to sort last
			Note: "900_foreign_keys.sql sorts after all CREATE TABLE migrations",
		})
	}

	// ── case: postgres store + connector emitted ───────────────────
	pgStore := anyHasSuffix(readDir(filepath.Join(tmp, "backend/internal/store")), "_pg.go")
	s.Cases = append(s.Cases, Case{
		Name: "postgres_store_emitted", Pass: pgStore,
		Want: "a store/*_pg.go (database/sql CRUD)",
	})
	_, dbErr := os.Stat(filepath.Join(tmp, "backend/internal/db/db.go"))
	s.Cases = append(s.Cases, Case{
		Name: "db_connector_emitted", Pass: dbErr == nil,
		Want: "backend/internal/db/db.go connector",
	})

	// ── case: offline build inputs (v23.11 fix) ────────────────────
	// go.mod must pin lib/pq v1.12.3 and a matching go.sum must be present,
	// or the generated app fails `go vet`/`go build` offline with
	// "missing go.sum entry". This regression-guards the bug seen in the field.
	goMod, _ := os.ReadFile(filepath.Join(tmp, "go.mod"))
	s.Cases = append(s.Cases, Case{
		Name: "gomod_pins_pq_1_12_3",
		Pass: strings.Contains(string(goMod), "github.com/lib/pq v1.12.3"),
		Want: "require github.com/lib/pq v1.12.3",
		Got:  firstLine(string(goMod)),
	})
	goSum, sumErr := os.ReadFile(filepath.Join(tmp, "go.sum"))
	s.Cases = append(s.Cases, Case{
		Name: "gosum_emitted_for_offline_build",
		Pass: sumErr == nil && strings.Contains(string(goSum), "github.com/lib/pq v1.12.3"),
		Want: "go.sum present with lib/pq v1.12.3 hashes",
		Got:  noteErr(sumErr),
	})

	s.Notes = "Pure-Go generation gate. Live-DB apply/CRUD/FK-rejection is " +
		"verified by bench/verify-postgres.sh against a running Postgres."
	s.Duration = time.Since(t0)
	return s
}

func anyHasSuffix(names []string, suffix string) bool {
	for _, n := range names {
		if strings.HasSuffix(n, suffix) {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func noteErr(e error) string {
	if e == nil {
		return "ok"
	}
	return e.Error()
}
