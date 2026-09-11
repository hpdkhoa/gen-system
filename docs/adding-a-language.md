# Adding a language

How to teach the Understand half a new language, and what the benchmark and verify tooling then
do on their own. The Understand half is `ast → symbols → flow → belief`. Only the first stage is
language specific. The other three run over the unified IR and need no change. The benchmark's
resolution recall and the verify pass need no change either; they bucket by the language a
symbol carries.

This page names the files a new language touches, in the order to touch them, and the tests that
prove each step. Paths are relative to `generator/`.

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

Three rules from the contract's doc comments, and they are not optional.

1. **A parse failure is a node, not an error.** Anything the parser cannot model becomes a
   `NodeUnknown` with an `UnknownReason` and the `RawSource` it covers. `Parse` returns an error
   only when the file cannot be read or the runtime the frontend needs is missing.
2. **An unavailable frontend still registers.** `Available()` says whether the machine can run
   it. The registry must know the language exists so the tooling can report "skipped: runtime
   missing" instead of "unknown file".
3. **`Emit` returns source.** For a parse only language it returns `RawSource` unchanged; that is
   what the Python and Java frontends do. Rewriting is a separate, optional step (section 8).

## 2. The unified IR you emit

`internal/ast/unified_ir.go` defines the node kinds. Declarations: `module`, `function`, `method`,
`struct`, `class`, `interface`, `enum`, `type_alias`, `variable`, `constant`, `import`, `export`.
Statements: `call`, `return`, `assign`, `branch`, `loop`, `defer`, `try`, `escape`, `procedure`.
And `unknown`.

What each downstream stage needs from your nodes:

- **Symbols** (`internal/symbols/table.go`, `extractNode`) reads the declaration kinds and builds
  one symbol per node. Methods need `ReceiverType`; imports need `ImportPath`, and `ImportAlias`
  when the source names one. Kinds it does not know are skipped silently, so a kind you invent
  produces no symbol.
- **Call edges** (`internal/symbols/index.go`, `extractCallEdges`) read every `NodeCall` under a
  function, method or procedure and take its `Callee` string as the edge name.
- **Flow** (`internal/flow/flow.go`, `buildFuncCFG`) reads `NodeCall`, `NodeBranch`, `NodeLoop`,
  `NodeReturn` and `NodeTry` from a function's `Children`, and `Else` on a branch. Any language
  that is not Go takes the structural path automatically. A body you could not parse may be one
  `NodeUnknown` with reason `body_not_parsed` wrapping the statements you did get; the CFG
  builder unwraps it.
- **Belief** (`internal/belief/`) is language independent. It groups routines by owner: the
  receiver for object languages, the program or package for flat ones. That grouping is driven by
  whether `ReceiverType` is empty, not by the language name.

### The callee string is the one shape rule that matters

The resolver (`resolveEdgeLocked` in `internal/symbols/index.go`) treats a dotted callee as
qualified: the first segment is looked up among the caller file's imports by alias or last path
segment, and if that finds an import path, only symbols under that path are candidates. If it
finds nothing, the bare name is resolved with provenance tiers, project first. So:

- Render the callee by shape, not by copying source text. The Go frontend's `calleeName` is the
  reference: `pkg.F` stays `pkg.F`; a chain such as `json.NewDecoder(r).Decode()` becomes
  `json.NewDecoder().Decode` with argument lists stripped; a call of an anonymous function is the
  constant `ast.FuncLiteralCallee`, which the classifier counts as literal and never resolves.
- Put the import alias or the receiver variable in the first segment. That is what lets the
  resolver tell a project call from a standard library call, and it is why the resolution numbers
  in the benchmark are not 100 percent by construction.

The Python frontend does the same inside its embedded analysis script (`safe_name` in
`py_frontend.go`), the TypeScript and Java frontends in their own walkers.

## 3. Register it

Registration is one `init()` in the frontend file, or a one line file beside it as
`cobol_register.go` and `java_register.go` do:

```go
func init() { Register(&myFrontend{}) }
```

`Register` panics on a duplicate `Language`, and fills the extension table from `Extensions()`.
`ForFile`, `ForLanguage`, `All` and `ParseFile` then find the frontend, and the parse cache
(`internal/ast/cache.go`, `ParseOrCache`) goes through `ForFile`, so `LoadContext` sees the
language with no further wiring.

There are, however, three extension lists outside the registry that must agree with yours:

| File | What it gates |
|---|---|
| `internal/ast/multi.go`, `DetectLanguage` | the rewrite path's language detection |
| `internal/context/context.go`, `isSourceFile` | whether `LoadContext` reads the file at all |
| `internal/watch/watch.go`, `isWatchedExt` | incremental rebuilds |

Miss the second and the parser is never called; the file is skipped without a message. The
presentation package has a project listing in `project.go` that names languages for display and
should list yours too.

## 4. The smallest complete template

Sizes as of this writing:

| Frontend | Lines | Notes |
|---|---|---|
| `cagen_frontend.go` | 594 | pure Go, the smallest complete one; start here |
| `go_frontend.go` | 766 | native `go/ast`; the callee shape reference |
| `py_frontend.go` | 807 | shells out to `python3` with an embedded script; the external runtime reference |
| `java_frontend.go` | 1327 | pure Go, with a golden IR test |
| `ts_frontend.go` | 1652 | pure Go |
| `cobol_frontend.go` | 1707 | copybooks and procedures |
| `java_sidecar.go` | 80 | the external process contract, not registered |

