package deps

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func WriteFrontendPackage(path string, profile string) error {

	v := Resolve(profile)

	pkg := map[string]interface{}{
		"name":    "frontend",
		"private": true,
		"version": "1.0.0",
		"scripts": map[string]string{
			"dev":   "vite",
			"build": "vite build",
		},
		"dependencies": map[string]string{
			"react":     v.Frontend["react"],
			"react-dom": v.Frontend["react-dom"],
		},
		"devDependencies": map[string]string{
			"vite":                 v.Frontend["vite"],
			"@vitejs/plugin-react": v.Frontend["@vitejs/plugin-react"],
		},
	}

	b, _ := json.MarshalIndent(pkg, "", "  ")

	os.MkdirAll(filepath.Join(path, "frontend"), 0755)
	return os.WriteFile(filepath.Join(path, "frontend/package.json"), b, 0644)

}
