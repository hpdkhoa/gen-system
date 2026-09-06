// go_callee_test.go — v26.34: callee names come from the shape of the call,
// not from its source text. A function literal called in place is named
// FuncLiteralCallee; a chained call keeps its receiver chain without the
// argument lists; a generic instantiation drops the type arguments.
package ast

import (
	"sort"
	"strings"
	"testing"
)

func collectCallees(nodes []*Node, out map[string]int) {
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if n.Kind == NodeCall || n.Kind == NodeDefer {
			out[n.Callee]++
		}
		collectCallees(n.Children, out)
		collectCallees(n.Else, out)
	}
}

func TestGoCalleeNamesByShape(t *testing.T) {
	src := `package p

import (
	"encoding/json"
	"net/http"
	"sync"
)

type S struct{ mu sync.Mutex }

func (s *S) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = json.NewDecoder(r.Body).Decode(nil)
	w.Header().Set("a", "b")
	func() { helper() }()
	helper()
	g[int](1)
	_ = []byte("x")
}

func helper() {}

func g[T any](x T) {}
`
	m, err := goFE().Parse("p.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	collectCallees(m.Nodes, got)
	for _, want := range []string{
		"s.mu.Lock", "s.mu.Unlock", "json.NewDecoder", "json.NewDecoder().Decode",
		"w.Header().Set", FuncLiteralCallee, "helper", "g",
	} {
		if got[want] == 0 {
			var keys []string
			for k := range got {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			t.Errorf("callee %q not found; callees seen: %s", want, strings.Join(keys, " | "))
		}
	}
	for k := range got {
		if strings.Contains(k, "\n") || strings.Contains(k, "{") {
			t.Errorf("callee name carries source text: %q", k)
		}
	}
}
