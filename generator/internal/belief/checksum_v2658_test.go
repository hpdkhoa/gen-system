// D018: the belief checksum kept only the belief count and the first 16 bytes of
// the joined text. The first sorted ID alone fills those bytes, so a changed value
// left the checksum equal and DiffSince returned nil.
package belief

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

func TestD018_ChecksumSeesValueChange(t *testing.T) {
	bs := NewStore(t.TempDir())
	bs.RebuildFromIndex(funcIndex("SaveOrder", []ast.Parameter{{Name: "id", Type: ast.TypeRef{Name: "int"}}}), nil)
	before := bs.CurrentChecksum()

	bs.RebuildFromIndex(funcIndex("SaveOrder", []ast.Parameter{{Name: "total", Type: ast.TypeRef{Name: "float64"}}}), nil)
	after := bs.CurrentChecksum()

	if before == after {
		t.Fatalf("checksum did not change after the params changed: %q", after)
	}
	if d := bs.DiffSince(before); len(d) == 0 {
		t.Fatal("DiffSince returned no change after the params changed")
	}
	// The same beliefs give the same checksum.
	bs.RebuildFromIndex(funcIndex("SaveOrder", []ast.Parameter{{Name: "total", Type: ast.TypeRef{Name: "float64"}}}), nil)
	if again := bs.CurrentChecksum(); again != after {
		t.Fatalf("checksum not stable: %q then %q", after, again)
	}
}
