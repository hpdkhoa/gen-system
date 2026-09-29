// D090: builtinSkip used strings.HasPrefix with no separator test, so vendor.go,
// output.go, outputs/x.go and .github/x.go were skipped as if they sat in a
// blocked folder. The watcher never saw edits to those files.
package watch

import "testing"

func TestD090_BuiltinSkipMatchesWholeNames(t *testing.T) {
	keep := []string{"vendor.go", "output.go", "outputs/x.go", "output_writer.go", "vendors/a.go", ".github/x.go"}
	for _, rel := range keep {
		if builtinSkip(rel) {
			t.Errorf("builtinSkip(%q) = true, want false", rel)
		}
	}
	skip := []string{"vendor/a.go", "output/x.go", ".git/hooks/x.go", "node_modules/p/i.ts", ".gen/snapshots/1/main.go", ".gen/entities/user.go"}
	for _, rel := range skip {
		if !builtinSkip(rel) {
			t.Errorf("builtinSkip(%q) = false, want true", rel)
		}
	}
}
