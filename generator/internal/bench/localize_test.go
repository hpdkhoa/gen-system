// localize_test.go (v26.31): the localization suite must be exact on a tiny
// synthetic project. a.go defines Seed which calls Helper in b.go; c.go is
// unrelated. Seeding on "Seed" with gold {b.go} must give recall 0 at hop 0,
// recall 1 at hop 1, and must never pull c.go into the neighbourhood.
package bench

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLocalizeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"a.go": "package main\n\nfunc Seed() { Helper() }\n",
		"b.go": "package main\n\nfunc Helper() {}\n",
		"c.go": "package main\n\nfunc Unrelated() {}\n",
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func metricOf(s Suite, name string) (float64, bool) {
	for _, m := range s.Metrics {
		if m.Name == name {
			return m.Value, true
		}
	}
	return 0, false
}

func TestLocalizationRecall_ReachesGoldAtHopOne(t *testing.T) {
	root := writeLocalizeFixture(t)
	s := LocalizationRecall(root, []string{"Seed"}, []string{"b.go"}, 2)

	want := map[string]float64{
		"seeds_matched":            1,
		"gold_files_total":         1,
		"hop0_file_recall":         0, // only a.go is reached at hop 0
		"hop0_neighbourhood_files": 1,
		"hop1_file_recall":         1, // b.go reached through the Seed → Helper edge
		"hop1_neighbourhood_files": 2, // a.go and b.go, never c.go
		"hop2_file_recall":         1,
		"hop2_neighbourhood_files": 2,
	}
	for name, exp := range want {
		got, ok := metricOf(s, name)
		if !ok {
			t.Fatalf("metric %q missing", name)
		}
		if got != exp {
			t.Errorf("%s = %v, want %v", name, got, exp)
		}
	}
	for _, c := range s.Cases {
		if !c.Pass {
			t.Errorf("case %q failed: got=%q want=%q", c.Name, c.Got, c.Want)
		}
	}
}

func TestLocalizationRecall_NoSeedMatchIsAFailedCaseNotAGuess(t *testing.T) {
	root := writeLocalizeFixture(t)
	s := LocalizationRecall(root, []string{"DoesNotExist"}, []string{"b.go"}, 2)

	if v, _ := metricOf(s, "seeds_matched"); v != 0 {
		t.Fatalf("seeds_matched = %v, want 0", v)
	}
	if v, _ := metricOf(s, "hop2_file_recall"); v != 0 {
		t.Fatalf("recall with no seeds = %v, want 0 (nothing may be guessed)", v)
	}
	failed := 0
	for _, c := range s.Cases {
		if !c.Pass {
			failed++
		}
	}
	if failed == 0 {
		t.Fatal("expected the seeds_matched and gold_reached cases to fail")
	}
}
