package cli

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/lib/pq"

	"gen-system/generator/internal/compiler"
	"gen-system/generator/internal/compiler/diff"
	"gen-system/generator/internal/compiler/migration"
	"gen-system/generator/internal/schema"
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
		fmt.Println("⚠️  Rollback: using last stored down SQL (if available)")
		// TODO: load from .gen/last_down.sql
		fmt.Println("❌ Automated rollback requires --force re-run or manual down SQL in .gen/last_down.sql")
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
				fmt.Printf("   ⚠️  %s.%s: %s → %s (TYPE CHANGE — data loss risk)\n",
					mod.EntityName, fc.Field, fc.OldType, fc.NewType)
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
