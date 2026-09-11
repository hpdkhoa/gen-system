package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
	"github.com/hpdkhoa/gen-system/generator/internal/tmpl"
)

// templateRoot returns the path to the templates/ directory.
// Priority:
//  1. GEN_TEMPLATES environment variable
//  2. Relative to the gen binary location (exe/../generator/internal/templates)
//  3. Hard fallback: gen-system/generator/internal/templates
func templateRoot() string {
	if v := os.Getenv("GEN_TEMPLATES"); v != "" {
		return v
	}
	// Resolve relative to binary location so it works from any cwd
	exe, err := os.Executable()
	if err == nil {
		// exe is typically <root>/gen
		// templates are at <root>/generator/internal/templates
		root := filepath.Join(filepath.Dir(exe), "generator", "internal", "templates")
		if _, err := os.Stat(root); err == nil {
			return root
		}
	}
	return "github.com/hpdkhoa/gen-system/generator/internal/templates"
}

func renderBackend(e schema.Entity, path string) {
	base := filepath.Join(templateRoot(), "backend")

	tmpl.RenderToFile(
		filepath.Join(base, "handler.tmpl"),
		filepath.Join(path, "backend/internal/handler/"+strings.ToLower(e.Name)+"_handler.go"),
		e,
	)

	tmpl.RenderToFile(
		filepath.Join(base, "service.tmpl"),
		filepath.Join(path, "backend/internal/service/"+strings.ToLower(e.Name)+"_service.go"),
		e,
	)

	tmpl.RenderToFile(
		filepath.Join(base, "repo.tmpl"),
		filepath.Join(path, "backend/internal/repository/"+strings.ToLower(e.Name)+"_repo.go"),
		e,
	)
}

func renderFrontend(e schema.Entity, path string) {
	base := filepath.Join(templateRoot(), "frontend")

	data := map[string]interface{}{
		"Name":  e.Name,
		"Lower": strings.ToLower(e.Name),
	}

	tmpl.RenderToFile(
		filepath.Join(base, "list.tmpl"),
		filepath.Join(path, "frontend/src/pages/"+e.Name+".jsx"),
		data,
	)
}

func renderMobile(e schema.Entity, path string) {
	base := filepath.Join(templateRoot(), "mobile")

	data := map[string]interface{}{
		"Name":  e.Name,
		"Lower": strings.ToLower(e.Name),
	}

	tmpl.RenderToFile(
		filepath.Join(base, "screen.tmpl"),
		filepath.Join(path, "mobile/src/screens/"+e.Name+"Screen.js"),
		data,
	)
}

// init registers the gen binary path for exec-based tools
func init() {
	_ = exec.Command
}
