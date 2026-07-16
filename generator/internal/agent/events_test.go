// events_test.go — v25.5: proves the structured agent-event recorder actually
// fans events out (not just that the funcs exist). Covers: emit→lifecycle sink,
// emitHeal→heal sink, multi-sink fan-out, inert-with-no-sink, and JSONL trace
// round-trip. Real behaviour, per the code-quality rule.
package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// captureSink collects events for assertions.
type captureSink struct{ got []AgentEvent }

func (c *captureSink) Record(ev AgentEvent) { c.got = append(c.got, ev) }

func TestRecorder_EmitLifecycle_ReachesSink(t *testing.T) {
	ResetAgentSinks()
	defer ResetAgentSinks()
	c := &captureSink{}
	AddAgentSink(c)

	emit("validate", "  [validate] Round 1/3 — go vet + go build...\n")

	if len(c.got) != 1 {
		t.Fatalf("want 1 event, got %d", len(c.got))
	}
	ev := c.got[0]
	if ev.Kind != KindLifecycle {
		t.Errorf("Kind = %q, want %q", ev.Kind, KindLifecycle)
	}
	if ev.Phase != "validate" {
		t.Errorf("Phase = %q, want validate", ev.Phase)
	}
	if ev.Message == "" {
		t.Error("Message empty — the emit() line was not carried into the event")
	}
	if ev.TS == "" {
		t.Error("TS empty — record() must stamp a timestamp")
	}
}

func TestRecorder_EmitHeal_ReachesSink(t *testing.T) {
	ResetAgentSinks()
	defer ResetAgentSinks()
	c := &captureSink{}
	AddAgentSink(c)

	emitHeal(HealEvent{
		TaskID:   "t-7",
		Func:     "Product.AdjustStock",
		Attempt:  1,
		Max:      2,
		Phase:    "start",
		Strategy: "recompile-with-error",
		Detail:   "repair attempt 1/2",
	})

	if len(c.got) != 1 {
		t.Fatalf("want 1 heal event, got %d", len(c.got))
	}
	ev := c.got[0]
	if ev.Kind != KindHeal {
		t.Errorf("Kind = %q, want %q", ev.Kind, KindHeal)
	}
	if ev.Func != "Product.AdjustStock" || ev.Attempt != 1 || ev.Max != 2 {
		t.Errorf("heal fields not carried: %+v", ev)
	}
	if ev.Message != "repair attempt 1/2" {
		t.Errorf("Message = %q, want the HealEvent.Detail", ev.Message)
	}
}

func TestRecorder_MultiSink_And_Inert(t *testing.T) {
	ResetAgentSinks()
	defer ResetAgentSinks()

	// Inert: no sink → no panic, nothing to observe.
	emit("start", "no sink here\n") // must not panic

	a, b := &captureSink{}, &captureSink{}
	AddAgentSink(a)
	AddAgentSink(b)
	emit("done", "both sinks\n")

	if len(a.got) != 1 || len(b.got) != 1 {
		t.Fatalf("fan-out failed: a=%d b=%d (want 1,1)", len(a.got), len(b.got))
	}
}

func TestJSONLSink_RoundTrip(t *testing.T) {
	ResetAgentSinks()
	defer ResetAgentSinks()

	dir := t.TempDir()
	path := filepath.Join(dir, "trace.jsonl")
	sink, err := NewJSONLSink(path)
	if err != nil {
		t.Fatalf("NewJSONLSink: %v", err)
	}
	AddAgentSink(sink)

	emit("start", "line one\n")
	emitHeal(HealEvent{Func: "X.Y", Phase: "success", Detail: "repaired"})
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var n int
	var kinds []AgentEventKind
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev AgentEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("line %d not valid JSON: %v", n+1, err)
		}
		kinds = append(kinds, ev.Kind)
		n++
	}
	if n != 2 {
		t.Fatalf("want 2 JSONL lines, got %d", n)
	}
	if kinds[0] != KindLifecycle || kinds[1] != KindHeal {
		t.Errorf("kinds = %v, want [lifecycle heal]", kinds)
	}
}

// FuncSink is the Path-B adapter (broker publisher). Prove it forwards.
func TestFuncSink_Forwards(t *testing.T) {
	ResetAgentSinks()
	defer ResetAgentSinks()
	var seen int
	AddAgentSink(FuncSink{Fn: func(AgentEvent) { seen++ }})
	emit("validate", "x\n")
	if seen != 1 {
		t.Fatalf("FuncSink did not forward: seen=%d", seen)
	}
}
