// events.go — v25.5 (self-healing observability, step 1 / foundation).
//
// WHAT & WHY. Until now the agent surfaced progress two ways: plain fmt.Print
// (via emit) and the HealObserver callback (via emitHeal). Neither was
// structured or persistable, so there was no way to (a) show repairs live in the
// Studio over SSE, or (b) capture a machine-readable repair trace for the
// fine-tune pipeline (open item #6, explicitly blocked on repair-trace data).
//
// This file adds ONE structured event type (AgentEvent) and a package-level
// Recorder that fans an event out to any number of registered sinks. It changes
// no behaviour on its own: with no sinks registered it is inert, and emit/
// emitHeal keep their existing fmt.Print + HealObserver paths untouched. A CLI
// run opts in a JSONL file sink (Path A); the Studio opts in a broker sink
// (Path B) — the agent code is identical for both.
//
// BOUNDARY SAFETY. Everything here is package-level state and plain funcs, NOT a
// constructor dependency. understand-server never links the agent package, so
// the archtest boundary (TestUnderstandServerHasNoGenDeps) is unaffected. Sinks
// must not block; the JSONL sink serialises under its own mutex.
package agent

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// AgentEventKind classifies a structured agent event. It unifies the two
// pre-existing channels: lifecycle/validation lines (previously only emit's
// free-text "phase") and self-healing steps (previously only HealEvent).
type AgentEventKind string

const (
	// KindLifecycle covers start/validate/warn/done free-text milestones — the
	// same strings emit() already prints, now also structured.
	KindLifecycle AgentEventKind = "lifecycle"
	// KindHeal covers one step of the self-healing loop (mirrors HealEvent).
	KindHeal AgentEventKind = "heal"
)

// AgentEvent is the single structured record a sink receives. It is a superset
// of HealEvent so a heal step and a lifecycle line share one wire shape; fields
// not relevant to a kind are left zero. Marshalled to JSON for the JSONL trace
// and for the SSE payload, so json tags are stable wire contract.
type AgentEvent struct {
	Kind AgentEventKind `json:"kind"`
	// TS is an RFC3339Nano timestamp set at record time.
	TS string `json:"ts"`
	// Phase is the lifecycle phase ("start"|"validate"|"warn"|"done") for
	// KindLifecycle, or the heal phase ("start"|"verify"|"success"|"fail"|
	// "exhausted") for KindHeal.
	Phase string `json:"phase,omitempty"`
	// Message is the human-readable line (KindLifecycle) or a short detail
	// (KindHeal, copied from HealEvent.Detail).
	Message string `json:"message,omitempty"`

	// ── KindHeal fields (zero for lifecycle) ──────────────────
	TaskID   string `json:"task_id,omitempty"`
	Func     string `json:"func,omitempty"`
	Attempt  int    `json:"attempt,omitempty"`
	Max      int    `json:"max,omitempty"`
	Strategy string `json:"strategy,omitempty"`
}

// AgentSink receives structured events. Implementations MUST NOT block; a slow
// or hung sink would stall generation. The JSONL and broker sinks both return
// immediately (buffered / best-effort).
type AgentSink interface {
	Record(AgentEvent)
}

// ─── Recorder: package-level fan-out ──────────────────────────

var (
	recorderMu sync.RWMutex
	sinks      []AgentSink
)

// AddAgentSink registers a sink to receive every subsequent AgentEvent. Safe to
// call from CLI setup (JSONL) or server setup (broker). Multiple sinks compose:
// a run can write a JSONL trace AND publish to the Studio at once.
func AddAgentSink(s AgentSink) {
	if s == nil {
		return
	}
	recorderMu.Lock()
	sinks = append(sinks, s)
	recorderMu.Unlock()
}

// ResetAgentSinks removes all sinks. Used by tests and between server runs.
func ResetAgentSinks() {
	recorderMu.Lock()
	sinks = nil
	recorderMu.Unlock()
}

// record fans one event out to every sink. Cheap and non-blocking when there are
// no sinks (the default), so the hot path of a plain CLI run is unaffected.
func record(ev AgentEvent) {
	recorderMu.RLock()
	if len(sinks) == 0 {
		recorderMu.RUnlock()
		return
	}
	// Copy the slice header under the read lock, then release before calling
	// out so a sink can't deadlock against AddAgentSink.
	local := make([]AgentSink, len(sinks))
	copy(local, sinks)
	recorderMu.RUnlock()
	for _, s := range local {
		s.Record(ev)
	}
}

// recordLifecycle is the structured counterpart to emit(). emit() calls it after
// its existing fmt.Print + EventHook so the free-text path is unchanged.
func recordLifecycle(phase, message string) {
	record(AgentEvent{
		Kind:    KindLifecycle,
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		Phase:   phase,
		Message: message,
	})
}

// recordHeal is the structured counterpart to emitHeal(). emitHeal() calls it
// after invoking HealObserver so the existing callback path is unchanged.
func recordHeal(ev HealEvent) {
	record(AgentEvent{
		Kind:     KindHeal,
		TS:       time.Now().UTC().Format(time.RFC3339Nano),
		Phase:    ev.Phase,
		Message:  ev.Detail,
		TaskID:   ev.TaskID,
		Func:     ev.Func,
		Attempt:  ev.Attempt,
		Max:      ev.Max,
		Strategy: ev.Strategy,
	})
}

// ─── JSONL file sink (Path A: CLI repair trace) ───────────────

// JSONLSink appends one JSON object per line to an open file. This is the
// repair-trace capture the fine-tune pipeline needs, and the CLI's --heal-log
// artifact. Concurrency-safe; the caller owns Close().
type JSONLSink struct {
	mu  sync.Mutex
	f   *os.File
	enc *json.Encoder
}

// NewJSONLSink creates (or truncates) a JSONL trace file at path and returns a
// sink writing to it. The caller registers it via AddAgentSink and closes it
// when the run ends.
func NewJSONLSink(path string) (*JSONLSink, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return &JSONLSink{f: f, enc: enc}, nil
}

// Record writes one event as a JSON line. Errors are swallowed deliberately: a
// trace-write failure must never abort generation. Non-blocking in practice
// (buffered file write under a short-held mutex).
func (s *JSONLSink) Record(ev AgentEvent) {
	s.mu.Lock()
	_ = s.enc.Encode(ev)
	s.mu.Unlock()
}

// Close flushes and closes the underlying file.
func (s *JSONLSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}

// ─── Function sink (Path B: broker publisher, and tests) ──────

// FuncSink adapts a plain callback into an AgentSink. The Studio wiring uses it
// to publish each event to the SSE broker without the agent package importing
// the api/sse package (which would be a needless coupling — the server passes a
// closure). Tests use it to capture events.
type FuncSink struct{ Fn func(AgentEvent) }

// Record forwards to the wrapped function when set.
func (s FuncSink) Record(ev AgentEvent) {
	if s.Fn != nil {
		s.Fn(ev)
	}
}
