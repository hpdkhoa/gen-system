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
	if schemaPath[0] == '-' {
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
	oldIR, _ := compiler.LoadPreviousIR()

	// 4. Diff
	cs := diff.Diff(
		compiler.ToDiffSchema(oldIR),
		compiler.ToDiffSchema(newIR),
	)

	// ✅ FIX 7: MIGRATION LINT — safety gate on breaking changes
	if migration.HasBreakingChanges(cs) && !force {
		fmt.Println("")
		fmt.Println("🚨 BREAKING CHANGES DETECTED:")
		for _, mod := range cs.Modified {
			for _, fc := range mod.Modified {
				if !fc.Breaking {
					continue
				}
				fmt.Printf("   ⚠️  %s.%s: %s → %s (TYPE CHANGE — data loss risk)\n",
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
		fmt.Println("✔ No migrations needed — schema is up to date")
		return
	}

	// Save down SQL for rollback reference
	downContent := ""
	for _, q := range downSQLs {
		downContent += q + "\n"
	}
	_ = os.MkdirAll(".gen", 0755)
	_ = os.WriteFile(".gen/last_down.sql", []byte(downContent), 0644)

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
		fmt.Printf("✅ Migration %s applied — %d statements\n", version, len(sqls))
		fmt.Println("   Down SQL saved to .gen/last_down.sql")
	}
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
