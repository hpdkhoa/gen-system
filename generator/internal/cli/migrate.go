package cli

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"github.com/hpdkhoa/gen-system/generator/internal/compiler"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler/diff"
	"github.com/hpdkhoa/gen-system/generator/internal/compiler/migration"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// Usage:
//
//	gen migrate <schema.json> [--dry] [--force] [--rollback]
func RunMigrate(args []string) {

	if len(args) == 0 {
		fmt.Println("Usage: gen migrate <schema.json> [--dry] [--force] [--rollback]")
		return
	}

	dryRun := false
	force := false
	rollback := false

	for _, a := range args {
		switch a {
		case "--dry":
			dryRun = true
		case "--force":
			force = true
		case "rollback", "--rollback":
			rollback = true
		}
	}

	if rollback {
		runRollback(dryRun)
		return
	}

	schemaPath := args[0]
	// v26.58 (D023): schemaPath[0] panicked on an empty first argument (an
	// unset shell variable). An empty path gets the usage text; HasPrefix is
	// safe on "".
	if strings.TrimSpace(schemaPath) == "" {
		fmt.Println("Usage: gen migrate <schema.json> [--dry] [--force] [--rollback]")
		return
	}
	if strings.HasPrefix(schemaPath, "-") {
		fmt.Println("❌ First arg must be schema path, not a flag")
		return
	}

	// 1. Load schema
	s, err := schema.LoadFromFile(schemaPath)
	if err != nil {
		fmt.Println("❌ failed to load schema:", err)
		return
	}

	// 2. Compile → IR
	newIR, err := compiler.Compile(s)
	if err != nil {
		fmt.Println("❌ compile error:", err)
		return
	}

	// 3. Load previous IR
	// v26.58 (D038): the error was dropped, so a corrupt snapshot counted as
	// empty, every entity looked new and ADD COLUMN steps were lost (then SaveIR
	// replaced the evidence). Stop and name the file so the user can fix it.
	oldIR, err := compiler.LoadPreviousIR()
	if err != nil {
		fmt.Println("❌ cannot read the IR snapshot .gen/ir_snapshot.json:", err)
		fmt.Println("   repair or restore the file, then run gen migrate again")
		return
	}

	// 4. Diff
	cs := diff.Diff(
		compiler.ToDiffSchema(oldIR),
		compiler.ToDiffSchema(newIR),
	)

	// ✅ FIX 7: MIGRATION LINT. A safety gate on breaking changes.
	if migration.HasBreakingChanges(cs) && !force {
		fmt.Println("")
		fmt.Println("🚨 BREAKING CHANGES DETECTED:")
		for _, mod := range cs.Modified {
			for _, fc := range mod.Modified {
				if !fc.Breaking {
					continue
				}
				fmt.Printf("   ⚠️  %s.%s: %s → %s (TYPE CHANGE: data loss risk)\n",
					mod.EntityName, fc.Field, fc.OldType, fc.NewType)
			}
			for _, f := range mod.Removed { // v26.57 (D005)
				fmt.Printf("   ❌  DROP COLUMN %s.%s (irreversible)\n", mod.EntityName, f.Name)
			}
		}
		for _, e := range cs.RemoveEntities {
			fmt.Printf("   ❌  DROP TABLE %s (irreversible)\n", e.Name)
		}
		fmt.Println("")
		fmt.Println("Re-run with --force to proceed, or --dry to preview SQL.")
		fmt.Println("Always backup your database before applying breaking changes.")
		return
	}

	// 5. Build migration IR + SQL
	mig := migration.BuildMigration(cs)
	sqls := migration.GenerateSQL(mig)
	downSQLs := migration.GenerateRollbackSQL(mig)

	if len(sqls) == 0 {
		fmt.Println("✔ No migrations needed; the schema is up to date")
		return
	}

	// 6. DB connection
	conn := os.Getenv("DB_URL")
	if conn == "" {
		fmt.Println("❌ DB_URL not set")
		fmt.Println("   export DB_URL=\"postgres://postgres:postgres@localhost:5432/app?sslmode=disable\"")
		return
	}

	db, err := sql.Open("postgres", conn)
	if err != nil {
		fmt.Println("DB error:", err)
		return
	}
	defer db.Close()

	if err = migration.EnsureMigrationTable(db); err != nil {
		fmt.Println("Migration table error:", err)
		return
	}

	version := time.Now().Format("20060102150405")

	if migration.AlreadyApplied(db, version) {
		fmt.Println("✔ already applied:", version)
		return
	}

	// 7. Apply SQL
	err = migration.ApplySQL(db, version, sqls, dryRun)
	if err != nil {
		fmt.Println("❌ migration failed:", err)
		return
	}

	if !dryRun {
		if err := compiler.SaveIR(newIR); err != nil {
			fmt.Println("⚠️  failed to save IR snapshot:", err)
		}
		fmt.Printf("✅ Migration %s applied: %d statements\n", version, len(sqls))
		// v26.58 (D039): the down SQL was written before the DB_URL check and
		// before ApplySQL, with both errors dropped, so a dry or failed run
		// replaced the down SQL of the migration really applied. runRollback
		// relies on this file, so write it only here, after a real apply, and say
		// "saved" only when the write worked.
		if err := saveLastDown(downSQLs); err != nil {
			fmt.Println("⚠️  failed to save down SQL to .gen/last_down.sql:", err)
		} else {
			fmt.Println("   Down SQL saved to .gen/last_down.sql")
		}
	}
}

