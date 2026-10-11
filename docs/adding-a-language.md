# Adding a language

This page shows how to teach the Understand half a new language, and what the benchmark and
verify tooling then do on their own. The Understand half is `ast → symbols → flow → belief`. Only
the first stage is language specific. The other three run over the unified IR and need no change.
The benchmark's resolution recall and the verify pass need no change either; they bucket by the
language a symbol carries.

This page names the files a new language touches, in the order to touch them, and the tests that
check each step. Paths are relative to `generator/`.

## 1. The contract

A frontend implements `ast.Frontend` in `internal/ast/frontend.go`:

```go
type Frontend interface {
    Language() Language           // canonical id, for example "go"
    Extensions() []string         // for example []string{".ts", ".tsx"}
    Available() bool              // external dependencies present on this machine
    Parse(filePath string, source []byte) (*Module, error)
    Emit(module *Module) ([]byte, error)
}
```

The contract's doc comments set three rules. Every frontend must follow them.

1. **A parse failure is a node, not an error.** Anything the parser cannot model becomes a
   `NodeUnknown` with an `UnknownReason` and the `RawSource` it covers. `Parse` returns an error
   only when it cannot read the file or the runtime the frontend needs is missing.
2. **An unavailable frontend still registers.** `Available()` says whether the machine can run
   it. The registry must know the language exists, so the tooling can report "skipped: runtime
   missing" instead of "unknown file".
3. **`Emit` returns source.** For a parse only language it returns `RawSource` unchanged; the
   Python and Java frontends do this. Rewriting is a separate, optional step (section 8).

## 2. The unified IR you emit

`internal/ast/unified_ir.go` defines the node kinds. Declarations: `module`, `function`, `method`,
`struct`, `class`, `interface`, `enum`, `type_alias`, `variable`, `constant`, `import`, `export`.
Statements: `call`, `return`, `assign`, `branch`, `loop`, `defer`, `try`, `escape`, `procedure`.
And `unknown`.

What each downstream stage needs from your nodes:

- **Symbols** (`internal/symbols/table.go`, `extractNode`) reads the declaration kinds and builds
  one symbol per node. Methods need `ReceiverType`; imports need `ImportPath`, and `ImportAlias`
  when the source names one. It skips kinds it does not know without a message, so a kind you
  invent produces no symbol.
- **Call edges** (`internal/symbols/index.go`, `extractCallEdges`) read every `NodeCall` under a
  function, method or procedure and take its `Callee` string as the edge name.
- **Flow** (`internal/flow/flow.go`, `buildFuncCFG`) reads `NodeCall`, `NodeBranch`, `NodeLoop`,
  `NodeReturn` and `NodeTry` from a function's `Children`, and `Else` on a branch. Any language
  that is not Go takes the structural path automatically. A body you could not parse may be one
  `NodeUnknown` with reason `body_not_parsed` that wraps the statements you did get; the CFG
  builder unwraps it.
- **Belief** (`internal/belief/`) is language independent. It groups routines by owner: the
  receiver for object languages, the program or package for flat ones. Whether `ReceiverType` is
  empty decides that grouping; the language name does not.

### The callee string is the one shape rule that matters

The resolver (`resolveEdgeLocked` in `internal/symbols/index.go`) treats a dotted callee as
qualified. It looks up the first segment among the caller file's imports, by alias or by last path
segment. If that finds an import path, it considers only symbols under that path. If it finds
nothing, it resolves the bare name with provenance tiers, project first. So:

- Render the callee by shape; do not copy the source text. The Go frontend's `calleeName` is the
  reference. `pkg.F` stays `pkg.F`. A chain such as `json.NewDecoder(r).Decode()` becomes
  `json.NewDecoder().Decode` without its argument lists. A call of an anonymous function is the
  constant `ast.FuncLiteralCallee`; the classifier counts it as literal and never resolves it.
