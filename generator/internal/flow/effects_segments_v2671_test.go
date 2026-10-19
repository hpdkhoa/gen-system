// v26.71: effect patterns match whole name segments. The substring rule read
// parent.matrix_world as the ent ORM (database), pos.lerp as the os package
// (files) and magnet.pull as the network.
package flow

import "testing"

func TestEffectPatternsMatchWholeSegments(t *testing.T) {
	for _, tc := range []struct {
		callee   string
		patterns []string
		want     bool
	}{
		{"parent.matrix_world.inverted", dbPatterns, false},
		{"current.copy", dbPatterns, false},
		{"pos.lerp", fsPatterns, false},
		{"magnet.pull", netPatterns, false},
		{"db.Query", dbPatterns, true},
		{"s.db.Exec", dbPatterns, true},
		{"tx.ExecContext", dbWritePatterns, true},
		{"ent.Client", dbPatterns, true},
		{"os.Open", fsPatterns, true},
		{"ioutil.ReadFile", fsPatterns, true},
		{"http.Get", netPatterns, true},
		{"client.Do", netPatterns, true},
	} {
		if got := matchesAny(tc.callee, tc.patterns); got != tc.want {
			t.Errorf("matchesAny(%q) = %v, want %v", tc.callee, got, tc.want)
		}
	}
}
