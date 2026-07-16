// context/cobol_wiring_test.go — v19.5: verifies LoadContextWithOpts
// resolves COBOL copybooks via opts.CopybookPath and parses .cob/.cbl/.cpy
// files into the symbol index.
package context

import (
	"os"
	"path/filepath"
	"testing"
)

// helper: write file under root with content
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

const testCobolSrc = `       IDENTIFICATION DIVISION.
       PROGRAM-ID. CUSTLOOKUP.

       DATA DIVISION.
       WORKING-STORAGE SECTION.
       COPY CUSTREC.

       PROCEDURE DIVISION.
       MAIN-PARA.
           PERFORM LOAD-CUSTOMER
           STOP RUN.
       LOAD-CUSTOMER.
           DISPLAY 'loading'.
`

const testCustRecCopybook = `       01 CUSTOMER-RECORD.
          05 CUST-ID    PIC 9(6).
          05 CUST-NAME  PIC X(40).
          05 CUST-EMAIL PIC X(80).
`

// TestLoadContext_CobolFilesAreParsed verifies the existence of COBOL
// extensions in isSourceFile and that LoadContext returns a non-empty
// Index when a .cob file is present.
func TestLoadContext_CobolFilesAreParsed(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/program.cob", testCobolSrc)

	ctx := LoadContext(root)

	// Without copybook configuration, COPY CUSTREC is unresolved — but
	// the program itself is still parsed and its paragraphs indexed.
	syms := ctx.Index.ByName("MAIN-PARA")
	if len(syms) == 0 {
		t.Errorf("MAIN-PARA paragraph should be in the symbol index after LoadContext")
	}
	syms = ctx.Index.ByName("LOAD-CUSTOMER")
	if len(syms) == 0 {
		t.Errorf("LOAD-CUSTOMER paragraph should be in the symbol index after LoadContext")
	}
}

// TestLoadContextWithOpts_CopybookResolved verifies that opts.CopybookPath
// causes COPY directives to be resolved and the resulting fields to appear.
func TestLoadContextWithOpts_CopybookResolved(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/program.cob", testCobolSrc)
	writeFile(t, root, "copybooks/CUSTREC.cpy", testCustRecCopybook)

	ctx := LoadContextWithOpts(root, LoadContextOpts{
		CopybookPath: []string{"copybooks"},
	})

	// CUSTOMER-RECORD is defined inside the copybook; if resolution worked,
	// it must now appear in the symbol index.
	syms := ctx.Index.ByName("CUSTOMER-RECORD")
	if len(syms) == 0 {
		t.Errorf("CUSTOMER-RECORD from copybook should appear in Index when CopybookPath is set; got: no symbol")
	}
}

// TestLoadContextWithOpts_AbsoluteCopybookPath verifies absolute paths work.
func TestLoadContextWithOpts_AbsoluteCopybookPath(t *testing.T) {
	root := t.TempDir()
	bookDir := t.TempDir() // separate absolute dir
	writeFile(t, root, "src/program.cob", testCobolSrc)
	writeFile(t, bookDir, "CUSTREC.cpy", testCustRecCopybook)

	ctx := LoadContextWithOpts(root, LoadContextOpts{
		CopybookPath: []string{bookDir},
	})

	if len(ctx.Index.ByName("CUSTOMER-RECORD")) == 0 {
		t.Errorf("CUSTOMER-RECORD from absolute-path copybook should appear in Index")
	}
}

// TestLoadContextWithOpts_EnvVarFallback verifies that GEN_COBOL_COPYBOOK_PATH
// is honoured when opts.CopybookPath is empty.
func TestLoadContextWithOpts_EnvVarFallback(t *testing.T) {
	root := t.TempDir()
	bookDir := t.TempDir()
	writeFile(t, root, "src/program.cob", testCobolSrc)
	writeFile(t, bookDir, "CUSTREC.cpy", testCustRecCopybook)

	t.Setenv("GEN_COBOL_COPYBOOK_PATH", bookDir)

	ctx := LoadContextWithOpts(root, LoadContextOpts{})

	if len(ctx.Index.ByName("CUSTOMER-RECORD")) == 0 {
		t.Errorf("CUSTOMER-RECORD from copybook should appear via GEN_COBOL_COPYBOOK_PATH env var")
	}
}

// TestLoadContext_BackwardCompatibility verifies the original LoadContext
// signature still works (no opts argument) and produces a usable Context.
func TestLoadContext_BackwardCompatibility(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func DoStuff() string { return "ok" }
`)
	ctx := LoadContext(root)
	if len(ctx.Index.ByName("DoStuff")) == 0 {
		t.Errorf("LoadContext(root) must still index Go symbols; DoStuff not found")
	}
}
