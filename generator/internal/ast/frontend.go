package ast

// ─────────────────────────────────────────────────────────────
// frontend.go: Frontend interface + global registry
//
// Every language frontend implements Frontend and registers
// itself via init(). Callers never import frontend packages
// directly; they go through the registry.
// ─────────────────────────────────────────────────────────────

import (
	"path/filepath"
	"strings"
	"sync"
)

// Frontend is the contract every language parser must satisfy.
type Frontend interface {
	// Language returns the canonical language identifier.
	Language() Language

	// Extensions returns the file extensions this frontend handles,
	// e.g. []string{".go"} or []string{".ts", ".tsx"}.
	Extensions() []string

	// Available returns true if the frontend can operate on the
	// current machine (all external dependencies present).
	// A frontend that returns false must still be registered.
	// The presentation layer then shows that it is unavailable.
	Available() bool

	// Parse converts source bytes at filePath into a Module.
	// Recoverable parse failures produce NodeUnknown nodes, not errors.
	// Only unrecoverable failures (file unreadable, runtime missing)
	// return a non-nil error.
	Parse(filePath string, source []byte) (*Module, error)

	// Emit converts a Module back to source bytes.
	// For modifications to be lossless, the frontend must reproduce
	// unchanged regions byte-identically.
	Emit(module *Module) ([]byte, error)
}

// ─── Registry ────────────────────────────────────────────────

var (
	registryMu sync.RWMutex
	registry   = map[Language]Frontend{}
	extMap     = map[string]Language{} // ".go" → LangGo
)

// Register adds a frontend to the global registry.
// Called from each frontend's init() function.
// Panics on duplicate Language registration (programming error).
func Register(f Frontend) {
	registryMu.Lock()
	defer registryMu.Unlock()

	lang := f.Language()
	if _, dup := registry[lang]; dup {
		panic("ast: frontend already registered for language " + string(lang))
	}
	registry[lang] = f
	for _, ext := range f.Extensions() {
		extMap[strings.ToLower(ext)] = lang
	}
}

// ForLanguage returns the frontend for a given language.
// Returns nil if no frontend is registered.
func ForLanguage(lang Language) Frontend {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[lang]
}

// ForFile returns the appropriate frontend for a file path based
// on its extension. Returns nil when no frontend matches.
func ForFile(path string) Frontend {
	ext := strings.ToLower(filepath.Ext(path))
	registryMu.RLock()
	lang, ok := extMap[ext]
	registryMu.RUnlock()
	if !ok {
		return nil
	}
	return ForLanguage(lang)
}

// All returns all registered frontends.
func All() []Frontend {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Frontend, 0, len(registry))
	for _, f := range registry {
		out = append(out, f)
	}
	return out
}

// ParseFile is a convenience wrapper: detect language, pick frontend, parse.
// Returns (nil, nil) when no frontend is registered for the file's extension.
func ParseFile(path string, source []byte) (*Module, error) {
	fe := ForFile(path)
	if fe == nil {
		return nil, nil
	}
	return fe.Parse(path, source)
}
