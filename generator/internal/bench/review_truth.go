package bench

// The judge for the review benchmark: Go's own type checker. After an edit, every package that can call the edited
// package is checked with function bodies; the errors name the callers the edit broke. Packages that did not change
// come from compiled export data; the edited package and any package a test file needs come from source with bodies
// skipped, so a broken body elsewhere cannot hide an error here.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type goPkg struct {
	ImportPath   string
	Dir          string
	Name         string
	GoFiles      []string
	TestGoFiles  []string
	XTestGoFiles []string
	Imports      []string
	TestImports  []string
	XTestImports []string
	Deps         []string
	Export       string
}

type typeJudge struct {
	moduleDir string
	pkgs      map[string]*goPkg // module packages by import path
	byDir     map[string]*goPkg
	exports   map[string]string // every dependency's export data file
	baseline  map[string]map[string]bool
}

func goList(dir string, args ...string) ([]*goPkg, error) {
	cmd := exec.Command("go", append([]string{"list", "-e", "-json"}, args...)...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %v: %s", err, stderr.String())
	}
	var list []*goPkg
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		p := &goPkg{}
		if err := dec.Decode(p); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, nil
}

func newTypeJudge(moduleDir string) (*typeJudge, error) {
	j := &typeJudge{moduleDir: moduleDir, pkgs: map[string]*goPkg{}, byDir: map[string]*goPkg{}, exports: map[string]string{},
		baseline: map[string]map[string]bool{}}
	deps, err := goList(moduleDir, "-export", "-deps", "-test", "./...")
	if err != nil {
		return nil, err
	}
	for _, p := range deps {
		if p.Export != "" && !strings.Contains(p.ImportPath, " [") && !strings.HasSuffix(p.ImportPath, ".test") {
			j.exports[p.ImportPath] = p.Export
		}
	}
	own, err := goList(moduleDir, "./...")
	if err != nil {
		return nil, err
	}
	for _, p := range own {
		j.pkgs[p.ImportPath] = p
		j.byDir[filepath.Clean(p.Dir)] = p
	}
	return j, nil
}

// srcImporter imports the packages in src from source (bodies skipped), the rest from export data.
type srcImporter struct {
	j        *typeJudge
	fset     *token.FileSet
	gc       types.Importer
	src      map[string]bool
	withTest string // this package is imported with its _test.go files (an external test package needs them)
	cache    map[string]*types.Package
}

func (j *typeJudge) importer(src map[string]bool, withTest string) *srcImporter {
	fset := token.NewFileSet()
	gc := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		f, ok := j.exports[path]
		if !ok {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(f)
	})
	return &srcImporter{j: j, fset: fset, gc: gc, src: src, withTest: withTest, cache: map[string]*types.Package{}}
}

func (s *srcImporter) Import(path string) (*types.Package, error) { return s.ImportFrom(path, "", 0) }

func (s *srcImporter) ImportFrom(path, _ string, _ types.ImportMode) (*types.Package, error) {
	if p, ok := s.cache[path]; ok {
		return p, nil
	}
	gp := s.j.pkgs[path]
	if gp == nil || (!s.src[path] && path != s.withTest) {
		return s.gc.Import(path)
	}
	files := append([]string{}, gp.GoFiles...)
	if path == s.withTest {
		files = append(files, gp.TestGoFiles...)
	}
	asts, err := s.parse(gp.Dir, files)
	if err != nil {
		return nil, err
	}
	conf := types.Config{IgnoreFuncBodies: true, Importer: s, Error: func(error) {}}
	pkg, _ := conf.Check(path, s.fset, asts, nil)
	s.cache[path] = pkg
	return pkg, nil
}

func (s *srcImporter) parse(dir string, files []string) ([]*ast.File, error) {
	var out []*ast.File
	for _, f := range files {
		af, err := parser.ParseFile(s.fset, filepath.Join(dir, f), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		out = append(out, af)
	}
	return out, nil
}

// typeErr is one error: file (absolute) and line.
type typeErr struct {
	File string
	Line int
	Msg  string
}

// checkPkg type-checks one package (or its external test package) with bodies and returns its errors.
func (j *typeJudge) checkPkg(gp *goPkg, xtest bool, src map[string]bool) []typeErr {
	withTest := ""
	files := append(append([]string{}, gp.GoFiles...), gp.TestGoFiles...)
	path := gp.ImportPath
	if xtest {
		files = gp.XTestGoFiles
		path += "_test"
		withTest = gp.ImportPath
	}
	if len(files) == 0 {
		return nil
	}
	imp := j.importer(src, withTest)
	asts, err := imp.parse(gp.Dir, files)
	if err != nil {
		return []typeErr{{Msg: err.Error()}}
	}
	var errs []typeErr
	conf := types.Config{Importer: imp, Error: func(err error) {
		if te, ok := err.(types.Error); ok {
			p := te.Fset.Position(te.Pos)
			errs = append(errs, typeErr{File: filepath.Clean(p.Filename), Line: p.Line, Msg: te.Msg})
		}
	}}
	_, _ = conf.Check(path, imp.fset, asts, nil)
	return errs
}

// recordBaseline checks every package on the clean tree, so errors that exist before an edit are not counted.
func (j *typeJudge) recordBaseline() int {
	n := 0
	for _, gp := range j.pkgs {
		for _, xtest := range []bool{false, true} {
			key := gp.ImportPath
			if xtest {
				key += "_test"
			}
			base := map[string]bool{}
			for _, e := range j.checkPkg(gp, xtest, map[string]bool{}) {
				base[e.File+fmt.Sprint(e.Line)+e.Msg] = true
				n++
			}
			j.baseline[key] = base
		}
	}
	return n
}

// brokenBy checks the edited package and every package that imports it, and returns the errors the edit added.
func (j *typeJudge) brokenBy(editedDir string) []typeErr {
	ep := j.byDir[filepath.Clean(editedDir)]
	if ep == nil {
		return nil
	}
	// Every module package that depends on the edited one comes from source too, or its types would exist twice.
	src := map[string]bool{ep.ImportPath: true}
	for _, gp := range j.pkgs {
		if contains(gp.Deps, ep.ImportPath) {
			src[gp.ImportPath] = true
		}
	}
	var out []typeErr
	run := func(gp *goPkg, xtest bool) {
		key := gp.ImportPath
		if xtest {
			key += "_test"
		}
		base := j.baseline[key]
		for _, e := range j.checkPkg(gp, xtest, src) {
			if !base[e.File+fmt.Sprint(e.Line)+e.Msg] {
				out = append(out, e)
			}
		}
	}
	for _, gp := range j.pkgs {
		inMain := gp == ep || contains(gp.Imports, ep.ImportPath) || contains(gp.TestImports, ep.ImportPath)
		if inMain {
			run(gp, false)
		}
		if gp == ep || contains(gp.XTestImports, ep.ImportPath) {
			run(gp, true)
		}
	}
	return out
}

// checkable reports whether the type checker sees a file: a .go file of a module package (folders such as
// _examples and testdata are outside ./..., so an error there can never be reported).
func (j *typeJudge) checkable(file string) bool {
	gp := j.byDir[filepath.Dir(filepath.Clean(file))]
	if gp == nil {
		return false
	}
	b := filepath.Base(file)
	return contains(gp.GoFiles, b) || contains(gp.TestGoFiles, b) || contains(gp.XTestGoFiles, b)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
