// Patched in Phase A: added TRACE/ERROR logging on cache hit/miss/parse-fail.
// No behavior change. Trace import added.
package ast

// ─────────────────────────────────────────────────────────────
// cache.go — Checksum-based AST cache
//
// Modules are expensive to produce (subprocesses for Python,
// full parse for large TS files). Cache keys on SHA-256 of
// the source bytes so re-parsing only happens on actual change.
// ─────────────────────────────────────────────────────────────

import (
	stdctx "context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gen-system/generator/internal/trace"
)

// Cache stores parsed modules keyed by (filePath, checksum).
// The in-memory map is the primary cache; the disk path is
// optional persistence across process restarts.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*Module // key: filePath+"#"+checksum
	diskDir string             // e.g. ".gen/ast_cache" — empty to disable disk
}

// NewCache creates a cache that optionally persists to diskDir.
// Pass diskDir="" to use in-memory only.
func NewCache(diskDir string) *Cache {
	c := &Cache{
		entries: make(map[string]*Module),
		diskDir: diskDir,
	}
	if diskDir != "" {
		c.loadDisk()
	}
	return c
}

// cacheKey builds a map key from path + checksum.
func cacheKey(filePath, checksum string) string {
	return filePath + "#" + checksum
}

// ChecksumFile computes the SHA-256 of a file's contents.
// Returns "" if the file cannot be read.
func ChecksumFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

// ChecksumBytes computes the SHA-256 of a byte slice.
func ChecksumBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

// Get retrieves a cached Module. Returns (nil, false) on miss.
func (c *Cache) Get(filePath, checksum string) (*Module, bool) {
	c.mu.RLock()
	m, ok := c.entries[cacheKey(filePath, checksum)]
	c.mu.RUnlock()
	return m, ok
}

// Put stores a Module in the cache and optionally persists it.
func (c *Cache) Put(m *Module) {
	if m == nil || m.Checksum == "" {
		return
	}
	key := cacheKey(m.FilePath, m.Checksum)
	c.mu.Lock()
	c.entries[key] = m
	c.mu.Unlock()

	if c.diskDir != "" {
		c.saveToDisk(m)
	}
}

// Invalidate removes all entries for filePath regardless of checksum.
func (c *Cache) Invalidate(filePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		// key format: "filePath#checksum"
		if len(k) > len(filePath) && k[:len(filePath)] == filePath {
			delete(c.entries, k)
		}
	}
}

// ParseOrCache parses the file at path using the appropriate frontend,
// returning a cached Module if the file is unchanged.
//
// Phase A: emits TRACE on hit/miss, ERROR on parse failure.
func (c *Cache) ParseOrCache(path string) (*Module, error) {
	tctx := trace.WithPhase(stdctx.Background(), "parse")

	data, err := os.ReadFile(path)
	if err != nil {
		trace.Error(tctx, "cache read failed", "file", path, "err", err)
		return nil, fmt.Errorf("cache: read %s: %w", path, err)
	}
	sum := ChecksumBytes(data)
	if m, ok := c.Get(path, sum); ok {
		trace.Trace(tctx, "cache hit", "file", path, "checksum", sum[:8])
		return m, nil
	}
	fe := ForFile(path)
	if fe == nil {
		// No frontend registered — not an error, just unsupported extension.
		trace.Trace(tctx, "no frontend", "file", path, "ext", filepath.Ext(path))
		return nil, nil
	}
	timer := trace.Start(tctx, "parse")
	m, err := fe.Parse(path, data)
	if err != nil {
		trace.Error(tctx, "frontend parse failed", "file", path, "lang", fe.Language(), "err", err)
		return nil, err
	}
	if m != nil {
		c.Put(m)
		timer.End("file", path, "lang", m.Language, "nodes", len(m.Nodes))
	} else {
		timer.End("file", path, "result", "nil-module")
	}
	return m, nil
}

// ─── Disk persistence ────────────────────────────────────────

// diskEntry is what we serialize. RawSource is NOT persisted to
// keep disk footprint small.
type diskEntry struct {
	FilePath    string       `json:"file"`
	Checksum    string       `json:"checksum"`
	Language    Language     `json:"lang"`
	PackageName string       `json:"pkg"`
	Nodes       []*Node      `json:"nodes"`
	ParseErrors []ParseError `json:"errors,omitempty"`
}

func (c *Cache) saveToDisk(m *Module) {
	if err := os.MkdirAll(c.diskDir, 0755); err != nil {
		return
	}
	entry := diskEntry{
		FilePath:    m.FilePath,
		Checksum:    m.Checksum,
		Language:    m.Language,
		PackageName: m.PackageName,
		Nodes:       m.Nodes,
		ParseErrors: m.ParseErrors,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	// One file per (filePath, checksum) pair using a safe hex name
	name := fmt.Sprintf("%x.json", sha256.Sum256([]byte(m.FilePath+m.Checksum)))
	_ = os.WriteFile(filepath.Join(c.diskDir, name), data, 0644)
}

func (c *Cache) loadDisk() {
	entries, err := os.ReadDir(c.diskDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.diskDir, e.Name()))
		if err != nil {
			continue
		}
		var de diskEntry
		if err := json.Unmarshal(data, &de); err != nil {
			continue
		}
		m := &Module{
			FilePath:    de.FilePath,
			Checksum:    de.Checksum,
			Language:    de.Language,
			PackageName: de.PackageName,
			Nodes:       de.Nodes,
			ParseErrors: de.ParseErrors,
		}
		c.entries[cacheKey(de.FilePath, de.Checksum)] = m
	}
}

// DefaultCache is the process-wide cache backed by .gen/ast_cache.
// Callers can use this directly or create their own Cache.
var DefaultCache = NewCache(".gen/ast_cache")
