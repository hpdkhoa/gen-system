// java_record_v2658_test.go — v26.58 (D058): parseRecordComponents looped
// forever when a component held a token it did not expect, such as the dots
// of a varargs component "record R(int... xs) {}" or a stray "=". A loop round
// consumed nothing, so Parse never returned. Each parse here runs in a
// goroutine with a timeout, so a regression fails instead of hanging the suite.
package ast

import (
	"testing"
	"time"
)

func parseJavaWithin(t *testing.T, src string, limit time.Duration) *Module {
	t.Helper()
	done := make(chan *Module, 1)
	go func() {
		m, _ := (&JavaFrontend{}).Parse("R.java", []byte(src))
		done <- m
	}()
	select {
	case m := <-done:
		if m == nil {
			t.Fatalf("Parse returned no module for %q", src)
		}
		return m
	case <-time.After(limit):
		t.Fatalf("Parse did not return within %v for %q", limit, src)
		return nil
	}
}

func TestJava_RecordVarargsComponentDoesNotHang(t *testing.T) {
	m := parseJavaWithin(t, "record R(int... xs) {}\n", 3*time.Second)
	r := findNode(m.Nodes, NodeClass, "R")
	if r == nil {
		t.Fatal("record R not found")
	}
	if len(r.Fields) != 1 || r.Fields[0].Name != "xs" || r.Fields[0].Type.Name != "int" {
		t.Errorf("want one component xs of type int, got %+v", r.Fields)
	}
}

func TestJava_RecordUnknownTokenDoesNotHang(t *testing.T) {
	m := parseJavaWithin(t, "record R(int x = 1, String y) {}\nclass After {}\n", 3*time.Second)
	if findNode(m.Nodes, NodeClass, "R") == nil {
		t.Error("record R not found")
	}
	if findNode(m.Nodes, NodeClass, "After") == nil {
		t.Error("class After after the record not found")
	}
}
