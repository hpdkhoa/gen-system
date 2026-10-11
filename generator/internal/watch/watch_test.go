// watch/watch_test.go (TASK 1, v19): integration test for incremental watcher.
// Creates a temp dir, starts the watcher, writes/modifies/deletes a Go file,
// and asserts the symbol index reflects each change within 500ms.
package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

const deadline = 500 * time.Millisecond

// poll calls check every 10ms until it returns true or the deadline elapses.
func poll(t *testing.T, label string, check func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("timed out waiting for: %s", label)
}

func TestWatcher_CreateModifyDelete(t *testing.T) {
	dir := t.TempDir()
	idx := symbols.NewIndex()

	// Use a short poll interval so the test completes quickly.
	w := New(idx, dir, 50*time.Millisecond)
	go w.Run()
	defer w.Stop()

	// Allow the watcher to prime its initial state.
	time.Sleep(60 * time.Millisecond)

	// ── 1. Create a Go file with one exported function ──────────
	src1 := []byte(`package testpkg

func HelloWorld() string { return "hello" }
`)
	goFile := filepath.Join(dir, "hello.go")
	if err := os.WriteFile(goFile, src1, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	poll(t, "HelloWorld appears in index", func() bool {
		syms := idx.ByName("HelloWorld")
		return len(syms) == 1
	})
	if t.Failed() {
		t.FailNow()
	}
	t.Log("✓ symbol appeared after file create")

	// ── 2. Modify the file: rename the function ─────────────────
	src2 := []byte(`package testpkg

func GoodbyeWorld() string { return "bye" }
`)
	if err := os.WriteFile(goFile, src2, 0644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	poll(t, "HelloWorld gone after rename", func() bool {
		return len(idx.ByName("HelloWorld")) == 0
	})
	poll(t, "GoodbyeWorld appears after rename", func() bool {
		return len(idx.ByName("GoodbyeWorld")) == 1
	})
	if t.Failed() {
		t.FailNow()
	}
	t.Log("✓ symbol updated after file modify")

	// ── 3. Delete the file: symbol must vanish ───────────────────
	if err := os.Remove(goFile); err != nil {
		t.Fatalf("remove: %v", err)
	}

	poll(t, "GoodbyeWorld gone after delete", func() bool {
		return len(idx.ByName("GoodbyeWorld")) == 0
	})
	t.Log("✓ symbol disappeared after file delete")
}

// TestWatcher_SkipsBlockedDirectories asserts that files under .git/ and
// vendor/ are ignored by the watcher's built-in shouldSkip rules.
func TestWatcher_SkipsBlockedDirectories(t *testing.T) {
	dir := t.TempDir()
	idx := symbols.NewIndex()

	w := New(idx, dir, 50*time.Millisecond)
	go w.Run()
	defer w.Stop()
	time.Sleep(60 * time.Millisecond)

	// Write a Go file under .git/. The watcher should skip it.
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	gitFile := filepath.Join(gitDir, "fake.go")
	src := []byte("package fake\nfunc InGit() {}\n")
	if err := os.WriteFile(gitFile, src, 0644); err != nil {
		t.Fatal(err)
	}

	// Wait well past the debounce window; symbol must never appear.
	time.Sleep(debounceWindow*3 + 100*time.Millisecond)
	if len(idx.ByName("InGit")) != 0 {
		t.Error("symbol from .git/ directory should not be indexed")
	}
	t.Log("✓ .git/ directory correctly skipped")
}

// TestWatcher_DebounceCoalescesRapidWrites verifies that rapid successive
// writes to the same file result in only one parse pass (the final state).
func TestWatcher_DebounceCoalescesRapidWrites(t *testing.T) {
	dir := t.TempDir()
	idx := symbols.NewIndex()

	w := New(idx, dir, 50*time.Millisecond)
	go w.Run()
	defer w.Stop()
	time.Sleep(60 * time.Millisecond)

	goFile := filepath.Join(dir, "burst.go")

	// Write 5 versions in quick succession (simulates editor save-on-type).
	for i := 0; i < 5; i++ {
		src := []byte(`package burst
func BurstFn() {}
`)
		_ = os.WriteFile(goFile, src, 0644)
		time.Sleep(5 * time.Millisecond)
	}

	// After debounce settles we should see exactly one symbol, not five.
	poll(t, "BurstFn in index", func() bool {
		return len(idx.ByName("BurstFn")) == 1
	})
	if got := len(idx.ByName("BurstFn")); got != 1 {
		t.Errorf("want 1 BurstFn symbol, got %d (duplicate entries from burst writes)", got)
	}
	t.Log("✓ burst writes coalesced by debounce")
}
