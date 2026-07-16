package ast

import (
	"os"
	"path/filepath"
)

func EnsureGoFile(path string, pkg string) {

	if _, err := os.Stat(path); err == nil {
		return // already exists
	}

	os.MkdirAll(filepath.Dir(path), 0755)

	content := "package " + pkg + "\n\n"

	os.WriteFile(path, []byte(content), 0644)
}
