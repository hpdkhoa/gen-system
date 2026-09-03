// java_frontend_test.go — v23.12 (Phase 4): exercises the Java 17 frontend on a
// Spring controller, a record, a switch expression, control flow, and the
// hard-cases-to-NodeUnknown discipline (lambdas). Also checks that method calls
// become resolvable call edges through the symbol index.
package ast

import (
	"strings"
	"testing"
)

func parseJava(t *testing.T, src string) *Module {
	t.Helper()
	fe := &JavaFrontend{}
	if !fe.Available() {
		t.Fatal("JavaFrontend.Available() should be true (pure Go)")
	}
	m, err := fe.Parse("Demo.java", []byte(src))
	if err != nil {
		t.Fatalf("Parse returned unrecoverable error: %v", err)
	}
	if m.Language != LangJava {
		t.Errorf("module language = %q, want java", m.Language)
	}
	return m
}

// findNode walks the module (top-level + nested children) for the first node
// matching kind and (optional) name.
func findNode(nodes []*Node, kind NodeKind, name string) *Node {
	for _, n := range nodes {
		// A NodeCall carries the invoked identifier in Callee, not Name (Name is
		// empty for calls). Matching only on Name meant every lookup for a call
		// silently found nothing, which made the golden-IR assertions below fail
		// against a frontend that was in fact emitting the correct tree.
		if n.Kind == kind && (name == "" || n.Name == name || n.Callee == name) {
			return n
		}
		if got := findNode(n.Children, kind, name); got != nil {
			return got
		}
		if got := findNode(n.Else, kind, name); got != nil {
			return got
		}
	}
	return nil
}

func countKind(nodes []*Node, kind NodeKind) int {
	c := 0
	for _, n := range nodes {
		if n.Kind == kind {
			c++
		}
		c += countKind(n.Children, kind)
		c += countKind(n.Else, kind)
	}
	return c
}

func TestJava_SpringControllerStructure(t *testing.T) {
	src := `
package com.example.api;

import org.springframework.web.bind.annotation.*;
import java.util.List;

@RestController
@RequestMapping("/orders")
public class OrderController {

    private final OrderService service;

    @Autowired
    public OrderController(OrderService service) {
        this.service = service;
    }

    @GetMapping
    public List<Order> list() {
        return service.findAll();
    }

    @PostMapping
    public Order create(@RequestBody Order o) {
        if (o.getTotal() <= 0) {
            throw new IllegalArgumentException("total");
        } else {
            validate(o);
        }
        return service.save(o);
    }
}
`
	m := parseJava(t, src)

	if m.PackageName != "com.example.api" {
		t.Errorf("package = %q, want com.example.api", m.PackageName)
	}
	cls := findNode(m.Nodes, NodeClass, "OrderController")
	if cls == nil {
		t.Fatal("OrderController class not found")
	}
	if !cls.Exported {
		t.Error("OrderController should be Exported (public)")
	}
	if !strings.Contains(cls.DocComment, "@RestController") {
		t.Errorf("class DocComment should capture @RestController annotation, got %q", cls.DocComment)
	}

	create := findNode(m.Nodes, NodeMethod, "create")
	if create == nil {
		t.Fatal("create method not found")
	}
	if create.Receiver == nil || create.Receiver.Name != "OrderController" {
		t.Errorf("create.Receiver should be OrderController, got %+v", create.Receiver)
	}
	// create contains an if/else → a NodeBranch with a populated Else.
	br := findNode(create.Children, NodeBranch, "")
	if br == nil {
		t.Fatal("expected a NodeBranch (if) inside create")
	}
	if len(br.Else) == 0 {
		t.Error("if/else should populate the branch Else arm")
	}
	// the method's calls (validate, service.save) become NodeCall children.
	if countKind(create.Children, NodeCall) == 0 {
		t.Error("create should contain NodeCall children for its invocations")
	}
}

