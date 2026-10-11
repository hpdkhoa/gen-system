package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStderr swaps the global logger to write text into a buffer
// and returns the buffer. Tests can then inspect what was logged.
func captureStderr(t *testing.T, lvl Level) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	if _, err := Init(Options{Level: lvl, Stderr: buf}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = Close() })
	return buf
}

func TestLevelString(t *testing.T) {
	cases := map[Level]string{
		LevelTrace: "TRACE",
		LevelInfo:  "INFO",
		LevelError: "ERROR",
	}
	for l, want := range cases {
		if got := l.String(); got != want {
			t.Errorf("Level(%d).String() = %q, want %q", l, got, want)
		}
	}
}

func TestEnabled(t *testing.T) {
	captureStderr(t, LevelInfo)
	if Enabled(LevelTrace) {
		t.Error("trace should be disabled at LevelInfo")
	}
	if !Enabled(LevelInfo) {
		t.Error("info should be enabled at LevelInfo")
	}
	if !Enabled(LevelError) {
		t.Error("error should always be enabled")
	}

	SetLevel(LevelTrace)
	if !Enabled(LevelTrace) {
		t.Error("trace should be enabled after SetLevel(LevelTrace)")
	}
}

func TestInfoFiltersOutTrace(t *testing.T) {
	buf := captureStderr(t, LevelInfo)
	Trace(context.Background(), "trace-msg", "k", "v")
	Info(context.Background(), "info-msg", "k", "v")

	out := buf.String()
	if strings.Contains(out, "trace-msg") {
		t.Errorf("trace message leaked through Info filter:\n%s", out)
	}
	if !strings.Contains(out, "info-msg") {
		t.Errorf("info message missing:\n%s", out)
	}
}

func TestTraceLevelLetsAllThrough(t *testing.T) {
	buf := captureStderr(t, LevelTrace)
	Trace(context.Background(), "trace-msg")
	Info(context.Background(), "info-msg")
	Error(context.Background(), "error-msg")

	out := buf.String()
	for _, want := range []string{"trace-msg", "info-msg", "error-msg"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestErrorAlwaysEmitted(t *testing.T) {
	// Even with a hypothetical level above Error, real-world callers
	// will only ever set Info or Trace, but verify Error is emitted
	// at Info level (the default).
	buf := captureStderr(t, LevelInfo)
	Error(context.Background(), "boom", "err", "fake")
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("error not emitted at info level:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "ERROR") {
		t.Errorf("ERROR label missing:\n%s", buf.String())
	}
}

func TestPhasePropagation(t *testing.T) {
	buf := captureStderr(t, LevelInfo)
	ctx := WithPhase(context.Background(), "parse")
	Info(ctx, "began")

	out := buf.String()
	if !strings.Contains(out, "phase=parse") {
		t.Errorf("phase attribute missing:\n%s", out)
	}
}

func TestPhaseEmptyWhenNotSet(t *testing.T) {
	buf := captureStderr(t, LevelInfo)
	Info(context.Background(), "no-phase")

	if strings.Contains(buf.String(), "phase=") {
		t.Errorf("phase= appeared when not set:\n%s", buf.String())
	}
}

func TestPhaseNestedOverride(t *testing.T) {
	// Inner WithPhase overrides outer.
	buf := captureStderr(t, LevelInfo)
	outer := WithPhase(context.Background(), "load")
	inner := WithPhase(outer, "index")
	Info(inner, "nested")

	out := buf.String()
	if !strings.Contains(out, "phase=index") {
		t.Errorf("inner phase did not override outer:\n%s", out)
	}
	if strings.Contains(out, "phase=load") {
		t.Errorf("outer phase leaked through inner:\n%s", out)
	}
}

func TestTraceFileWritesJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sub", "trace.log") // exercise mkdir
	stderrBuf := &bytes.Buffer{}

	f, err := Init(Options{
		Level:     LevelInfo,
		TraceFile: path,
		Stderr:    stderrBuf,
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if f == nil {
		t.Fatal("expected non-nil file handle")
	}
	t.Cleanup(func() { _ = Close() })

	ctx := WithPhase(context.Background(), "test")
	Info(ctx, "hello", "count", 7)

	// Force flush. Close() will sync.
	if err := Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace file: %v", err)
	}
	// One JSON object per line; should parse.
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("want 1 trace line, got %d:\n%s", len(lines), data)
	}
	var ev map[string]any
	if err := json.Unmarshal(lines[0], &ev); err != nil {
		t.Fatalf("JSON parse: %v\nline: %s", err, lines[0])
	}
	if ev["msg"] != "hello" {
		t.Errorf("msg = %v, want hello", ev["msg"])
	}
	if ev["phase"] != "test" {
		t.Errorf("phase = %v, want test", ev["phase"])
	}
	if ev["count"].(float64) != 7 {
		t.Errorf("count = %v, want 7", ev["count"])
	}
}

func TestTraceFileWritesAlsoToStderr(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "trace.log")
	stderrBuf := &bytes.Buffer{}

	if _, err := Init(Options{
		Level:     LevelInfo,
		TraceFile: path,
		Stderr:    stderrBuf,
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = Close() })

	Info(context.Background(), "fanout-check")

	if !strings.Contains(stderrBuf.String(), "fanout-check") {
		t.Errorf("stderr did not receive message:\n%s", stderrBuf.String())
	}
}

func TestTimerEmitsDuration(t *testing.T) {
	buf := captureStderr(t, LevelInfo)
	timer := Start(context.Background(), "fake-op")
	timer.End("files", 3)

	out := buf.String()
	if !strings.Contains(out, "op=fake-op") {
		t.Errorf("op attribute missing:\n%s", out)
	}
	if !strings.Contains(out, "duration_ms=") {
		t.Errorf("duration_ms attribute missing:\n%s", out)
	}
	if !strings.Contains(out, "files=3") {
		t.Errorf("extra attribute missing:\n%s", out)
	}
}

func TestTimerNilSafe(t *testing.T) {
	// Defensive: t.End() must not panic if t is nil.
	var timer *Timer
	timer.End() // should be a no-op
}

func TestInitReplacesPreviousFile(t *testing.T) {
	tmp := t.TempDir()
	first := filepath.Join(tmp, "a.log")
	second := filepath.Join(tmp, "b.log")

	if _, err := Init(Options{Level: LevelInfo, TraceFile: first, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatalf("Init first: %v", err)
	}
	Info(context.Background(), "first-msg")

	// Re-init to second file. The first should be closed cleanly.
	if _, err := Init(Options{Level: LevelInfo, TraceFile: second, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatalf("Init second: %v", err)
	}
	Info(context.Background(), "second-msg")
	if err := Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	firstData, _ := os.ReadFile(first)
	secondData, _ := os.ReadFile(second)
	if !strings.Contains(string(firstData), "first-msg") {
		t.Errorf("first file missing first-msg:\n%s", firstData)
	}
	if strings.Contains(string(firstData), "second-msg") {
		t.Errorf("first file leaked second-msg:\n%s", firstData)
	}
	if !strings.Contains(string(secondData), "second-msg") {
		t.Errorf("second file missing second-msg:\n%s", secondData)
	}
}

func TestOddAttrsDoNotPanic(t *testing.T) {
	// If a caller passes an odd number of attrs we shouldn't crash.
	captureStderr(t, LevelInfo)
	Info(context.Background(), "odd", "key-with-no-value")
}

func TestNonStringKeyDoesNotPanic(t *testing.T) {
	captureStderr(t, LevelInfo)
	Info(context.Background(), "bad-key", 42, "v")
}
