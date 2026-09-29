// D022: parseContinueArgs skipped a flag value only for --review-dir and --env,
// so the value of --heal-log, --out, --style, --style-mode, --stdlib and
// --verify-understand became part of the idea text or even the path.
package cli

import "testing"

func TestParseContinueArgs_ValueFlagsSkipped(t *testing.T) {
	cases := []struct {
		args       []string
		path, idea string
	}{
		{[]string{"./app", "add tags", "--heal-log", "trace.jsonl"}, "./app", "add tags"},
		{[]string{"--heal-log", "trace.jsonl", "./app", "add tags"}, "./app", "add tags"},
		{[]string{"./app", "add tags", "--verify-understand", "stop"}, "./app", "add tags"},
		{[]string{"./app", "add tags", "--verify-understand", "--apply"}, "./app", "add tags"},
		{[]string{"./app", "--out", "o", "add tags", "--style", "s.json", "--style-mode", "strict", "--stdlib", "off"}, "./app", "add tags"},
		{[]string{"./app", "add tags", "--review-dir", "rd", "--env", "e"}, "./app", "add tags"},
	}
	for _, c := range cases {
		path, idea, ok := parseContinueArgs(c.args)
		if !ok || path != c.path || idea != c.idea {
			t.Errorf("parseContinueArgs(%q) = (%q, %q, %v), want (%q, %q, true)", c.args, path, idea, ok, c.path, c.idea)
		}
	}
}