func TestJava_CallNodesCarryCalleeForResolution(t *testing.T) {
	// The ast package cannot import symbols (symbols imports ast), so this test
	// verifies the IR *shape* the symbol index relies on: a NodeMethod with a
	// Receiver, whose body holds NodeCall children with a Callee name. Actual
	// CalleeName→CalleeID resolution is covered in the symbols package.
	src := `
package p;
public class Svc {
    public int run() {
        return helper();
    }
    private int helper() {
        return 1;
    }
}
`
	m := parseJava(t, src)
	run := findNode(m.Nodes, NodeMethod, "run")
	if run == nil {
		t.Fatal("run method not found")
	}
	if run.Receiver == nil || run.Receiver.Name != "Svc" {
		t.Errorf("method Receiver must be set for SymMethod ID derivation, got %+v", run.Receiver)
	}
	var callee string
	for _, c := range allCalls(run.Children) {
		if c.Callee == "helper" {
			callee = c.Callee
		}
	}
	if callee != "helper" {
		t.Error("run() should contain a NodeCall with Callee=helper for the call graph")
	}
	// helper is defined as a method, so the name is resolvable in-file.
	if findNode(m.Nodes, NodeMethod, "helper") == nil {
		t.Error("helper method declaration should be present for resolution")
	}
}

func allCalls(nodes []*Node) []*Node {
	var out []*Node
	for _, n := range nodes {
		if n.Kind == NodeCall {
			out = append(out, n)
		}
		out = append(out, allCalls(n.Children)...)
		out = append(out, allCalls(n.Else)...)
	}
	return out
}

func TestJava_RecordAndSwitchExpression(t *testing.T) {
	src := `
package p;
public record Point(int x, int y) {}

class Shapes {
    String describe(Point pt) {
        return switch (pt.x()) {
            case 0 -> "origin-x";
            default -> compute(pt);
        };
    }
}
`
	m := parseJava(t, src)
	// record → a class named Point with two component Fields.
	pt := findNode(m.Nodes, NodeClass, "Point")
	if pt == nil {
		t.Fatal("record Point not parsed as a class")
	}
	if len(pt.Fields) != 2 {
		t.Errorf("Point should have 2 record components as Fields, got %d", len(pt.Fields))
	}
	// switch expression → a NodeBranch named "switch" inside describe.
	describe := findNode(m.Nodes, NodeMethod, "describe")
	if describe == nil {
		t.Fatal("describe method not found")
	}
	if findNode(describe.Children, NodeBranch, "switch") == nil {
		t.Error("switch expression should emit a NodeBranch")
	}
}

func TestJava_LambdaPushedToUnknown(t *testing.T) {
	src := `
package p;
import java.util.List;
class L {
    void go(List<String> xs) {
        xs.forEach(x -> process(x));
    }
}
`
	m := parseJava(t, src)
	goM := findNode(m.Nodes, NodeMethod, "go")
	if goM == nil {
		t.Fatal("go method not found")
	}
	if countKind(goM.Children, NodeUnknown) == 0 {
		t.Error("a statement containing a lambda should yield a NodeUnknown (first-class ignorance)")
	}
	// but the call inside the lambda should still be harvested.
	if countKind(goM.Children, NodeCall) == 0 {
		t.Error("calls inside a lambda statement should still be harvested for the call graph")
	}
}

func TestJava_TextBlockDoesNotBreakBraces(t *testing.T) {
	// A text block containing braces and quotes must not desync brace tracking;
	// the method after it must still be found.
	src := `
package p;
class T {
    String q() {
        String s = """
            { "not": "real braces } " }
            """;
        return s;
    }
    int after() { return helper(); }
}
`
	m := parseJava(t, src)
	if findNode(m.Nodes, NodeMethod, "after") == nil {
		t.Error("method after the text block was lost — brace tracking desynced on the text block")
	}
}

