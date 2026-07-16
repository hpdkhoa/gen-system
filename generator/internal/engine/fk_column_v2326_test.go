// generator/internal/engine/fk_column_v2326_test.go — v23.26 (task E): the FK
// migration column MUST match the column the CREATE TABLE emits and the column
// the pg store INSERTs into. Before the fix, genForeignKeyMigration used
// jsonTag(splitCamel(name)) → "user_id" while genTable used jsonTag(name) →
// "userid", so ALTER TABLE referenced a missing column and the FK silently
// never applied on a live DB. This pins all three to the same name.
package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gen-system/generator/internal/schema"
)

func TestFKColumnMatchesTableColumn(t *testing.T) {
	prev, had := os.LookupEnv("GEN_BACKEND")
	_ = os.Setenv("GEN_BACKEND", "postgres")
	defer func() {
		if had {
			_ = os.Setenv("GEN_BACKEND", prev)
		} else {
			_ = os.Unsetenv("GEN_BACKEND")
		}
	}()

	tmp, err := os.MkdirTemp("", "fkcol-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	entities := []schema.Entity{
		{Name: "User", Fields: []schema.Field{{Name: "Name", Type: "string"}}},
		{Name: "Post", Fields: []schema.Field{{Name: "Title", Type: "string"}},
			Relations: []schema.Relation{{Type: "belongsTo", Target: "User", ForeignKey: "UserID"}}},
	}
	RunAll(entities, tmp)
	for _, e := range entities {
		GenerateRepo(e, tmp)
	}
	GenerateBackendApp(entities, tmp)

	migDir := filepath.Join(tmp, "backend/internal/db/migrations")
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(migDir, name))
		return string(b)
	}
	postsTable := read("002_posts.sql")
	fk := read("900_foreign_keys.sql")
	storeB, _ := os.ReadFile(filepath.Join(tmp, "backend/internal/store/post_pg.go"))
	store := string(storeB)

	// The canonical FK column for "UserID" via the shared helper. We assert all
	// three artifacts agree with it — NOT a hardcoded spelling — so the test
	// stays correct whether the helper yields "user_id" or "userid".
	col := resolveFKColumn(entities[1], "UserID")
	if col == "" {
		t.Fatal("resolveFKColumn returned empty")
	}
	if !strings.Contains(postsTable, col+" INTEGER") {
		t.Errorf("CREATE TABLE posts should define column %q; got:\n%s", col, postsTable)
	}
	if !strings.Contains(fk, "FOREIGN KEY ("+col+")") {
		t.Errorf("FK migration should reference column %q; got:\n%s", col, fk)
	}
	if !strings.Contains(store, col) {
		t.Errorf("pg store should INSERT/SELECT column %q; got:\n%s", col, store)
	}
	// The constraint name is derived from the SAME column, so they must agree.
	if !strings.Contains(fk, "fk_posts_"+col) {
		t.Errorf("FK constraint name should be fk_posts_%s; got:\n%s", col, fk)
	}
}

// TestFKColumnMatches_ExplicitField covers the harder case: the schema declares
// an EXPLICIT foreign-key field (userID) alongside the relation. fieldsForEntity
// dedupes, so the explicit field's column spelling wins in the table + store;
// the FK migration must follow it, not the synthesized splitCamel form.
func TestFKColumnMatches_ExplicitField(t *testing.T) {
	prev, had := os.LookupEnv("GEN_BACKEND")
	_ = os.Setenv("GEN_BACKEND", "postgres")
	defer func() {
		if had {
			_ = os.Setenv("GEN_BACKEND", prev)
		} else {
			_ = os.Unsetenv("GEN_BACKEND")
		}
	}()

	tmp, err := os.MkdirTemp("", "fkcol2-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	entities := []schema.Entity{
		{Name: "User", Fields: []schema.Field{{Name: "Name", Type: "string"}}},
		{Name: "Post", Fields: []schema.Field{
			{Name: "Title", Type: "string"},
			{Name: "userID", Type: "int"}, // EXPLICIT FK field
		}, Relations: []schema.Relation{{Type: "belongsTo", Target: "User", ForeignKey: "userID"}}},
	}
	RunAll(entities, tmp)
	for _, e := range entities {
		GenerateRepo(e, tmp)
	}
	GenerateBackendApp(entities, tmp)

	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(tmp, "backend/internal/db/migrations", name))
		return string(b)
	}
	postsTable := read("002_posts.sql")
	fk := read("900_foreign_keys.sql")
	storeB, _ := os.ReadFile(filepath.Join(tmp, "backend/internal/store/post_pg.go"))
	store := string(storeB)

	col := resolveFKColumn(entities[1], "userID")
	if !strings.Contains(postsTable, col+" INTEGER") {
		t.Errorf("CREATE TABLE should define %q; got:\n%s", col, postsTable)
	}
	if !strings.Contains(fk, "FOREIGN KEY ("+col+")") {
		t.Errorf("FK migration should reference the table's actual column %q; got:\n%s", col, fk)
	}
	if !strings.Contains(store, "(title, "+col+")") {
		t.Errorf("pg store INSERT should use column %q; got:\n%s", col, store)
	}
}
