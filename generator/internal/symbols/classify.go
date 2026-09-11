// generator/internal/symbols/classify.go — v26.34: one rule for what a call
// edge is, shared by the resolution benchmark and the Understand verify pass.
//
// Before this, each had its own rule. The benchmark called an edge internal
// when its bare callee name matched ANY symbol name, stdlib included, so a
// stdlib ingest moved the population. The verify pass called an edge a
// project defect unless it was one of 16 builtin names, so every stdlib call
// in a generated backend was reported. Both are now this:
//
//	literal   an anonymous function called in place; never a defect
//	external  resolved to a non project symbol, or unresolved and either
//	          package qualified through an import that holds no project
//	          symbol, or named after nothing any project symbol defines
//	internal  resolved to a project symbol, or unresolved while a project
//	          symbol carries that bare name and no import rules it out
//
// Internal recall is resolved internal over internal. It does not move when
// a standard library is attached, because a stdlib call is external either
// way: unresolved through its import before, resolved to a stdlib symbol
// after.
package symbols

import (
	"path/filepath"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/ast"
)

// EdgeClass is the population an edge belongs to.
type EdgeClass int

const (
	EdgeInternal EdgeClass = iota
	EdgeExternal
	EdgeLiteral
)

func (c EdgeClass) String() string {
	switch c {
	case EdgeInternal:
		return "internal"
	case EdgeExternal:
		return "external"
	default:
		return "literal"
	}
}

// EdgeClassifier snapshots the project side of an index once, so
// classifying thousands of edges stays linear.
type EdgeClassifier struct {
	idx          *Index
	projectNames map[string]bool
	projectDirs  []string
}

// NewEdgeClassifier builds the snapshot.
func (idx *Index) NewEdgeClassifier() *EdgeClassifier {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	c := &EdgeClassifier{idx: idx, projectNames: map[string]bool{}}
	dirs := map[string]bool{}
	for _, s := range idx.allSymbols {
		if s.Provenance != ProvProject || s.Kind == SymImport {
			continue
		}
		c.projectNames[s.Name] = true
		dirs[filepath.ToSlash(filepath.Dir(s.DefinedIn))] = true
	}
	for d := range dirs {
		c.projectDirs = append(c.projectDirs, d)
	}
	return c
}

// Classify returns the class of an edge and whether it resolved to a
// PROJECT symbol.
func (c *EdgeClassifier) Classify(e CallEdge) (EdgeClass, bool) {
	name := e.CalleeName
	if name == "" {
		return EdgeExternal, false
	}
	if strings.HasPrefix(name, ast.FuncLiteralCallee) {
		return EdgeLiteral, false
	}
	c.idx.mu.RLock()
	defer c.idx.mu.RUnlock()
	if e.CalleeID != "" {
		if callee, ok := c.idx.allSymbols[e.CalleeID]; ok && callee.Provenance == ProvProject {
			return EdgeInternal, true
		}
		return EdgeExternal, false
	}
	if dot := strings.Index(name, "."); dot >= 0 {
		if caller, ok := c.idx.allSymbols[e.CallerID]; ok {
			if importPath := c.idx.resolveImportPath(name[:dot], caller.DefinedIn); importPath != "" && !c.projectPackage(importPath) {
				return EdgeExternal, false
			}
		}
	}
	bare := name
	if dot := strings.LastIndex(bare, "."); dot >= 0 {
		bare = bare[dot+1:]
	}
	if c.projectNames[bare] {
		return EdgeInternal, false
	}
	return EdgeExternal, false
}

// projectPackage reports whether any project symbol lives under importPath.
func (c *EdgeClassifier) projectPackage(importPath string) bool {
	for _, d := range c.projectDirs {
		if dirMatchesImport(d+"/x", importPath) {
			return true
		}
	}
	return false
}
