package tmpl

import (
	"bytes"
	"os"
	"path/filepath"
	"text/template"
)

// RenderToFile renders a .tmpl file to outputPath.
// If outputPath already exists, it is not overwritten. This
// preserves any custom logic a developer has added.
// To force regeneration, delete the output file first.
func RenderToFile(templatePath string, outputPath string, data interface{}) error {

	// Skip if file already exists, to protect developer edits
	if _, err := os.Stat(outputPath); err == nil {
		return nil
	}

	t, err := template.ParseFiles(templatePath)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err = t.Execute(&buf, data); err != nil {
		return err
	}

	os.MkdirAll(filepath.Dir(outputPath), 0755)
	return os.WriteFile(outputPath, buf.Bytes(), 0644)
}

// RenderToFileForce renders a .tmpl file, overwriting any existing file.
// Use only when you explicitly want to regenerate from scratch.
func RenderToFileForce(templatePath string, outputPath string, data interface{}) error {

	t, err := template.ParseFiles(templatePath)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err = t.Execute(&buf, data); err != nil {
		return err
	}

	os.MkdirAll(filepath.Dir(outputPath), 0755)
	return os.WriteFile(outputPath, buf.Bytes(), 0644)
}
