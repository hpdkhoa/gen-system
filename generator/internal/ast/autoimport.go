package ast

import (
	"errors"
	"strings"
)

// commonImports maps code patterns to their Go import path.
// v26.57 (D012): plain paths. EnsureImport adds the quote marks; the old
// table held quoted paths, so files got the line import ""fmt"" and broke.
// Ordered: more specific patterns should be checked before generic ones.
var commonImports = []struct {
	pattern    string
	importPath string
}{
	{"fmt.", "fmt"},
	{"strings.", "strings"},
	{"errors.", "errors"},
	{"errors.New", "errors"},
	{"context.", "context"},
	{"time.", "time"},
	{"strconv.", "strconv"},
	{"sort.", "sort"},
	{"math.", "math"},
	{"math/rand", "math/rand"},
	{"encoding/json", "encoding/json"},
	{"json.", "encoding/json"},
	{"http.", "net/http"},
	{"net/http", "net/http"},
	{"log.", "log"},
	{"os.", "os"},
	{"io.", "io"},
	{"bytes.", "bytes"},
	{"sync.", "sync"},
	{"regexp.", "regexp"},
	{"filepath.", "path/filepath"},
	{"path/filepath", "path/filepath"},
	{"sql.", "database/sql"},
	{"database/sql", "database/sql"},
}

// AutoResolveImports scans newBody for usages of common standard-library
// packages and calls EnsureImport for each one detected.
// Called after every RewriteFunctionBody to prevent import-missing build failures.
//
// This covers ~90% of cases without needing golang.org/x/tools/imports.
//
// v26.58 (D077): it returns the errors of EnsureImport, joined, so the caller
// can report an import that was not added. It still tries every import.
func AutoResolveImports(filePath, newBody string) error {
	seen := map[string]bool{}
	var errs []error
	for _, entry := range commonImports {
		if seen[entry.importPath] {
			continue
		}
		if strings.Contains(newBody, entry.pattern) {
			if err := EnsureImport(filePath, entry.importPath); err != nil {
				errs = append(errs, err)
			}
			seen[entry.importPath] = true
		}
	}
	return errors.Join(errs...)
}
