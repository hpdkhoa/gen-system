// ts_export_type_v2658_test.go — v26.58 (D054): "export type { X, Y } from './x'"
// became a NodeTypeAlias named "{" plus two unrecognized_top_level_statement
// nodes. The check for the brace looked at the word "type" itself, so the
// re-export branch never ran.
package ast

import "testing"

func TestTS_ExportTypeReexportBlock(t *testing.T) {
	src := "export type { X, Y } from './x'\nfunction g() {}\n"
	m := parseTSMust(t, src)
	for _, n := range m.Nodes {
		if n.Kind == NodeTypeAlias {
			t.Errorf("unexpected type alias %q from a re-export block", n.Name)
		}
		if n.Kind == NodeUnknown {
			t.Errorf("unexpected unknown node %q (%s)", n.RawSource, n.UnknownReason)
		}
	}
	if fns := m.TopLevelByKind(NodeFunction); len(fns) != 1 || fns[0].Name != "g" {
		t.Errorf("want function g after the re-export block, got %d functions", len(fns))
	}
}

func TestTS_ExportTypeAliasStillParses(t *testing.T) {
	m := parseTSMust(t, "export type Foo = string\n")
	aliases := m.TopLevelByKind(NodeTypeAlias)
	if len(aliases) != 1 || aliases[0].Name != "Foo" || !aliases[0].Exported {
		t.Fatalf("want exported alias Foo, got %+v", aliases)
	}
}