// v24 — a method REFERENCE (::) passed as an argument to a normal call must not
// push the whole statement to NodeUnknown; the outer call must still be
// harvested. Regression for the "stock.merge(...) missing" bug where
// Integer::sum caused the merge call to vanish into an Unknown node.
func TestJava_MethodReferenceArgDoesNotHideCall_v24(t *testing.T) {
	src := `
package p;
import java.util.Map;
class Inventory {
    void add(String sku, int qty) {
        stock.merge(sku, qty, Integer::sum);
    }
}
`
	m := parseJava(t, src)
	addM := findNode(m.Nodes, NodeMethod, "add")
	if addM == nil {
		t.Fatal("add method not found")
	}
	// The statement must NOT collapse to Unknown just because it contains '::'.
	if countKind(addM.Children, NodeUnknown) != 0 {
		t.Error("a bare method reference argument (Integer::sum) should not push the statement to NodeUnknown")
	}
	// The real call must be harvested for the call graph.
	var found bool
	for _, c := range addM.Children {
		if c.Kind == NodeCall && c.Callee == "merge" {
			found = true
		}
	}
	if !found {
		t.Error("stock.merge(...) should be harvested as a NodeCall (callee 'merge')")
	}
}

// v24 — if/for/while conditions must be captured into RawSource so the CFG can
// render a real decision/loop label instead of the literal word "decision".
func TestJava_BranchAndLoopConditionCaptured_v24(t *testing.T) {
	src := `
package p;
class C {
    int totalUnits() {
        int total = 0;
        for (int q : stock.values()) {
            total += q;
        }
        if (total < 0) { total = 0; }
        return total;
    }
}
`
	m := parseJava(t, src)
	fn := findNode(m.Nodes, NodeMethod, "totalUnits")
	if fn == nil {
		t.Fatal("totalUnits not found")
	}
	var loop, branch *Node
	for _, c := range fn.Children {
		if c.Kind == NodeLoop {
			loop = c
		}
		if c.Kind == NodeBranch {
			branch = c
		}
	}
	if loop == nil || loop.RawSource == "" {
		t.Errorf("loop condition (RawSource) not captured, got %+v", loop)
	} else if !strings.Contains(loop.RawSource, "stock.values") {
		t.Errorf("loop condition should mention stock.values, got %q", loop.RawSource)
	}
	if branch == nil || branch.RawSource == "" {
		t.Errorf("if condition (RawSource) not captured, got %+v", branch)
	} else if !strings.Contains(branch.RawSource, "total") {
		t.Errorf("if condition should mention total, got %q", branch.RawSource)
	}
}

// v24 — a try/catch must produce a try body AND a catch arm (in Else) so the CFG
// builder can model it as flow, not a lone "try {" node.
func TestJava_TryCatchArms_v24(t *testing.T) {
	src := `
package p;
class C {
    void run() {
        try {
            svc.place(order);
            log("ok");
        } catch (OutOfStockException e) {
            log("failed");
        }
    }
}
`
	m := parseJava(t, src)
	fn := findNode(m.Nodes, NodeMethod, "run")
	if fn == nil {
		t.Fatal("run not found")
	}
	var try *Node
	for _, c := range fn.Children {
		if c.Kind == NodeTry {
			try = c
		}
	}
	if try == nil {
		t.Fatal("no NodeTry emitted")
	}
	// try body (Children) should contain the place/log calls; catch (Else) the
	// failure-path call.
	if len(try.Children) == 0 {
		t.Error("try body (Children) is empty; body calls were not captured")
	}
	if len(try.Else) == 0 {
		t.Error("catch arm (Else) is empty; catch body was not captured as the exception path")
	}
}

// v24 — a plain assignment statement with no call (e.g. a constructor's
// "this.inv = inv;") must still emit a node so the CFG shows the body instead of
// collapsing to Start→End.
func TestJava_AssignmentStatementEmitted_v24(t *testing.T) {
	src := `
package p;
class OrderService {
    OrderService(Inventory inv, PaymentProcessor pp) {
        this.inv = inv;
        this.pp = pp;
    }
}
`
	m := parseJava(t, src)
	ctor := findNode(m.Nodes, NodeMethod, "OrderService")
	if ctor == nil {
		t.Fatal("constructor not found")
	}
	assigns := 0
	for _, c := range ctor.Children {
		if c.Kind == NodeAssign {
			assigns++
		}
	}
	if assigns < 2 {
		t.Errorf("expected the two field assignments to emit NodeAssign nodes, got %d", assigns)
	}
}
