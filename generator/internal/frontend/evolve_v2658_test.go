// evolve_v2658_test.go: D031. EvolveForm and EvolveList read
// frontend/src/components/<Name>Form.jsx and <Name>List.jsx, but when the file
// was missing they called GenerateForm and GenerateList, which write
// frontend/<lower>Form.jsx and frontend/<lower>List.jsx. No run ever created
// the file they read, so marker evolution never started.
package frontend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func TestEvolve_MissingFileCreatesComponentWithMarkers(t *testing.T) {
	root := t.TempDir()
	e := schema.Entity{Name: "BlogPost", Fields: []schema.Field{{Name: "title", Type: "string"}}}

	cases := []struct {
		evolve func(schema.Entity, string)
		file   string
		block  string
	}{
		{EvolveForm, "BlogPostForm.jsx", "FieldInputs"},
		{EvolveList, "BlogPostList.jsx", "TableColumns"},
	}
	for _, c := range cases {
		p := filepath.Join(root, "frontend", "src", "components", c.file)
		c.evolve(e, root)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("first run did not create %s: %v", c.file, err)
		}
		if !strings.Contains(string(b), fmt.Sprintf(markerStart, c.block)) {
			t.Fatalf("%s has no start marker:\n%s", c.file, b)
		}

		// A second run with a new field must update the block in place.
		e2 := e
		e2.Fields = append(append([]schema.Field{}, e.Fields...), schema.Field{Name: "summary", Type: "string"})
		c.evolve(e2, root)
		b, _ = os.ReadFile(p)
		if !strings.Contains(string(b), "summary") {
			t.Fatalf("second run did not add the new field to %s:\n%s", c.file, b)
		}
	}
}