- Put the import alias or the receiver variable in the first segment. That lets the resolver tell
  a project call from a standard library call. It is also why the resolution numbers in the
  benchmark are not 100 percent by construction.

The Python frontend does the same inside its embedded analysis script (`safe_name` in
`py_frontend.go`), and the TypeScript and Java frontends do it in their own walkers.

## 3. Register it

To register a frontend, add one `init()` in the frontend file, or a one line file beside it as
`cobol_register.go` and `java_register.go` do:

```go
func init() { Register(&myFrontend{}) }
```

`Register` panics on a duplicate `Language` and fills the extension table from `Extensions()`.
`ForFile`, `ForLanguage`, `All` and `ParseFile` then find the frontend. The parse cache
(`internal/ast/cache.go`, `ParseOrCache`) goes through `ForFile`, so `LoadContext` sees the
language with no further wiring.

Three extension lists outside the registry must still agree with yours:

| File | What it gates |
|---|---|
| `internal/ast/multi.go`, `DetectLanguage` | the rewrite path's language detection |
| `internal/context/context.go`, `isSourceFile` | whether `LoadContext` reads the file at all |
| `internal/watch/watch.go`, `isWatchedExt` | incremental rebuilds |

If you miss the second, the parser never runs, and the walk skips the file without a message. The
presentation package has a project listing in `project.go` that names languages for display. Add
yours there too.

## 4. The smallest complete template

Sizes at the time of writing:

| Frontend | Lines | Notes |
|---|---|---|
| `cagen_frontend.go` | 594 | pure Go, the smallest complete one; start here |
| `go_frontend.go` | 766 | native `go/ast`; the callee shape reference |
| `py_frontend.go` | 807 | shells out to `python3` with an embedded script; the external runtime reference |
| `java_frontend.go` | 1327 | pure Go, with a golden IR test |
| `ts_frontend.go` | 1652 | pure Go |
| `cobol_frontend.go` | 1707 | copybooks and procedures |
| `java_sidecar.go` | 80 | the external process contract, not registered |

If your language needs an external tool, follow the Python frontend. Probe with `exec.LookPath`
in `Available()`, cache the binary path, and run it per file with the source on stdin. Turn a
failed run into `NodeUnknown{UnknownReason: "analysis_script_failed"}` and do not return an error.
`java_sidecar.go` documents the same contract for a long lived process. Either way, the frontend
must not claim a language that another frontend already registered.

## 5. Standard library attachment

`--stdlib` and `GEN_STDLIB` (`internal/cli/stdlib_flag.go`) accept `auto`, `none`, a shorthand
such as `go` or `cobol`, or `<lang>=<dir>`. With `auto`, `DetectStdlibRoots` in
`internal/context/stdlib_detect.go` looks at the languages present in the index and attaches a
root per language: Go from `GOROOT/src`, COBOL from a synthetic catalog, Java from
`$JAVA_HOME` with `lib/src.zip`. It reports every other language as skipped, with the message to
pass `--stdlib <lang>=<dir>`. It skips Python, TypeScript and CA Gen on purpose.

A new language that wants automatic attachment adds a case to that switch and a detector. With
or without it, three things must line up:

- The extension must be in `isStdlibSourceFile` and `langFamilyForExt` in the same package.
  Otherwise the loader walks the stdlib tree and ingests nothing.
- Import paths must map to directories. `importMatchScore` in the symbols index counts trailing
  path segments of an import that a defining file's directory ends with. A language whose import
  syntax is not path shaped normalises it in the frontend or in `importPathToSubdir`. That is how
  Java's dotted packages become slashes.
- The loader adds stdlib symbols with external provenance (`AddModuleWithProvenance`), so a
  project symbol always wins a bare name tie. The invariant test in section 7 checks that
  attaching a stdlib never changes internal recall.

## 6. What the tooling does without you

