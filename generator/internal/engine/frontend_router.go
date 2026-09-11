package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func genRoute(s schema.Entity, path string) {
	file := filepath.Join(path, "frontend/src/App.jsx")

	route := fmt.Sprintf(
		`<Route path="/%s" element={<%sPage />} />`,
		strings.ToLower(s.Name),
		s.Name,
	)

	importLine := fmt.Sprintf(
		`import %sPage from "./pages/%s";`,
		s.Name,
		s.Name,
	)

	appendIfMissing(file, importLine)
	appendIfMissing(file, route)

}

func appendIfMissing(path, line string) {
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), line) {
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
		defer f.Close()
		f.WriteString("\n" + line + "\n")
	}
}
