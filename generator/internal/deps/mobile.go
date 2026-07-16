package deps

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func WriteMobilePackage(path string, profile string) error {

	v := Resolve(profile)

	pkg := map[string]interface{}{
		"name":    "mobile",
		"private": true,
		"version": "1.0.0",
		"main":    "node_modules/expo/AppEntry.js",
		"scripts": map[string]string{
			"start": "expo start",
		},
		"dependencies": v.Mobile,
	}

	b, _ := json.MarshalIndent(pkg, "", "  ")

	os.MkdirAll(filepath.Join(path, "mobile"), 0755)
	return os.WriteFile(filepath.Join(path, "mobile/package.json"), b, 0644)

}
