package router

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// GenerateRouter writes frontend/AppRouter.jsx. It returns an error when the
// file cannot be written.
func GenerateRouter(entities []schema.Entity, path string) error {

	var routes []string
	var imports []string

	for _, e := range entities {

		name := e.Name
		lower := strings.ToLower(name)

		imports = append(imports,
			fmt.Sprintf(`import %sList from "./%sList";`, name, lower),
		)

		routes = append(routes,
			fmt.Sprintf(`<Route path="/%ss" element={<%sList />} />`, lower, name),
		)
	}

	content := fmt.Sprintf(`


import { BrowserRouter, Routes, Route } from "react-router-dom";
%s

export default function AppRouter() {
return ( <BrowserRouter> <Routes>
%s </Routes> </BrowserRouter>
);
}
`, strings.Join(imports, "\n"), strings.Join(routes, "\n"))

	// v26.58 (D034): create frontend/ first and return the write error. The
	// bare os.WriteFile failed on a root with no frontend folder (or a read
	// only AppRouter.jsx), nothing was reported, and the task counted as done.
	dir := filepath.Join(path, "frontend")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("frontend router: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AppRouter.jsx"), []byte(content), 0o644); err != nil {
		return fmt.Errorf("frontend router: %w", err)
	}
	return nil
}
