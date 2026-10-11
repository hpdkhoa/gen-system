// generator/internal/bench/localize.go (v26.31): localization recall on an
// arbitrary repository.
//
// Given seed identifiers (for example, names mentioned in a bug report) and the
// files a gold patch touched, load the repo through the Understand engine and
// ask one question: are the gold files inside the call graph neighbourhood of
// the seeds, within N hops?
//
// This is the measurement DeepDiscovery (arXiv 2606.22906) reports as file
// recovery and Full Recall Rate on SWE-bench Verified. It is not a solve rate.
// gen-system has no issue to patch pipeline, so nothing produced here is
// comparable to a SWE-bench solve rate. The driver under bench/swebench/ prints
// that boundary with every result.
//
// Deterministic by construction: seeds match symbols by exact name, expansion
// walks resolved call edges only (an unresolved edge is ignored, never
// guessed), and the neighbourhood is a set, so iteration order cannot change
// the answer.
package bench

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
)

// ReadLines returns the non-empty, trimmed lines of a text file. Used for the
// seed list and the gold file list, one entry per line.
func ReadLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if s := strings.TrimSpace(sc.Text()); s != "" && !strings.HasPrefix(s, "#") {
			out = append(out, s)
		}
	}
	return out, sc.Err()
}

// LocalizationRecall loads root, seeds the search with every symbol whose bare
// name matches a seed, expands over resolved call edges (both directions) up to
// maxHops, and reports at each hop how many gold files fall inside the reached
// set. Gold paths are relative to root, forward slashes.
func LocalizationRecall(root string, seeds, gold []string, maxHops int) Suite {
	t0 := time.Now()
	s := Suite{Name: "localization_recall"}
	if maxHops < 0 {
		maxHops = 0
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}

	ctx := agentctx.LoadContext(abs)
	if ctx.Index == nil {
		s.Cases = []Case{{Name: "index_built", Pass: false, Got: "nil index", Want: "index"}}
		s.Duration = time.Since(t0)
		return s
	}
	relOf := func(p string) string {
		r, err := filepath.Rel(abs, p)
		if err != nil {
			return filepath.ToSlash(p)
		}
		return filepath.ToSlash(r)
	}

	// Seeds → symbol IDs. Exact bare-name match, nothing fuzzy.
	visited := map[string]bool{}
	matched := 0
	for _, name := range seeds {
		syms := ctx.Index.ByName(name)
		if len(syms) > 0 {
			matched++
		}
		for _, sy := range syms {
			visited[sy.ID] = true
		}
	}

	// Undirected adjacency over resolved edges only.
	adj := map[string][]string{}
	resolved := 0
	for _, e := range ctx.Index.AllCallEdges() {
		if e.CallerID == "" || e.CalleeID == "" {
			continue
		}
		resolved++
		adj[e.CallerID] = append(adj[e.CallerID], e.CalleeID)
		adj[e.CalleeID] = append(adj[e.CalleeID], e.CallerID)
	}

	goldSet := map[string]bool{}
	for _, g := range gold {
		if g = filepath.ToSlash(strings.TrimSpace(g)); g != "" {
			goldSet[g] = true
		}
	}

	fileOf := func(id string) string {
		sy, ok := ctx.Index.ByID(id)
		if !ok || sy.DefinedIn == "" {
			return ""
		}
		return relOf(sy.DefinedIn)
	}

	frontier := map[string]bool{}
	for id := range visited {
		frontier[id] = true
	}
	finalHit := 0
	for hop := 0; hop <= maxHops; hop++ {
		files := map[string]bool{}
		for id := range visited {
			if f := fileOf(id); f != "" {
				files[f] = true
			}
		}
		hit := 0
		for g := range goldSet {
			if files[g] {
				hit++
			}
		}
		finalHit = hit
		recall := 0.0
		if len(goldSet) > 0 {
			recall = float64(hit) / float64(len(goldSet))
		}
		s.Metrics = append(s.Metrics,
			Metric{Name: fmt.Sprintf("hop%d_file_recall", hop), Value: recall, Unit: "ratio", HigherIsBetter: true},
			Metric{Name: fmt.Sprintf("hop%d_gold_files_hit", hop), Value: float64(hit), Unit: "files", HigherIsBetter: true},
			Metric{Name: fmt.Sprintf("hop%d_neighbourhood_files", hop), Value: float64(len(files)), Unit: "files"},
			Metric{Name: fmt.Sprintf("hop%d_neighbourhood_symbols", hop), Value: float64(len(visited)), Unit: "symbols"},
		)
		// Expand one hop. Sorted keys so the walk order is stable; the result is
		// a set either way, this just keeps traces reproducible.
		ids := make([]string, 0, len(frontier))
		for id := range frontier {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		next := map[string]bool{}
		for _, id := range ids {
			for _, n := range adj[id] {
				if !visited[n] {
					visited[n] = true
					next[n] = true
				}
			}
		}
		frontier = next
	}

	s.Metrics = append(s.Metrics,
		Metric{Name: "seeds_total", Value: float64(len(seeds)), Unit: "seeds"},
		Metric{Name: "seeds_matched", Value: float64(matched), Unit: "seeds", HigherIsBetter: true},
		Metric{Name: "gold_files_total", Value: float64(len(goldSet)), Unit: "files"},
		Metric{Name: "symbols_total", Value: float64(len(ctx.Index.AllSymbols())), Unit: "symbols"},
		Metric{Name: "call_edges_resolved", Value: float64(resolved), Unit: "edges"},
	)
	s.Cases = []Case{
		{Name: "gold_files_provided", Pass: len(goldSet) > 0, Got: fmt.Sprintf("%d", len(goldSet)), Want: ">=1"},
		{Name: "seeds_matched_at_least_one", Pass: matched > 0, Got: fmt.Sprintf("%d/%d", matched, len(seeds)), Want: ">=1"},
		{Name: fmt.Sprintf("gold_file_reached_within_%d_hops", maxHops), Pass: finalHit > 0,
			Got: fmt.Sprintf("%d/%d gold files", finalHit, len(goldSet)), Want: ">=1"},
	}
	s.Duration = time.Since(t0)
	return s
}
