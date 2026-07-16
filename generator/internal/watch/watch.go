// watch/watch.go — TASK 1 (v19): added 200ms debounce, shouldSkip integration,
// and NotifyFunc hook so gen-server can push SSE events after each file change.
package watch

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gen-system/generator/internal/ast"
	"gen-system/generator/internal/flow"
	"gen-system/generator/internal/symbols"
)

// Event is delivered on the Events channel whenever a file changes.
type Event struct {
	Path    string // absolute path that changed
	Kind    EventKind
	Module  *ast.Module              // freshly parsed module (nil on Delete)
	CFGs    flow.CFGSet              // freshly built CFGs (nil on Delete)
	Effects map[string]*flow.FuncEffects
}

// EventKind classifies what happened to the file.
type EventKind string

const (
	EventModified EventKind = "modified"
	EventDeleted  EventKind = "deleted"
)

// NotifyFunc is called after the index has been updated for a file.
// path is the absolute path. kind is "modified" or "deleted".
// It is called from the watcher's internal goroutine; it must not block.
type NotifyFunc func(path string, kind EventKind)

// Watcher polls a directory tree for source-file changes and incrementally
// rebuilds the symbol index. Debounce: changes are coalesced over a 200ms
// quiet window before being processed and emitted.
//
// Usage:
//
//	w := watch.New(idx, root, 500*time.Millisecond)
//	w.OnChange = func(path string, kind watch.EventKind) { ... }  // optional
//	go w.Run()
//	for ev := range w.Events {
//	    // react to incremental update
//	}
//	w.Stop()
type Watcher struct {
	// Events receives one Event per changed file. Buffered (64).
	Events <-chan Event

	// OnChange is an optional hook called after each index update.
	// It runs in the watcher goroutine; must not block.
	OnChange NotifyFunc

	// SkipPath is an optional predicate that supplements the built-in
	// shouldSkip rules. Return true to ignore the path. If nil, only the
	// built-in rules apply. Path is relative to root.
	SkipPath func(rel string) bool

	root     string
	idx      *symbols.Index
	interval time.Duration

	stop   chan struct{}
	once   sync.Once
	events chan Event

	// debounce: pending file-change accumulator
	pendingMu sync.Mutex
	pending   map[string]pendingKind // absPath → last-seen kind

	// state: path → last seen ModTime + Size
	mu    sync.Mutex
	state map[string]fileState
}

type fileState struct {
	modTime time.Time
	size    int64
}

type pendingKind int

const (
	pendingModified pendingKind = iota
	pendingDeleted
)

// debounceWindow is the quiet period after the last change before the
// accumulated batch is flushed. Matches the spec: ~200ms.
const debounceWindow = 200 * time.Millisecond

// New creates a Watcher rooted at root that updates idx on changes.
// interval controls how often the tree is stat-polled (suggested: 500ms).
// Call Run() in a goroutine; stop with Stop().
func New(idx *symbols.Index, root string, interval time.Duration) *Watcher {
	ch := make(chan Event, 64)
	w := &Watcher{
		Events:   ch,
		root:     root,
		idx:      idx,
		interval: interval,
		stop:     make(chan struct{}),
		events:   ch,
		state:    make(map[string]fileState),
		pending:  make(map[string]pendingKind),
	}
	return w
}

// Run starts the polling loop. Blocks until Stop() is called.
// Call in a goroutine: go w.Run()
func (w *Watcher) Run() {
	// Prime state without emitting events.
	w.scanDir(false)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	debounce := time.NewTimer(0)
	<-debounce.C // drain the initial fire

	var pendingFlush bool

	for {
		select {
		case <-w.stop:
			return

		case <-ticker.C:
			changed := w.scanDir(true)
			if changed {
				if !pendingFlush {
					pendingFlush = true
					debounce.Reset(debounceWindow)
				} else {
					// Extend the quiet window.
					if !debounce.Stop() {
						select {
						case <-debounce.C:
						default:
						}
					}
					debounce.Reset(debounceWindow)
				}
			}

		case <-debounce.C:
			if pendingFlush {
				pendingFlush = false
				w.flushPending()
			}
		}
	}
}

// Stop shuts down the watcher. Safe to call multiple times.
func (w *Watcher) Stop() {
	w.once.Do(func() { close(w.stop) })
}

