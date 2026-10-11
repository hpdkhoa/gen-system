// java_params_v2658_test.go (v26.58, F4): parseParamList looped forever when
// a parameter held a token it did not expect, such as the "=" of a default
// value "void f(int x = 1)". A loop round consumed nothing, so Parse never
// returned. The parse runs in a goroutine with a timeout (parseJavaWithin), so
// a regression fails instead of hanging the suite.
package ast

import (
	"testing"
	"time"
)

func TestJava_ParamDefaultValueDoesNotHang(t *testing.T) {
	src := "class C { void f(int x = 1) {} void g() {} }\nclass After {}\n"
	m := parseJavaWithin(t, src, 3*time.Second)
	f := findNode(m.Nodes, NodeMethod, "f")
	if f == nil {
		t.Fatal("method f not found")
	}
	if len(f.Params) != 1 || f.Params[0].Name != "x" || f.Params[0].Type.Name != "int" {
		t.Errorf("want one parameter x of type int, got %+v", f.Params)
	}
	if findNode(m.Nodes, NodeMethod, "g") == nil {
		t.Error("method g after f not found")
	}
	if findNode(m.Nodes, NodeClass, "After") == nil {
		t.Error("class After not found")
	}
}
