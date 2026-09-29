// typescript_v2658_test.go: D033. GenerateTypeScript wrote one import line and
// one navigation property per relation. An entity with two belongsTo relations
// to the same target (author_id and editor_id, both User) got the line
// import type { User } from './User' two times, so tsc rejected the file, and
// only one of the two related objects could be typed.
package frontend

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestTypeScript_TwoRelationsToOneTarget(t *testing.T) {
	root := t.TempDir()
	e := schema.Entity{
		Name:   "BlogPost",
		Fields: []schema.Field{{Name: "title", Type: "string"}},
		Relations: []schema.Relation{
			{Type: "belongsTo", Target: "User", ForeignKey: "author_id"},
			{Type: "belongsTo", Target: "User", ForeignKey: "editor_id"},
		},
	}
	GenerateTypeScript(e, root)
	b, err := os.ReadFile(filepath.Join(root, "frontend", "src", "types", "BlogPost.ts"))
	if err != nil {
		t.Fatal(err)
	}
	ts := string(b)

	if n := strings.Count(ts, "import type { User } from './User';"); n != 1 {
		t.Errorf("import of User appears %d times, want 1:\n%s", n, ts)
	}
	props := map[string]int{}
	for _, m := range regexp.MustCompile(`(?m)^\s+'?([A-Za-z0-9_]+)'?\??:`).FindAllStringSubmatch(ts, -1) {
		props[m[1]]++
	}
	for p, n := range props {
		if n > 1 {
			t.Errorf("property %s declared %d times:\n%s", p, n, ts)
		}
	}
	// Each relation gets its own navigation property, named after its key.
	for _, want := range []string{"author?: User;", "editor?: User;"} {
		if !strings.Contains(ts, want) {
			t.Errorf("missing navigation property %q:\n%s", want, ts)
		}
	}
}

// A single relation to a target keeps the old property name.
func TestTypeScript_SingleRelationKeepsTargetName(t *testing.T) {
	root := t.TempDir()
	e := schema.Entity{
		Name:      "BlogPost",
		Relations: []schema.Relation{{Type: "belongsTo", Target: "User", ForeignKey: "author_id"}},
	}
	GenerateTypeScript(e, root)
	b, _ := os.ReadFile(filepath.Join(root, "frontend", "src", "types", "BlogPost.ts"))
	if !strings.Contains(string(b), "  user?: User;") {
		t.Errorf("single relation lost the property user:\n%s", b)
	}
}
