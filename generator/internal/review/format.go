package review

import (
	"fmt"
	"strings"
)

var levelTitle = []struct{ key, title string }{
	{"break", "May break"},
	{"behavior", "New side effects"},
	{"test", "No test reaches these"},
	{"size", "Grew"},
}

// Markdown writes the report for a person: the points first, then one line per function.
func Markdown(r *Report) string {
	var b strings.Builder
	base := ""
	if r.Base != "" {
		base = " against " + r.Base
	}
	fmt.Fprintf(&b, "# Review%s\n\n%d files changed, %d functions touched, %d points.\n", base, r.Files, len(r.Functions), len(r.Points))
	for _, l := range levelTitle {
		var lines []string
		n := 0
		if l.key == "test" {
			// one line per file: the functions no test reaches
			byFile := map[string][]string{}
			var files []string
			for _, p := range r.Points {
				if p.Level == "test" {
					if byFile[p.File] == nil {
						files = append(files, p.File)
					}
					byFile[p.File] = append(byFile[p.File], p.Symbol)
					n++
				}
			}
			for _, f := range files {
				lines = append(lines, fmt.Sprintf("- %s: %s", f, strings.Join(byFile[f], ", ")))
			}
		} else {
			for _, p := range r.Points {
				if p.Level == l.key {
					lines = append(lines, fmt.Sprintf("- %s:%d %s", p.File, p.Line, p.Text))
					n++
				}
			}
		}
		if n > 0 {
			fmt.Fprintf(&b, "\n## %s (%d)\n\n%s\n", l.title, n, strings.Join(lines, "\n"))
		}
	}
	if len(r.Functions) > 0 {
		b.WriteString("\n## Functions\n\n| Function | Status | Callers outside | Tests | Touches |\n|---|---|---|---|---|\n")
		for _, f := range r.Functions {
			tests := fmt.Sprint(len(f.ReachedBy))
			if f.IsTest {
				tests = "is a test"
			}
			touches := strings.Join(f.EffectsAfter, ", ")
			if f.Status == "removed" {
				touches = "-"
			}
			fmt.Fprintf(&b, "| %s (%s:%d) | %s | %d | %s | %s |\n", f.Name, f.File, f.Line, f.Status, len(f.CallersOutside), tests, touches)
		}
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(&b, "\nNot parsed: %s\n", strings.Join(r.Skipped, ", "))
	}
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "\nNote: %s\n", n)
	}
	return b.String()
}
