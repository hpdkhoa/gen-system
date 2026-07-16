package ast

import (
	"fmt"
	"os"
	"strings"
)

// UpsertRoute safely injects an HTTP route into the router file.
// Strategy: find the ROUTES injection comment anchor and insert there.
// Falls back to AST-aware file append if anchor is missing.
func UpsertRoute(path, route, handler string) {

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("⚠️ UpsertRoute: could not read router file:", path, err)
		return
	}

	content := string(data)
	routeLine := "\t" + route

	// Idempotent: skip if already present
	if strings.Contains(content, route) {
		return
	}

	// ✅ Strategy 1: inject at anchor comment
	anchor := "// ROUTES (auto-injected)"
	if strings.Contains(content, anchor) {
		content = strings.Replace(content, anchor, anchor+"\n"+routeLine, 1)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			fmt.Println("⚠️ UpsertRoute: write failed:", err)
		}
		return
	}

	// ✅ Strategy 2: inject before last closing brace of Register/main func
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "}" {
			lines = append(lines[:i], append([]string{routeLine}, lines[i:]...)...)
			newContent := strings.Join(lines, "\n")
			if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
				fmt.Println("⚠️ UpsertRoute fallback: write failed:", err)
			}
			return
		}
	}

	fmt.Println("⚠️ UpsertRoute: could not find injection point in", path)
}
