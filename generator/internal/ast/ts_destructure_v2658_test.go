// ts_destructure_v2658_test.go (v26.58, D055): a destructuring declaration
// without semicolon swallowed every later statement up to the next semicolon.
// parseVarDecl skipped with skipUntil(tsSemicolon, tsEOF), and peek drops
// newlines, so the line end never stopped the skip. Array patterns were not
// skipped at all, since skipBraces only handles "{".
package ast

import "testing"

func TestTS_DestructuringWithoutSemicolonKeepsNextStatements(t *testing.T) {
	for _, pat := range []string{"[a, b]", "{a, b}"} {
		src := "const " + pat + " = foo()\nfunction g() {}\nconst z = 1;\nfunction h() {}\n"
		m := parseTSMust(t, src)
		names := map[string]bool{}
		for _, fn := range m.TopLevelByKind(NodeFunction) {
			names[fn.Name] = true
		}
		if !names["g"] || !names["h"] {
			t.Errorf("%s: want functions g and h, got %v", pat, names)
		}
		unknown := 0
		for _, n := range m.Nodes {
			if n.Kind == NodeUnknown && n.UnknownReason == "destructuring_declaration" {
				unknown++
			}
		}
		if unknown != 1 {
			t.Errorf("%s: want 1 destructuring node, got %d", pat, unknown)
		}
		found := false
		for _, n := range m.Nodes {
			if n.Name == "z" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: const z was swallowed", pat)
		}
	}
}
