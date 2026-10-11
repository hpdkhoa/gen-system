package intelligence

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

const MemoryFile = ".gen/memory.json"

// ─────────────────────────────────────────────
// FixRecord stores a repair that was applied, with outcome tracking.
// Keyed by the SHA256 hash of the error message so identical errors
// (across entities/tasks) share the same memory entry.
// ─────────────────────────────────────────────
type FixRecord struct {
	ErrorType  string `json:"error_type"`
	TaskType   string `json:"task_type"`
	EntityName string `json:"entity_name"`
	FixBody    string `json:"fix_body"`
	// BrokenBody is the function body that failed, before the repair.
	// v26.58 (follow up of D050): the training export shows it under
	// "Broken Code"; before, the record held only the fix.
	BrokenBody   string `json:"broken_body,omitempty"`
	SuccessCount int    `json:"success_count"`
	FailCount    int    `json:"fail_count"`
}

// Memory is the persistent store of AI repairs.
// It lives in .gen/memory.json and is versioned with the project.
type Memory struct {
	Fixes map[string]FixRecord `json:"fixes"`
}

// LoadMemory reads the memory file from disk.
// Returns an empty memory (not an error) if the file doesn't exist yet.
func LoadMemory() Memory {
	data, err := os.ReadFile(MemoryFile)
	if err != nil {
		return Memory{Fixes: map[string]FixRecord{}}
	}
	var m Memory
	if err := json.Unmarshal(data, &m); err != nil {
		return Memory{Fixes: map[string]FixRecord{}}
	}
	if m.Fixes == nil {
		m.Fixes = map[string]FixRecord{}
	}
	return m
}

// SaveMemory writes the memory to disk.
// Creates .gen/ if it doesn't exist.
func SaveMemory(m Memory) error {
	if err := os.MkdirAll(".gen", 0755); err != nil {
		return fmt.Errorf("memory: mkdir .gen: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(MemoryFile, data, 0644)
}

// HashError creates a short, stable key from an error message.
// Uses first 8 bytes of SHA256. Collision probability is negligible
// for the number of distinct compiler errors in a typical project.
func HashError(errMsg string) string {
	h := sha256.Sum256([]byte(errMsg))
	return fmt.Sprintf("%x", h[:8])
}

// Get retrieves a fix record by error hash.
// Returns the record and whether it was found.
func (m *Memory) Get(errHash string) (FixRecord, bool) {
	rec, ok := m.Fixes[errHash]
	return rec, ok
}

// RecordSuccess saves a successful repair to memory.
// If a record already exists for this hash, increments success count.
func (m *Memory) RecordSuccess(errHash, errType, taskType, entityName, fixBody string) {
	rec := m.Fixes[errHash]
	rec.ErrorType = errType
	rec.TaskType = taskType
	rec.EntityName = entityName
	rec.FixBody = fixBody
	rec.SuccessCount++
	m.Fixes[errHash] = rec
}

// RecordBroken stores the body that failed for an error hash. An empty body
// keeps what the record already holds.
func (m *Memory) RecordBroken(errHash, brokenBody string) {
	if brokenBody == "" {
		return
	}
	rec := m.Fixes[errHash]
	rec.BrokenBody = brokenBody
	m.Fixes[errHash] = rec
}

// RecordFailure marks that a previously cached fix did not work.
// After enough failures, the cached fix should be evicted.
func (m *Memory) RecordFailure(errHash string) {
	rec := m.Fixes[errHash]
	rec.FailCount++
	m.Fixes[errHash] = rec
}

// Evict removes entries where failures exceed successes by a threshold.
// Called automatically after SaveMemory to keep the cache clean.
func (m *Memory) Evict() {
	for key, rec := range m.Fixes {
		if rec.FailCount > 0 && rec.FailCount >= rec.SuccessCount*2 {
			delete(m.Fixes, key)
		}
	}
}

// Stats returns human-readable memory statistics for logging.
func (m *Memory) Stats() string {
	total := len(m.Fixes)
	usable := 0
	for _, rec := range m.Fixes {
		if rec.SuccessCount > 0 {
			usable++
		}
	}
	return fmt.Sprintf("%d total records, %d with successful fixes", total, usable)
}

// TopEntities returns the entity names with the most recorded fixes,
// useful for understanding which parts of the codebase need the most repair.
func (m *Memory) TopEntities() []string {
	counts := map[string]int{}
	for _, rec := range m.Fixes {
		if rec.EntityName != "" {
			counts[rec.EntityName]++
		}
	}
	type pair struct {
		name  string
		count int
	}
	var pairs []pair
	for k, v := range counts {
		pairs = append(pairs, pair{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].count > pairs[j].count })

	var result []string
	for i, p := range pairs {
		if i >= 5 {
			break
		}
		result = append(result, fmt.Sprintf("%s(%d)", p.name, p.count))
	}
	return result
}