// saveLastDown writes the down SQL of an applied migration to
// .gen/last_down.sql for `gen migrate --rollback`. v26.58 (D039).
func saveLastDown(downSQLs []string) error {
	downContent := ""
	for _, q := range downSQLs {
		downContent += q + "\n"
	}
	if err := os.MkdirAll(".gen", 0755); err != nil {
		return err
	}
	return os.WriteFile(".gen/last_down.sql", []byte(downContent), 0644)
}

// runRollback applies the down SQL that the last `gen migrate` run saved to
// .gen/last_down.sql, through the same migration table as the up path. It is
// recorded under a "rollback-<timestamp>" version so it shows up in the history.
// --dry prints the statements and applies nothing.
func runRollback(dryRun bool) {
	const downPath = ".gen/last_down.sql"
	raw, err := os.ReadFile(downPath)
	if err != nil {
		fmt.Println("❌ no down SQL found at", downPath)
		fmt.Println("   a down file is written by every applied `gen migrate` run; nothing to roll back")
		return
	}
	var stmts []string
	for _, part := range strings.Split(string(raw), ";") {
		if q := strings.TrimSpace(part); q != "" {
			stmts = append(stmts, q+";")
		}
	}
	if len(stmts) == 0 {
		fmt.Println("✔ down SQL is empty, nothing to roll back")
		return
	}
	fmt.Printf("↩️  Rollback: %d statements from %s\n", len(stmts), downPath)

	conn := os.Getenv("DB_URL")
	if conn == "" {
		fmt.Println("❌ DB_URL not set")
		fmt.Println("   export DB_URL=\"postgres://postgres:postgres@localhost:5432/app?sslmode=disable\"")
		return
	}
	db, err := sql.Open("postgres", conn)
	if err != nil {
		fmt.Println("DB error:", err)
		return
	}
	defer db.Close()
	if err = migration.EnsureMigrationTable(db); err != nil {
		fmt.Println("Migration table error:", err)
		return
	}
	version := "rollback-" + time.Now().Format("20060102150405")
	if err = migration.ApplySQL(db, version, stmts, dryRun); err != nil {
		fmt.Println("❌ rollback failed:", err)
		return
	}
	if !dryRun {
		fmt.Printf("✅ Rollback %s applied, %d statements\n", version, len(stmts))
		fmt.Println("   the IR snapshot was not changed; run `gen migrate <schema.json>` to re-apply forward")
	}
}