If your language needs an external tool, follow the Python frontend: probe with `exec.LookPath`
in `Available()`, cache the binary path, run it per file with the source on stdin, and turn a
failed run into `NodeUnknown{UnknownReason: "analysis_script_failed"}` rather than an error.
`java_sidecar.go` documents the same contract for a long lived process. Either way the frontend
must not claim a language that is already registered.

## 5. Standard library attachment

`--stdlib` and `GEN_STDLIB` (`internal/cli/stdlib_flag.go`) accept `auto`, `none`, a shorthand
such as `go` or `cobol`, or `<lang>=<dir>`. With `auto`, `DetectStdlibRoots` in
`internal/context/stdlib_detect.go` looks at the languages present in the index and attaches a
root per language: Go from `GOROOT/src`, COBOL from a synthetic catalog, Java from
`$JAVA_HOME` with `lib/src.zip`. Every other language is reported as skipped with the message to
pass `--stdlib <lang>=<dir>`. Python, TypeScript and CA Gen are skipped on purpose.

A new language that wants automatic attachment adds a case to that switch and a detector. With
or without it, three things must line up:

- The extension must be in `isStdlibSourceFile` and `langFamilyForExt` in the same package, or
  the stdlib tree is walked and nothing is ingested.
- Import paths must map to directories. `importMatchScore` in the symbols index counts trailing
  path segments of an import that a defining file's directory ends with. A language whose import
  syntax is not path shaped normalises it in the frontend or in `importPathToSubdir`, which is
  how Java's dotted packages become slashes.
- Stdlib symbols are added with external provenance (`AddModuleWithProvenance`), so a project
  symbol always wins a bare name tie. The invariant test in section 7 checks that attaching a
  stdlib never changes internal recall.

## 6. What the tooling does without you

`internal/bench/resolution.go` buckets every call edge by the caller symbol's language and emits
`recall_internal_<lang>` plus a pass case `internal_recall_<lang>_at_least_85%` for each language
it sees. `internal/agent/verify_understand.go` uses the same `EdgeClassifier` as the benchmark,
so a generated backend in your language is checked for unresolved calls with the same rule. There
is no per language code in either. The one harness special case is in `bench/run-understand.sh`:
COBOL targets get their `_copybooks` directories passed as `--copybooks`. A language that needs
an extra input path adds a branch there.

To put a public repository of your language into the understand benchmark, add one entry to
`understand_targets` in `bench/tasks.json` with `name`, `url`, `commit` and `language`. The
driver clones it at that commit, checks the checkout matches, and runs `genbench parse`,
`resolution` and `cfg` on it.

## 7. The tests you add

1. **Registry** (`internal/ast/frontend_test.go`): add the language to the four tables in
   `TestRegistry_ForLanguage`, `TestRegistry_ForFile`, `TestRegistry_DetectLanguage` and the
   required list in `TestRegistry_AllFrontends`.
2. **Frontend parse tests** (`internal/ast/<lang>_frontend_test.go`): one test per declaration
   kind you emit, one for the callee shapes (the Go one is `go_callee_test.go`, whose
   `collectCallees` walks `Children` and `Else`), one for imports with and without alias, and one
   that feeds unparsable input and asserts an `unknown` node with a reason, not an error.
3. **Golden IR** (`internal/ast/golden_ir_java_test.go` is the model): lock what the frontend
   emits for one representative file.
4. **Golden CFG** (`internal/flow/golden_cfg_java_test.go` is the model): instantiate the
   frontend, skip if `!Available()`, `Parse`, `symbols.BuildFromModules`, `flow.Build`, and
   compare the block and edge shape for a branch, a loop, and a try.
5. **Stdlib invariant** (`internal/bench/resolution_stdlib_test.go`,
   `TestResolutionRecall_StdlibInvariant`): if you added a detector, the same repository loaded
   with and without the stdlib must report equal internal recall, stdlib symbols must be present,
   and edges must land in them. `internal/context/stdlib_java_test.go` is the template for a
   second language's stdlib.
6. **Boundary** (`internal/archtest/boundary_test.go`): nothing to write, but it constrains
   where your code may live. Core understand packages (`ast`, `symbols`, `flow`, `compiler`,
   `objgraph`, `context`, `trace`, `presentation`) may not import any generation package (`ai`,
   `engine`, `agent`, `planner`, `rag`, `frontend` and the rest). A frontend lives in
   `internal/ast` and imports only core packages. No model calls, no network, and no external
   process on the default path except behind `Available()`.

Run `go build ./... && go test ./...` from `generator/`, then `gofmt -l .` must print nothing.

## 8. Optional: rewriting

The generation half rewrites function bodies through `RewriteFunction` in `internal/ast/multi.go`.
Only Go implements it today; every other language returns `ErrRewriterNotImplemented`, which the
README lists as a known limit. A parse only frontend is complete without it. If you add
rewriting, add the language to `DetectLanguage` and `RewriteFunction`, keep `Emit` byte exact for
unchanged modules, and add a round trip test that parses, emits, and compares.

## 9. A checklist

- [ ] `internal/ast/<lang>_frontend.go` implements the five methods and registers in `init()`
- [ ] parse failures produce `unknown` nodes with reasons; `Emit` returns `RawSource`
- [ ] callees rendered by shape, first segment is the import alias or receiver
- [ ] imports emitted with path and alias
- [ ] extension added to `DetectLanguage`, `isSourceFile`, `isWatchedExt`, and the project listing
- [ ] stdlib: detector added or the skip message accepted; `isStdlibSourceFile` and `langFamilyForExt` updated if attached
- [ ] tests 1 to 5 above; the boundary test stays green
- [ ] one public repository at a pinned commit added to `bench/tasks.json`
- [ ] README's supported languages line updated
