package deps

import (
	"os"
	"path/filepath"
)

func WriteGoMod(path string, profile string) error {

	v := Resolve(profile)

	content := "module app\n\ngo 1.22\n\nrequire (\n"

	for k, ver := range v.Backend {
		content += "\t" + k + " " + ver + "\n"
	}

	content += ")\n"

	os.MkdirAll(filepath.Join(path, "backend"), 0755)
	return os.WriteFile(filepath.Join(path, "backend/go.mod"), []byte(content), 0644)

}
