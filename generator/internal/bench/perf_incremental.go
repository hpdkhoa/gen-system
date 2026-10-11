// generator/internal/bench/perf_incremental.go (v19.7): measures the end-to-end
// latency from "source file changed on disk" to "index updated and watcher
// event delivered". Uses a temp dir so we don't disturb the user's tree.
//
// Floor is bounded by the watcher's poll interval + the 200ms debounce
// window. We measure both phases:
//
//   - first_event_latency: from write() to first Event on the channel
//   - reflected_in_index: from write() to ByName lookup of the new symbol
package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
	"github.com/hpdkhoa/gen-system/generator/internal/watch"
)

// IncrementalUpdate writes a Go file into a temp dir, starts a watcher,
// modifies the file, and measures latency to event + index update.
//
// pollInterval is the Watcher poll. The test uses 100ms to keep wall time
// down without making the test flaky.
func IncrementalUpdate() Suite {
	t0 := time.Now()
	s := Suite{Name: "incremental_update_latency"}

	dir, err := os.MkdirTemp("", "genbench_watch_")
	if err != nil {
		s.Cases = []Case{{Name: "mktemp", Pass: false, Note: err.Error()}}
		s.Duration = time.Since(t0)
		return s
	}
	defer os.RemoveAll(dir)

	initial := []byte("package p\nfunc OriginalSymbol() {}\n")
	path := filepath.Join(dir, "src.go")
	if err := os.WriteFile(path, initial, 0644); err != nil {
		s.Cases = []Case{{Name: "write_initial", Pass: false, Note: err.Error()}}
		s.Duration = time.Since(t0)
		return s
	}

	idx := symbols.NewIndex()
	w := watch.New(idx, dir, 100*time.Millisecond)
	go w.Run()
	defer w.Stop()

	// Drain any priming events.
	drainTimeout := 500 * time.Millisecond
	deadline := time.Now().Add(drainTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-w.Events:
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Modify file. Backdate the mtime so polling notices a change even
	// on filesystems with second-precision timestamps that might collide
	// with the file we just wrote during priming.
	newSrc := []byte("package p\nfunc OriginalSymbol() {}\nfunc NewSymbol() {}\n")
	writeStart := time.Now()
	if err := os.WriteFile(path, newSrc, 0644); err != nil {
		s.Cases = []Case{{Name: "write_modified", Pass: false, Note: err.Error()}}
		s.Duration = time.Since(t0)
		return s
	}
	// Bump mtime forward to defeat second-precision filesystems.
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(path, future, future)

	// Wait for the first event up to 3 seconds.
	var eventLatency time.Duration
	gotEvent := false
	eventTimeout := time.NewTimer(3 * time.Second)
	defer eventTimeout.Stop()
WAIT:
	for {
		select {
		case <-w.Events:
			eventLatency = time.Since(writeStart)
			gotEvent = true
			break WAIT
		case <-eventTimeout.C:
			break WAIT
		}
	}

	// Now confirm the index reflects the new symbol.
	var indexLatency time.Duration
	indexed := false
	indexDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(indexDeadline) {
		if hits := idx.ByName("NewSymbol"); len(hits) > 0 {
			indexLatency = time.Since(writeStart)
			indexed = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	s.Cases = append(s.Cases,
		Case{Name: "event_delivered_within_3s", Pass: gotEvent, Got: fmt.Sprintf("%.0fms", eventLatency.Seconds()*1000), Want: "<=3000ms"},
		Case{Name: "new_symbol_in_index", Pass: indexed, Got: fmt.Sprintf("%.0fms", indexLatency.Seconds()*1000), Want: "<=2000ms after event"},
	)

	if gotEvent {
		s.Metrics = append(s.Metrics, Metric{
			Name: "first_event_latency_ms", Value: eventLatency.Seconds() * 1000, Unit: "ms",
		})
	}
	if indexed {
		s.Metrics = append(s.Metrics, Metric{
			Name: "index_updated_latency_ms", Value: indexLatency.Seconds() * 1000, Unit: "ms",
		})
	}

	s.Duration = time.Since(t0)
	return s
}
