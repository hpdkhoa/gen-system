// v26.71: Python methods were indexed twice. The extractor reported every def
// as a module function, and the class added a name-only method stub, so the
// index held func:run (with the body, the CFG and the call edges) and
// method:A.run (line 0, no edges, no CFG). Search could land on the stub. Two
// helpers named err in different functions also shared the ID func:err.
package flow

import (
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

func TestPythonMethodsAreOneSymbolWithTheirCFG(t *testing.T) {
	fe := &ast.PyFrontend{}
	if !fe.Available() {
		t.Skip("python3/python not found in PATH")
	}
	src := `
class A:
    def run(self, x):
        if x > 0:
            self.helper()
        return x

    def helper(self):
        pass

class B:
    def run(self):
        for i in range(3):
            print(i)

def first():
    def err():
        return 1
    return err()

def second():
    def err():
        return 2
    return err()

def third():
    def add(x):
        return x
    return add(1)

def fourth():
    seen = set()
    seen.add(1)
    return seen

def add(x):
    return x
`
	const file = "pkg/mod.py"
	m, err := fe.Parse(file, []byte(src))
	if err != nil || len(m.ParseErrors) > 0 {
		t.Fatalf("parse: %v %v", err, m.ParseErrors)
	}
	idx := symbols.NewIndex()
	idx.AddModule(m)
	idx.ResolveAll()
	cfgs := Build(m, idx)

	for _, qn := range []string{"A.run", "A.helper", "B.run"} {
		id := symbols.SymbolID(file, symbols.SymMethod, qn)
		s, ok := idx.ByID(id)
		if !ok {
			t.Errorf("method %s missing", qn)
			continue
		}
		if s.Span.Start.Line == 0 {
			t.Errorf("method %s has line 0 (a body-less stub)", qn)
		}
		if _, ok := cfgs[id]; !ok {
			t.Errorf("method %s has no CFG", qn)
		}
	}
	if cfg := cfgs[symbols.SymbolID(file, symbols.SymMethod, "A.run")]; cfg == nil || len(cfg.Blocks) < 2 {
		t.Errorf("A.run: want a branching CFG, got %+v", cfg)
	}
	// no module function stands in for a method
	for _, name := range []string{"run", "helper"} {
		if _, ok := idx.ByID(symbols.SymbolID(file, symbols.SymFunc, name)); ok {
			t.Errorf("a module function %q duplicates the method", name)
		}
	}
	// A.run's call to self.helper still resolves, now to the method
	var found bool
	for _, e := range idx.AllCallEdges() {
		if e.CallerID == symbols.SymbolID(file, symbols.SymMethod, "A.run") && e.CalleeName == "self.helper" {
			found = e.CalleeID == symbols.SymbolID(file, symbols.SymMethod, "A.helper")
		}
	}
	if !found {
		t.Error("A.run -> self.helper should resolve to method A.helper")
	}
	// self is the receiver, not an input
	if s, ok := idx.ByID(symbols.SymbolID(file, symbols.SymMethod, "A.run")); ok {
		if len(s.Params) != 1 || s.Params[0].Name != "x" {
			t.Errorf("A.run params %+v, want only x", s.Params)
		}
	}
	// a nested helper resolves from its own function, never from a receiver call
	edgeTo := func(caller, callee string) (string, bool) {
		for _, e := range idx.AllCallEdges() {
			if e.CallerID == symbols.SymbolID(file, symbols.SymFunc, caller) && e.CalleeName == callee {
				return e.CalleeID, true
			}
		}
		return "", false
	}
	if id, ok := edgeTo("first", "err"); !ok || id != symbols.SymbolID(file, symbols.SymFunc, "first.err") {
		t.Errorf("first -> err resolved to %q, want first.err", id)
	}
	if id, ok := edgeTo("fourth", "seen.add"); !ok || id != "" {
		t.Errorf("fourth -> seen.add resolved to %q, want unresolved (neither third.add nor the module's add is an attribute)", id)
	}
	// each nested helper keeps its own ID and CFG
	for _, qn := range []string{"first.err", "second.err"} {
		id := symbols.SymbolID(file, symbols.SymFunc, qn)
		s, ok := idx.ByID(id)
		if !ok {
			t.Errorf("nested helper %s missing", qn)
			continue
		}
		if s.Name != "err" {
			t.Errorf("nested helper %s: name %q, want err", qn, s.Name)
		}
		if _, ok := cfgs[id]; !ok {
			t.Errorf("nested helper %s has no CFG", qn)
		}
	}
}

// v26.71: a call through a Python import resolves in the imported module's
// file: store.load() after `from pkg import store`, Timeline.build() after
// `from pkg.timeline import Timeline`. A module outside the project stays
// unresolved and counts as an external call.
func TestPythonCallsThroughImportsResolveInTheModule(t *testing.T) {
	fe := &ast.PyFrontend{}
	if !fe.Available() {
		t.Skip("python3/python not found in PATH")
	}
	files := map[string]string{
		"proj/pkg/store.py":    "def load(p):\n    return p\n",
		"proj/pkg/timeline.py": "class Timeline:\n    def build(self):\n        return 1\n",
		"proj/pkg/other.py":    "def load(p):\n    return None\n",
		"proj/pkg/app.py": `from pkg import store
from pkg.timeline import Timeline
import numpy as np

def run():
    store.load(1)
    Timeline.build()
    np.zeros(3)
`,
	}
	idx := symbols.NewIndex()
	for path, src := range files {
		m, err := fe.Parse(path, []byte(src))
		if err != nil || len(m.ParseErrors) > 0 {
			t.Fatalf("%s: %v %v", path, err, m.ParseErrors)
		}
		idx.AddModule(m)
	}
	idx.ResolveAll()
	run := symbols.SymbolID("proj/pkg/app.py", symbols.SymFunc, "run")
	want := map[string]string{
		"store.load":     symbols.SymbolID("proj/pkg/store.py", symbols.SymFunc, "load"),
		"Timeline.build": symbols.SymbolID("proj/pkg/timeline.py", symbols.SymMethod, "Timeline.build"),
		"np.zeros":       "",
	}
	classifier := idx.NewEdgeClassifier()
	for _, e := range idx.AllCallEdges() {
		if e.CallerID != run {
			continue
		}
		w, ok := want[e.CalleeName]
		if !ok {
			continue
		}
		if e.CalleeID != w {
			t.Errorf("%s resolved to %q, want %q", e.CalleeName, e.CalleeID, w)
		}
		class, _ := classifier.Classify(e)
		if (w == "") != (class != symbols.EdgeInternal) {
			t.Errorf("%s classified %v", e.CalleeName, class)
		}
		delete(want, e.CalleeName)
	}
	for k := range want {
		t.Errorf("no edge for %s", k)
	}
}
