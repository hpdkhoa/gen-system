// Package trace is gen-system's structured logging facade.
//
// Phase A delivery. Built on log/slog (Go stdlib, no new deps).
//
// Three levels, deliberate:
//   - LevelTrace: verbose engine internals (per-symbol, per-block, per-cache-hit).
//     Off by default. Enabled with --trace or trace.SetLevel(LevelTrace).
//   - LevelInfo:  phase boundaries and meaningful state transitions.
//     On by default. Sparse: a normal run produces 20-40 lines.
//   - LevelError: recoverable failures with full context. Always on.
//
// No WARN level, because people ignore warnings. Recoverable+notable becomes
// Info with outcome=degraded; broken becomes Error.
//
// Output:
//   - stderr always, human-readable text format.
//   - .gen/trace.log when --trace is set, JSON format, one event per line.
//   - Never network. Hard constraint.
//
// Phase context: callers attach the current phase (load/parse/index/cfg/heal/...)
// via WithPhase(ctx, "phase-name"). Every log call inside that ctx auto-tags
// with phase=<name>, letting humans grep one phase out of a noisy run.
//
// Errors are logged AND returned.
package trace

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// ─── Levels ───────────────────────────────────────────────────

// Level is a log severity. Internally maps to slog.Level but
// exposes only the three we use.
type Level int

const (
	// LevelTrace is the most verbose level. Per-item events.
	LevelTrace Level = -8
	// LevelInfo is the default. Phase boundaries and state changes.
	LevelInfo Level = 0
	// LevelError is for recoverable failures. Always emitted.
	LevelError Level = 8
)

func (l Level) String() string {
	switch l {
	case LevelTrace:
		return "TRACE"
	case LevelInfo:
		return "INFO"
	case LevelError:
		return "ERROR"
	default:
		return fmt.Sprintf("L%d", int(l))
	}
}

func (l Level) slogLevel() slog.Level {
	return slog.Level(int(l))
}

// ─── Global state ─────────────────────────────────────────────

// Single global logger. Logging is process-wide concern; multiple
// loggers create confusion about which one is "the" log.
var (
	mu      sync.RWMutex
	current *slog.Logger
	curLvl  atomic.Int64 // current minimum level as int64(slog.Level)
	curFile *os.File     // backing file for trace.log if open, for Close()
)

func init() {
	// Default: Info to stderr, no file.
	curLvl.Store(int64(LevelInfo.slogLevel()))
	current = newTextLogger(os.Stderr, LevelInfo)
}

// Options configures the global logger.
type Options struct {
	// Level is the minimum level emitted. Defaults to LevelInfo.
	Level Level

	// TraceFile, if non-empty, opens (or creates) the file at this
	// path and writes JSON events to it in addition to stderr.
	// Caller should defer trace.Close() to flush.
	TraceFile string

	// Stderr is the destination for human-readable output.
	// Defaults to os.Stderr.
	Stderr io.Writer
}

// Init applies Options and replaces the global logger.
// Safe to call multiple times (each call replaces).
// Returns the file handle if TraceFile was opened, so callers can
// inspect it; the package also tracks it internally for Close().
func Init(opts Options) (*os.File, error) {
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}

	var f *os.File
	if opts.TraceFile != "" {
		if err := os.MkdirAll(filepath.Dir(opts.TraceFile), 0o755); err != nil {
			return nil, fmt.Errorf("trace: mkdir %s: %w", filepath.Dir(opts.TraceFile), err)
		}
		var err error
		f, err = os.OpenFile(opts.TraceFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, fmt.Errorf("trace: open %s: %w", opts.TraceFile, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()

	// Close previous file if any.
	if curFile != nil {
		_ = curFile.Close()
	}
	curFile = f

	curLvl.Store(int64(opts.Level.slogLevel()))

	if f != nil {
		current = newMultiLogger(opts.Stderr, f, opts.Level)
	} else {
		current = newTextLogger(opts.Stderr, opts.Level)
	}
	return f, nil
}

// Close flushes and closes any open trace file.
// Safe to call even if no file is open.
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if curFile == nil {
		return nil
	}
	err := curFile.Close()
	curFile = nil
	return err
}

// SetLevel changes the minimum log level at runtime.
// Useful for tests or for toggling --trace mid-process.
func SetLevel(l Level) {
	curLvl.Store(int64(l.slogLevel()))
	mu.Lock()
	defer mu.Unlock()
	// Re-create logger so the new level takes effect for the handler.
	// We can't change a slog.Handler's level after construction without
	// using a LevelVar, which we use below so this is actually cheap.
	if lvHolder != nil {
		lvHolder.Set(l.slogLevel())
	}
}

// Enabled reports whether the given level would be emitted.
// Cheap; useful for guarding expensive attribute construction.
func Enabled(l Level) bool {
	return l.slogLevel() >= slog.Level(curLvl.Load())
}

// ─── Phase context ────────────────────────────────────────────

type phaseKey struct{}

// WithPhase returns a context carrying the given phase name.
// Subsequent log calls receiving this context auto-attach phase=<name>.
func WithPhase(ctx context.Context, phase string) context.Context {
	return context.WithValue(ctx, phaseKey{}, phase)
}

// Phase returns the phase attached to ctx, or "" if none.
func Phase(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	p, _ := ctx.Value(phaseKey{}).(string)
	return p
}

// ─── Logging API ──────────────────────────────────────────────

// Trace logs at LevelTrace. Per-item, verbose, off by default.
func Trace(ctx context.Context, msg string, attrs ...any) {
	logAt(ctx, LevelTrace, msg, attrs...)
}

// Info logs at LevelInfo. Default level. Phase boundaries, summaries.
func Info(ctx context.Context, msg string, attrs ...any) {
	logAt(ctx, LevelInfo, msg, attrs...)
}

// Error logs at LevelError. Recoverable failures. Always emitted.
// Convention: pass err as an "err" attribute, e.g.
//
//	trace.Error(ctx, "parse failed", "file", path, "err", err)
func Error(ctx context.Context, msg string, attrs ...any) {
	logAt(ctx, LevelError, msg, attrs...)
}

func logAt(ctx context.Context, l Level, msg string, attrs ...any) {
	if !Enabled(l) {
		return
	}
	mu.RLock()
	lg := current
	mu.RUnlock()

	if p := Phase(ctx); p != "" {
		attrs = append([]any{"phase", p}, attrs...)
	}
	lg.LogAttrs(ctx, l.slogLevel(), msg, anySliceToAttrs(attrs)...)
}

// anySliceToAttrs converts a flat key,value,key,value,... slice
// into []slog.Attr. Non-string keys are coerced to "?".
func anySliceToAttrs(kv []any) []slog.Attr {
	if len(kv) == 0 {
		return nil
	}
	out := make([]slog.Attr, 0, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			k = "?"
		}
		var v any
		if i+1 < len(kv) {
			v = kv[i+1]
		}
		out = append(out, slog.Any(k, v))
	}
	return out
}

