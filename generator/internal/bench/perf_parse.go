// generator/internal/bench/perf_parse.go (v19.7): cold parse throughput per
// language. Walks the project root, groups files by frontend, parses each
// group sequentially with a fresh in-memory ast.Cache (no disk persistence)
// to remove disk noise from the GPU-era runs.
package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

// ParseThroughput measures wall-clock cold parse rate per language across
// every source file under root. Per-language metrics: files/sec, kB/sec,
// total bytes, total files.
func ParseThroughput(root string) Suite {
	t0 := time.Now()
	s := Suite{Name: "parse_throughput"}

	// Bucket files by language.
	type fileEntry struct {
		path string
		size int64
		data []byte
	}
	groups := map[ast.Language][]fileEntry{}
	totalFiles, totalBytes := 0, int64(0)

	walkErr := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		// Skip large vendored/cached dirs.
		rel, _ := filepath.Rel(root, p)
		if shouldSkipForBench(rel) {
			return nil
		}
		fe := ast.ForFile(p)
		if fe == nil {
			return nil
		}
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		lang := ast.DetectLanguage(p)
		groups[lang] = append(groups[lang], fileEntry{path: p, size: info.Size(), data: data})
		totalFiles++
		totalBytes += info.Size()
		return nil
	})
	if walkErr != nil {
		s.Cases = []Case{{Name: "walk", Pass: false, Note: walkErr.Error()}}
		s.Duration = time.Since(t0)
		return s
	}

	if totalFiles == 0 {
		s.Cases = []Case{{Name: "files_found", Pass: false, Note: "no source files under " + root}}
		s.Duration = time.Since(t0)
		return s
	}

	// Parse each group with a fresh in-memory cache.
	langs := make([]ast.Language, 0, len(groups))
	for l := range groups {
		langs = append(langs, l)
	}
	sort.Slice(langs, func(i, j int) bool { return string(langs[i]) < string(langs[j]) })

	parseErrs := 0
	overallStart := time.Now()
	for _, lang := range langs {
		entries := groups[lang]
		fe := ast.ForLanguage(lang)
		if fe == nil {
			continue
		}
		langStart := time.Now()
		langBytes := int64(0)
		langOk := 0
		for _, ent := range entries {
			m, err := fe.Parse(ent.path, ent.data)
			if err == nil && m != nil {
				langOk++
				langBytes += ent.size
			} else {
				parseErrs++
			}
		}
		dur := time.Since(langStart).Seconds()
		if dur == 0 {
			dur = 1e-6
		}
		s.Metrics = append(s.Metrics,
			Metric{Name: fmt.Sprintf("files_per_sec_%s", lang), Value: float64(langOk) / dur, Unit: "files/s", HigherIsBetter: true},
			Metric{Name: fmt.Sprintf("kb_per_sec_%s", lang), Value: float64(langBytes) / 1024.0 / dur, Unit: "kB/s", HigherIsBetter: true},
			Metric{Name: fmt.Sprintf("files_total_%s", lang), Value: float64(langOk), Unit: "files", HigherIsBetter: true},
		)
	}
	overall := time.Since(overallStart).Seconds()
	if overall == 0 {
		overall = 1e-6
	}
	s.Metrics = append(s.Metrics,
		Metric{Name: "files_per_sec_overall", Value: float64(totalFiles) / overall, Unit: "files/s", HigherIsBetter: true},
		Metric{Name: "kb_per_sec_overall", Value: float64(totalBytes) / 1024.0 / overall, Unit: "kB/s", HigherIsBetter: true},
		Metric{Name: "parse_errors", Value: float64(parseErrs), Unit: "errors"},
	)

	s.Cases = []Case{
		{Name: "files_found", Pass: totalFiles > 0, Got: fmt.Sprintf("%d", totalFiles), Want: ">0"},
		{Name: "parse_errors_zero", Pass: parseErrs == 0, Got: fmt.Sprintf("%d", parseErrs), Want: "0"},
	}
	s.Notes = fmt.Sprintf("languages: %d, files: %d, bytes: %d", len(langs), totalFiles, totalBytes)
	s.Duration = time.Since(t0)
	return s
}

// shouldSkipForBench mirrors the project's general skip-list but is local
// to bench to avoid coupling to context's unexported shouldSkip.
func shouldSkipForBench(rel string) bool {
	if rel == "" || rel == "." {
		return false
	}
	skipPrefixes := []string{
		".git", ".gen", "node_modules", "vendor",
		"output", "build", "dist", ".idea", ".vscode",
	}
	for _, p := range skipPrefixes {
		if rel == p || hasPathPrefix(rel, p) {
			return true
		}
	}
	return false
}

func hasPathPrefix(rel, prefix string) bool {
	// matches "prefix" or "prefix/..." case-sensitively
	if len(rel) < len(prefix)+1 {
		return false
	}
	if rel[:len(prefix)] != prefix {
		return false
	}
	return rel[len(prefix)] == '/' || rel[len(prefix)] == filepath.Separator
}
