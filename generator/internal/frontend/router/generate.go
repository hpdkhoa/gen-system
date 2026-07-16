package router

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gen-system/generator/internal/schema"
)

func GenerateRouter(entities []schema.Entity, path string) {

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

	file := filepath.Join(path, "frontend/AppRouter.jsx")
	os.WriteFile(file, []byte(content), 0644)

}
