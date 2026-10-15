// edges.go (v26.70): export every call edge gen-system finds, one JSON object
// per line, so an outside tool can diff them against another parser. The COBOL
// comparison in bench/compare/ reads this output next to the call sites that
// GnuCOBOL's cobc emits for the same files.
package bench

import (
	"encoding/json"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
	agentctx "github.com/hpdkhoa/gen-system/generator/internal/context"
	"github.com/hpdkhoa/gen-system/generator/internal/symbols"
)

// EdgeRecord is one call site as gen-system sees it. Paths are relative to the
// project root, with forward slashes.
type EdgeRecord struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Callee   string `json:"callee"`
	Language string `json:"lang"`
	CallerID string `json:"caller_id"`
	// COBOL only: the program and the paragraph or section that make the call.
	// A call from a program body before its first paragraph has no paragraph.
	CallerProgram   string `json:"caller_program,omitempty"`
	CallerParagraph string `json:"caller_paragraph,omitempty"`
	// Class is internal, external or literal, as the resolution benchmark counts.
	Class      string `json:"class"`
	ResolvedID string `json:"resolved_id,omitempty"`
}

// CallEdges loads root the way the other suites do and returns every call edge,
// sorted by file, line and callee.
func CallEdges(root string, copybookPath []string) []EdgeRecord {
	ctx := agentctx.LoadContextWithOpts(root, agentctx.LoadContextOpts{CopybookPath: copybookPath})
	if ctx.Index == nil {
		return nil
	}
	idx := ctx.Index
	classifier := idx.NewEdgeClassifier()
	rel := func(id string) string {
		i := strings.Index(id, "#")
		if i < 0 {
			return id
		}
		r, err := filepath.Rel(root, id[:i])
		if err != nil {
			return id
		}
		return filepath.ToSlash(r) + id[i:]
	}
	// The program of a single program COBOL file: its one module symbol.
	onlyProgram := map[string]string{}
	programOf := func(file string) string {
		if p, ok := onlyProgram[file]; ok {
			return p
		}
		name := ""
		if t, ok := idx.TableFor(file); ok {
			for _, s := range t.All() {
				if s.Kind == symbols.SymModule {
					if name != "" {
						name = ""
						break
					}
					name = s.Name
				}
			}
		}
		onlyProgram[file] = name
		return name
	}

	var out []EdgeRecord
	for _, e := range idx.AllCallEdges() {
		caller, ok := idx.ByID(e.CallerID)
		if !ok || caller.Provenance != symbols.ProvProject {
			continue
		}
		class, _ := classifier.Classify(e)
		r := EdgeRecord{
			Line:       e.Span.Start.Line,
			Callee:     e.CalleeName,
			Language:   string(caller.Language),
			CallerID:   rel(e.CallerID),
			Class:      class.String(),
			ResolvedID: rel(e.CalleeID),
		}
		if f, err := filepath.Rel(root, caller.DefinedIn); err == nil {
			r.File = filepath.ToSlash(f)
		}
		if caller.Language == ast.LangCOBOL {
			switch caller.Kind {
			case symbols.SymModule:
				r.CallerProgram = caller.Name
			case symbols.SymFunc, symbols.SymProcedure:
				r.CallerParagraph = caller.Name
				r.CallerProgram = caller.Program
				if r.CallerProgram == "" {
					r.CallerProgram = programOf(caller.DefinedIn)
				}
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Callee != b.Callee {
			return a.Callee < b.Callee
		}
		return a.CallerID < b.CallerID
	})
	return out
}

// WriteEdgesJSONL writes one EdgeRecord per line.
func WriteEdgesJSONL(w io.Writer, edges []EdgeRecord) error {
	enc := json.NewEncoder(w)
	for _, e := range edges {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}
