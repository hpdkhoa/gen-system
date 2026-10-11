// context/skip_generated_test.go (v25.6.1): pins the freeze fix. Loading a
// generated project root used to walk into the dense generated frontend/mobile
// TS/JSX trees and parse them synchronously in the load handler, freezing the
// Studio. shouldSkip now excludes those generated sibling layers while still
// parsing the backend Go and any legacy source at the root.
package context

import (
	"path/filepath"
	"testing"
)

func TestShouldSkip_GeneratedLayers(t *testing.T) {
	skip := []string{
		// generated web/mobile layers (the freeze source)
		"frontend",
		filepath.Join("frontend", "src", "App.tsx"),
		"mobile",
		filepath.Join("mobile", "screens", "List.tsx"),
		// manifest + migrations churn
		"design-manifest",
		filepath.Join("design-manifest", "plan.json"),
		filepath.Join("db", "migrations"),
		filepath.Join("db", "migrations", "0001_init.sql"),
		// always-skip set (unchanged)
		".git", ".gen", "node_modules", "vendor", "output",
		filepath.Join("node_modules", "react", "index.js"),
		filepath.Join("output", "backend", "main.go"),
	}
	for _, rel := range skip {
		if !shouldSkip(rel) {
			t.Errorf("shouldSkip(%q) = false, want true (generated/ignored)", rel)
		}
	}
}

func TestShouldSkip_KeepsRealSource(t *testing.T) {
	keep := []string{
		// backend Go is the whole point; it must still parse
		filepath.Join("backend", "main.go"),
		filepath.Join("backend", "internal", "service", "order.go"),
		// legacy source at the root
		"legacy.cob",
		filepath.Join("src", "Account.java"),
		"handler.py",
		// a hand-written db/*.sql at the db root (not under migrations/)
		filepath.Join("db", "seed.sql"),
		// a dir that only contains the substring must not be skipped
		filepath.Join("myfrontendlib", "x.go"),
	}
	for _, rel := range keep {
		if shouldSkip(rel) {
			t.Errorf("shouldSkip(%q) = true, want false (real source)", rel)
		}
	}
}