// Snapshot returns a copy of the current file-state map.
// Useful for testing; not needed in production.
func (w *Watcher) Snapshot() map[string]fileState {
	w.mu.Lock()
	defer w.mu.Unlock()
	cp := make(map[string]fileState, len(w.state))
	for k, v := range w.state {
		cp[k] = v
	}
	return cp
}

// ─── Scan ─────────────────────────────────────────────────────

// scanDir walks the directory tree and accumulates pending changes.
// Returns true if any changes were found.
func (w *Watcher) scanDir(emit bool) bool {
	// Collect current file set from disk.
	current := make(map[string]fileState)
	_ = filepath.Walk(w.root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !isWatchedExt(path) {
			return nil
		}
		rel, _ := filepath.Rel(w.root, path)
		if w.skipRel(rel) {
			return nil
		}
		current[path] = fileState{modTime: info.ModTime(), size: info.Size()}
		return nil
	})

	w.mu.Lock()
	prev := w.state
	w.state = current
	w.mu.Unlock()

	if !emit {
		return false
	}

	changed := false

	w.pendingMu.Lock()
	// Detect modified / new files.
	for path, cur := range current {
		old, seen := prev[path]
		if !seen || old.modTime != cur.modTime || old.size != cur.size {
			w.pending[path] = pendingModified
			changed = true
		}
	}
	// Detect deleted files.
	for path := range prev {
		if _, exists := current[path]; !exists {
			w.pending[path] = pendingDeleted
			changed = true
		}
	}
	w.pendingMu.Unlock()

	return changed
}

// flushPending processes all accumulated pending changes.
func (w *Watcher) flushPending() {
	w.pendingMu.Lock()
	batch := w.pending
	w.pending = make(map[string]pendingKind)
	w.pendingMu.Unlock()

	for path, kind := range batch {
		switch kind {
		case pendingModified:
			w.handleChange(path)
		case pendingDeleted:
			w.handleDelete(path)
		}
	}
}

// handleChange re-parses path and updates the index.
func (w *Watcher) handleChange(path string) {
	fe := ast.ForFile(path)
	if fe == nil {
		return
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return
	}
	m, err := fe.Parse(path, src)
	if err != nil || m == nil {
		return
	}

	// Atomic index update (UpdateFile = RemoveFile + AddModule under one lock).
	w.idx.UpdateFile(path, m)

	cfgSet := flow.Build(m, w.idx)
	effects := flow.InferEffects(m, cfgSet)

	select {
	case w.events <- Event{
		Path:    path,
		Kind:    EventModified,
		Module:  m,
		CFGs:    cfgSet,
		Effects: effects,
	}:
	default:
		// Channel full — drop; caller can read from Events at its own pace.
	}

	if w.OnChange != nil {
		w.OnChange(path, EventModified)
	}
}

// handleDelete removes a file's symbols from the index.
func (w *Watcher) handleDelete(path string) {
	w.idx.RemoveFile(path)

	select {
	case w.events <- Event{
		Path: path,
		Kind: EventDeleted,
	}:
	default:
	}

	if w.OnChange != nil {
		w.OnChange(path, EventDeleted)
	}
}

// ─── Helpers ──────────────────────────────────────────────────

// skipRel applies the built-in shouldSkip rules and the caller-supplied
// SkipPath predicate (if any).
func (w *Watcher) skipRel(rel string) bool {
	if builtinSkip(rel) {
		return true
	}
	if w.SkipPath != nil {
		return w.SkipPath(rel)
	}
	return false
}

// builtinSkip mirrors context.shouldSkip without importing context
// (to keep the watch package free of that dependency).
func builtinSkip(rel string) bool {
	blocked := []string{
		".git", ".gen/snapshots", "node_modules", "vendor",
		"output", ".gen/entities",
	}
	for _, b := range blocked {
		if strings.HasPrefix(rel, b) {
			return true
		}
	}
	return false
}

// isWatchedExt returns true for source file extensions the engine handles.
func isWatchedExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".ts", ".tsx", ".py", ".pyw",
		".ad", ".cagen", ".gel",
		".cob", ".cbl", ".cpy", ".cobol":
		return true
	}
	return false
}