`internal/bench/resolution.go` buckets every call edge by the caller symbol's language. It emits
`recall_internal_<lang>` plus a pass case `internal_recall_<lang>_at_least_85%` for each language
it sees. `internal/agent/verify_understand.go` uses the same `EdgeClassifier` as the benchmark,
so it checks a generated backend in your language for unresolved calls with the same rule.
Neither has per language code. The one harness special case is in `bench/run-understand.sh`: it
passes the `_copybooks` directories of COBOL targets as `--copybooks`. A language that needs an
extra input path adds a branch there.

To put a public repository of your language into the understand benchmark, add one entry to
`understand_targets` in `bench/tasks.json` with `name`, `url`, `commit` and `language`. The
driver clones it at that commit, checks that the checkout matches, and runs `genbench parse`,
`resolution` and `cfg` on it.

## 7. The tests you add

1. **Registry** (`internal/ast/frontend_test.go`): add the language to the four tables in
   `TestRegistry_ForLanguage`, `TestRegistry_ForFile`, `TestRegistry_DetectLanguage` and the
   required list in `TestRegistry_AllFrontends`.
2. **Frontend parse tests** (`internal/ast/<lang>_frontend_test.go`): one test per declaration
   kind you emit, and one for the callee shapes (the Go one is `go_callee_test.go`, whose
   `collectCallees` walks `Children` and `Else`). Add one for imports with and without alias, and
   one that feeds unparsable input and asserts an `unknown` node with a reason, not an error.
3. **Golden IR** (`internal/ast/golden_ir_java_test.go` is the model): lock what the frontend
   emits for one representative file.
4. **Golden CFG** (`internal/flow/golden_cfg_java_test.go` is the model): instantiate the
   frontend, skip if `!Available()`, `Parse`, `symbols.BuildFromModules`, `flow.Build`, and
   compare the block and edge shape for a branch, a loop, and a try.
5. **Stdlib invariant** (`internal/bench/resolution_stdlib_test.go`,
   `TestResolutionRecall_StdlibInvariant`): if you added a detector, the same repository must
   report equal internal recall with and without the stdlib. The stdlib symbols must be present,
   and edges must land in them. `internal/context/stdlib_java_test.go` is the template for a
   second language's stdlib.
6. **Boundary** (`internal/archtest/boundary_test.go`): nothing to write, but it limits where your
   code may live. Core understand packages (`ast`, `symbols`, `flow`, `compiler`, `objgraph`,
   `context`, `trace`, `presentation`) may not import any generation package (`ai`, `engine`,
   `agent`, `planner`, `rag`, `frontend` and the rest). A frontend lives in `internal/ast` and
   imports only core packages. No model calls, no network, and no external process on the default
   path except behind `Available()`.

Run `go build ./... && go test ./...` from `generator/`. Then `gofmt -l .` must print nothing.

## 8. Optional: rewriting

The generation half rewrites function bodies through `RewriteFunction` in `internal/ast/multi.go`.
Go and Python implement it today (Python through a `python3` subprocess). Java, TypeScript, CA Gen
and COBOL return `ErrRewriterNotImplemented`, and the README lists this as a known limit. A parse
only frontend is complete without it. If you add rewriting, add the language to `DetectLanguage` and `RewriteFunction`. Keep `Emit` byte exact for
unchanged modules, and add a round trip test that parses, emits, and compares.

## 9. A checklist

- [ ] `internal/ast/<lang>_frontend.go` implements the five methods and registers in `init()`
- [ ] parse failures produce `unknown` nodes with reasons; `Emit` returns `RawSource`
- [ ] the frontend renders callees by shape; the first segment is the import alias or receiver
- [ ] the frontend emits imports with path and alias
- [ ] add the extension to `DetectLanguage`, `isSourceFile`, `isWatchedExt`, and the project listing
- [ ] stdlib: add a detector or accept the skip message; if you attach one, update `isStdlibSourceFile` and `langFamilyForExt`
- [ ] tests 1 to 5 above; the boundary test stays green
- [ ] add one public repository at a pinned commit to `bench/tasks.json`
- [ ] update the README's supported languages line
