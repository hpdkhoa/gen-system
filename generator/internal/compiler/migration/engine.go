package migration

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// -----------------------------
// Ensure migration table exists
// -----------------------------
func EnsureMigrationTable(db *sql.DB) error {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		checksum TEXT,
		applied_at TIMESTAMP
	)
	`)
	return err
}

// -----------------------------
// checksum (NEW)
// -----------------------------
func checksum(sqls []string) string {
	h := sha1.New()
	for _, s := range sqls {
		h.Write([]byte(s))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// -----------------------------
// Check if migration applied
// -----------------------------
func AlreadyApplied(db *sql.DB, version string) bool {
	var v string
	err := db.QueryRow(
		"SELECT version FROM schema_migrations WHERE version=$1",
		version,
	).Scan(&v)

	return err == nil
}

// -----------------------------
// Apply SQL safely (transaction)
// -----------------------------
func ApplySQL(db *sql.DB, version string, queries []string, dryRun bool) error {

	if dryRun {
		fmt.Println("🧪 DRY RUN MODE:")
		for _, q := range queries {
			fmt.Println(q)
		}
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	for _, q := range queries {
		if _, err := tx.Exec(q); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed SQL: %s | %w", q, err)
		}
	}

	// insert with checksum
	_, err = tx.Exec(
		"INSERT INTO schema_migrations(version, checksum, applied_at) VALUES($1, $2, $3)",
		version,
		checksum(queries),
		time.Now(),
	)

	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// -----------------------------
// Rollback (basic safe mode)
// -----------------------------
func RollbackLast(db *sql.DB, downSQL []string, version string, dryRun bool) error {

	if dryRun {
		fmt.Println("🧪 DRY RUN ROLLBACK:")
		for _, q := range downSQL {
			fmt.Println(q)
		}
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	for _, q := range downSQL {
		if _, err := tx.Exec(q); err != nil {
			tx.Rollback()
			return err
		}
	}

	_, err = tx.Exec(
		"DELETE FROM schema_migrations WHERE version=$1",
		version,
	)

	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}