// ─── Helpers for timing ───────────────────────────────────────

// Timer is a small helper for "log how long this phase took."
//
//	t := trace.Start(ctx, "parse")
//	defer t.End("files", len(files))
//
// On End, emits an Info event with op=<name> duration_ms=<float>.
type Timer struct {
	ctx   context.Context
	name  string
	start time.Time
}

// Start records the current time and returns a Timer.
func Start(ctx context.Context, name string) *Timer {
	return &Timer{ctx: ctx, name: name, start: time.Now()}
}

// End emits the timing event. extraAttrs are appended verbatim.
func (t *Timer) End(extraAttrs ...any) {
	if t == nil {
		return
	}
	d := time.Since(t.start)
	attrs := append([]any{"op", t.name, "duration_ms", float64(d.Microseconds()) / 1000.0}, extraAttrs...)
	Info(t.ctx, "op complete", attrs...)
}

// ─── Internal handler construction ────────────────────────────

// lvHolder is the shared slog.LevelVar used by handlers so we
// can update levels at runtime without rebuilding loggers.
var lvHolder *slog.LevelVar

func newLevelVar(l Level) *slog.LevelVar {
	lv := new(slog.LevelVar)
	lv.Set(l.slogLevel())
	lvHolder = lv
	return lv
}

// newTextLogger writes human-readable lines to w.
// Format:  HH:MM:SS.mmm LEVEL  msg  key=value key=value
func newTextLogger(w io.Writer, l Level) *slog.Logger {
	lv := newLevelVar(l)
	h := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level:       lv,
		ReplaceAttr: replaceAttrForText,
	})
	return slog.New(h)
}

// newMultiLogger writes text to stderr and JSON to file.
func newMultiLogger(stderr io.Writer, file io.Writer, l Level) *slog.Logger {
	lv := newLevelVar(l)
	textH := slog.NewTextHandler(stderr, &slog.HandlerOptions{
		Level:       lv,
		ReplaceAttr: replaceAttrForText,
	})
	jsonH := slog.NewJSONHandler(file, &slog.HandlerOptions{
		Level: lv,
	})
	return slog.New(&multiHandler{handlers: []slog.Handler{textH, jsonH}})
}

// replaceAttrForText prettifies timestamps and the level name for
// human reading without affecting the JSON file output.
func replaceAttrForText(groups []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.TimeKey:
		if t, ok := a.Value.Any().(time.Time); ok {
			a.Value = slog.StringValue(t.Format("15:04:05.000"))
		}
	case slog.LevelKey:
		// slog's default prints DEBUG/INFO/WARN/ERROR; our LevelTrace
		// is slog.Level(-8) which slog prints as "DEBUG-4". Normalize.
		if lv, ok := a.Value.Any().(slog.Level); ok {
			a.Value = slog.StringValue(Level(int(lv)).String())
		}
	}
	return a
}

// multiHandler fans out to N child handlers.
type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range m.handlers {
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := &multiHandler{handlers: make([]slog.Handler, len(m.handlers))}
	for i, h := range m.handlers {
		out.handlers[i] = h.WithAttrs(attrs)
	}
	return out
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	out := &multiHandler{handlers: make([]slog.Handler, len(m.handlers))}
	for i, h := range m.handlers {
		out.handlers[i] = h.WithGroup(name)
	}
	return out
}
