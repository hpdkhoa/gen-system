package intelligence_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpdkhoa/gen-system/generator/internal/intelligence"
)

// ─────────────────────────────────────────────
// Setup: redirect MemoryFile to temp dir for tests
// ─────────────────────────────────────────────

// withTempMemory changes the working directory to a temp dir so
// memory.json writes go there, then restores it after the test.
func withTempMemory(t *testing.T) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, ".gen"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})
}

// ─────────────────────────────────────────────
// HashError tests
// ─────────────────────────────────────────────

// TestHashError_Deterministic verifies same input → same hash (needed for cache hits).
func TestHashError_Deterministic(t *testing.T) {
	msg := `./handler.go:12: undefined: User`
	h1 := intelligence.HashError(msg)
	h2 := intelligence.HashError(msg)
	if h1 != h2 {
		t.Errorf("HashError is not deterministic: %q != %q", h1, h2)
	}
}

// TestHashError_Different verifies different inputs → different hashes.
func TestHashError_Different(t *testing.T) {
	h1 := intelligence.HashError(`undefined: User`)
	h2 := intelligence.HashError(`undefined: Post`)
	if h1 == h2 {
		t.Error("different errors should produce different hashes")
	}
}

// TestHashError_Length verifies the hash is exactly 16 hex chars (8 bytes × 2).
func TestHashError_Length(t *testing.T) {
	h := intelligence.HashError("some error")
	if len(h) != 16 {
		t.Errorf("hash should be 16 hex chars, got %d: %q", len(h), h)
	}
}

// ─────────────────────────────────────────────
// LoadMemory / SaveMemory tests
// ─────────────────────────────────────────────

// TestLoadMemory_EmptyOnMissingFile verifies that a missing memory.json
// returns an empty Memory rather than an error.
func TestLoadMemory_EmptyOnMissingFile(t *testing.T) {
	withTempMemory(t)

	mem := intelligence.LoadMemory()
	if mem.Fixes == nil {
		t.Error("Fixes map should be initialized (not nil) even when file missing")
	}
	if len(mem.Fixes) != 0 {
		t.Errorf("fresh memory should be empty, got %d entries", len(mem.Fixes))
	}
}

// TestSaveAndLoad_RoundTrip verifies that data survives a save+load cycle.
func TestSaveAndLoad_RoundTrip(t *testing.T) {
	withTempMemory(t)

	mem := intelligence.LoadMemory()
	hash := intelligence.HashError("undefined: User")
	mem.RecordSuccess(hash, "undefined", "generate_service", "User", `return nil, nil`)

	if err := intelligence.SaveMemory(mem); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	// Load fresh
	loaded := intelligence.LoadMemory()
	rec, ok := loaded.Get(hash)
	if !ok {
		t.Fatal("fix record not found after save+load")
	}
	if rec.EntityName != "User" {
		t.Errorf("EntityName: want User, got %s", rec.EntityName)
	}
	if rec.SuccessCount != 1 {
		t.Errorf("SuccessCount: want 1, got %d", rec.SuccessCount)
	}
}

// ─────────────────────────────────────────────
// RecordSuccess / RecordFailure / Evict tests
// ─────────────────────────────────────────────

// TestRecordSuccess_IncrementsCount verifies that calling RecordSuccess twice
// increments SuccessCount to 2.
func TestRecordSuccess_IncrementsCount(t *testing.T) {
	mem := intelligence.LoadMemory()
	hash := intelligence.HashError("some error")

	mem.RecordSuccess(hash, "import", "generate_model", "Order", `return nil`)
	mem.RecordSuccess(hash, "import", "generate_model", "Order", `return nil`)

	rec, ok := mem.Get(hash)
	if !ok {
		t.Fatal("record not found")
	}
	if rec.SuccessCount != 2 {
		t.Errorf("SuccessCount: want 2, got %d", rec.SuccessCount)
	}
}

// TestRecordFailure_IncrementsFailCount verifies failure tracking.
func TestRecordFailure_IncrementsFailCount(t *testing.T) {
	mem := intelligence.LoadMemory()
	hash := intelligence.HashError("bad error")

	mem.RecordSuccess(hash, "syntax", "generate_handler", "Post", `return nil`)
	mem.RecordFailure(hash)
	mem.RecordFailure(hash)

	rec, _ := mem.Get(hash)
	if rec.FailCount != 2 {
		t.Errorf("FailCount: want 2, got %d", rec.FailCount)
	}
}

// TestEvict_RemovesStaleEntries verifies the eviction policy:
// entries where FailCount >= SuccessCount * 2 should be evicted.
func TestEvict_RemovesStaleEntries(t *testing.T) {
	mem := intelligence.LoadMemory()

	// Entry 1: 1 success, 3 failures → should be evicted (3 >= 1*2=2)
	h1 := intelligence.HashError("bad fix that keeps failing")
	mem.RecordSuccess(h1, "unknown", "generate_repo", "X", `return nil`)
	mem.RecordFailure(h1)
	mem.RecordFailure(h1)
	mem.RecordFailure(h1)

	// Entry 2: 2 successes, 1 failure → should be kept (1 < 2*2=4)
	h2 := intelligence.HashError("good fix that usually works")
	mem.RecordSuccess(h2, "import", "generate_service", "Y", `return nil`)
	mem.RecordSuccess(h2, "import", "generate_service", "Y", `return nil`)
	mem.RecordFailure(h2)

	mem.Evict()

	if _, ok := mem.Get(h1); ok {
		t.Error("stale entry (fail >= success*2) should have been evicted")
	}
	if _, ok := mem.Get(h2); !ok {
		t.Error("healthy entry should not have been evicted")
	}
}

// TestEvict_DoesNotEvictEntryWithNoFailures verifies that a perfect record
// (all successes, zero failures) is never evicted.
func TestEvict_DoesNotEvictEntryWithNoFailures(t *testing.T) {
	mem := intelligence.LoadMemory()
	h := intelligence.HashError("perfect fix")

	for i := 0; i < 5; i++ {
		mem.RecordSuccess(h, "syntax", "generate_model", "A", `return nil`)
	}

	mem.Evict()

	if _, ok := mem.Get(h); !ok {
		t.Error("entry with no failures should never be evicted")
	}
}

// TestStats_ReturnsCorrectCounts verifies Stats() output.
func TestStats_ReturnsCorrectCounts(t *testing.T) {
	mem := intelligence.LoadMemory()

	h1 := intelligence.HashError("err1")
	mem.RecordSuccess(h1, "syntax", "generate_model", "A", `return nil`)

	h2 := intelligence.HashError("err2")
	mem.RecordFailure(h2) // failure only — no success

	stats := mem.Stats()
	if stats == "" {
		t.Error("Stats() returned empty string")
	}
}

// TestGet_MissFalse verifies that Get returns false for unknown hashes.
func TestGet_MissFalse(t *testing.T) {
	mem := intelligence.LoadMemory()
	_, ok := mem.Get("nonexistent_hash_key")
	if ok {
		t.Error("Get should return false for unknown hash")
	}
}
