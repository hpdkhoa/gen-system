package ast

import "strings"

// commonImports maps code patterns to their Go import path.
// Ordered: more specific patterns should be checked before generic ones.
var commonImports = []struct {
	pattern    string
	importPath string
}{
	{"fmt.", `"fmt"`},
	{"strings.", `"strings"`},
	{"errors.", `"errors"`},
	{"errors.New", `"errors"`},
	{"context.", `"context"`},
	{"time.", `"time"`},
	{"strconv.", `"strconv"`},
	{"sort.", `"sort"`},
	{"math.", `"math"`},
	{"math/rand", `"math/rand"`},
	{"encoding/json", `"encoding/json"`},
	{"json.", `"encoding/json"`},
	{"http.", `"net/http"`},
	{"net/http", `"net/http"`},
	{"log.", `"log"`},
	{"os.", `"os"`},
	{"io.", `"io"`},
	{"bytes.", `"bytes"`},
	{"sync.", `"sync"`},
	{"regexp.", `"regexp"`},
	{"filepath.", `"path/filepath"`},
	{"path/filepath", `"path/filepath"`},
	{"sql.", `"database/sql"`},
	{"database/sql", `"database/sql"`},
}

// AutoResolveImports scans newBody for usages of common standard-library
// packages and calls EnsureImport for each one detected.
// Called after every RewriteFunctionBody to prevent import-missing build failures.
//
// This covers ~90% of cases without needing golang.org/x/tools/imports.
func AutoResolveImports(filePath, newBody string) {
	seen := map[string]bool{}
	for _, entry := range commonImports {
		if seen[entry.importPath] {
			continue
		}
		if strings.Contains(newBody, entry.pattern) {
			EnsureImport(filePath, entry.importPath)
			seen[entry.importPath] = true
		}
	}
}
