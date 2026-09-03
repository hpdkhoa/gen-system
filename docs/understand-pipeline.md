# The Understand pipeline

From a directory on disk to a queryable model of the code. No model is involved at any step. The
entry point is `context.LoadContext(root)`, which delegates immediately to `LoadContextWithOpts`
(`generator/internal/context/context.go:199`).

Counted by distinct mechanism there are fifteen steps. Steps 1 to 11 run per file inside the
directory walk; 12 to 15 run once over the whole project.

## The steps

**1. Resolve the copybook configuration.** `cobolFrontendFor` (`context.go:688`) takes the
explicit option, else `GEN_COBOL_COPYBOOK_PATH`, else nothing. Returning nil here is meaningful:
with no copybook path configured, COBOL files fall through to the generic cached parse.

**2. Load the previous IR snapshot.** `compiler.LoadPreviousIR` (`context.go:219`), used for diffing.

**3. Walk and skip.** `filepath.Walk` (`context.go:232`) with `shouldSkip` (`context.go:652`),
which excludes `.git`, `.gen`, `node_modules`, `vendor`, `output`, `frontend`, `mobile`,
`design-manifest`, and `db/migrations`.

**4. Admit by extension, cap by size.** `isSourceFile` (`context.go:729`). Files over 64 KiB are
counted and skipped (`context.go:248`). Some admitted extensions have no frontend at all
(`.sql`, `.json`, `.tmpl`, `.js`); those are read into the file map and counted as
`filesNoFrontend` rather than failing.

**5. Read into `ctx.Files`,** keyed by path relative to the root (`context.go:253`).

**6. Go import pre-pass.** `analyzeGoFile` (`context.go:480`) parses with `parser.ImportsOnly` to
fill the package map and the Go portion of the import graph.

**7. Parse.** Either the copybook-aware COBOL frontend, or `DefaultCache.ParseOrCache`
(`context.go:271`). Caching is covered below.

**8. Build the symbol table.** `Index.AddModule` calls `BuildTable` (`symbols/table.go:62`).

**9. Extract call edges, unresolved.** `extractCallEdges` (`symbols/index.go:111`) records the
callee *name* as written and leaves `CalleeID` empty for later.

**10. Language-agnostic import graph.** `collectModuleImports` (`context.go:755`).

**11. Build the control-flow graph.** `flow.Build` (`flow/flow.go:65`).

**12. Infer effects.** `flow.InferEffects` (`flow/flow.go:564`).

**13. Ingest the standard library,** when asked. `DetectStdlibRoots` then `loadStdlibRoots`
(`context.go:332`). Deliberately after the project walk and before resolution.

**14. Resolve every call edge.** `Index.ResolveAll` (`symbols/index.go:166`).

**15. Build derived views.** `buildEntityUsage` (`context.go:510`) and `buildArchSummary`
(`context.go:540`).

Two things people assume are in this pipeline and are not: the index is never written to disk by
`LoadContext`, and dead-code analysis is not run here (see step 11 below).

## The six parsers

`ast.ForFile` (`ast/frontend.go:79`) maps extension to frontend through a registry that panics on
duplicate registration.

| Language | Extensions | Source |
|---|---|---|
| Go | `.go` | `ast/go_frontend.go` |
| COBOL | `.cob .cbl .cpy .cobol` | `ast/cobol_frontend.go` |
| CA Gen | `.ad .cagen .gel` | `ast/cagen_frontend.go` |
| Java | `.java` | `ast/java_frontend.go` |
| TypeScript | `.ts .tsx` | `ast/ts_frontend.go` |
| Python | `.py .pyw` | `ast/py_frontend.go` |

Five are pure Go with no external toolchain. **Python is the exception:** it shells out to
`python3` with an embedded analysis script, and when the interpreter is absent it emits a single
unknown node with the reason `python_runtime_unavailable` (`py_frontend.go:447`) rather than
failing the parse. If you are quoting offline or air-gapped behaviour, that is the one asterisk.

A Java bytecode frontend exists (`ast/java_sidecar.go`) but is not registered and reports itself
unavailable.

## Parse caching

Key is `filePath + "#" + sha256hex(bytes)` (`ast/cache.go:48`, hash at `cache.go:116`). Two tiers:
an in-memory map under a read-write mutex, and a JSON file per entry under `.gen/ast_cache`.

Three details that matter in practice:

- `DefaultCache` is created with the **relative** path `.gen/ast_cache` (`cache.go:207`), so the
  disk cache lands relative to the working directory, not the project root.
- `RawSource` is deliberately not persisted (`cache.go:144`), so a module restored from the disk
  cache cannot serve the lossless emit path.
- The COBOL branch in `LoadContextWithOpts` calls the frontend directly and **bypasses the cache
  entirely** (`context.go:271`). A copybook-aware parse is never cached.

## Explicit ignorance: `NodeUnknown`

The rule is stated in the IR header (`ast/unified_ir.go:13`): constructs outside the supported
subset become `NodeUnknown` with a reason, never a silent drop. When the kind is `NodeUnknown`,
both `RawSource` and `UnknownReason` are always populated (`unified_ir.go:193`).

There is no central policy function. Each frontend emits it inline at fifteen sites. The Go one:

```go
default:
    m.Nodes = append(m.Nodes, &Node{
        Kind:          NodeUnknown,
        Language:      LangGo,
        Span:          cv.span(decl.Pos(), decl.End()),
        RawSource:     extractSource(source, fset, decl.Pos(), decl.End()),
        UnknownReason: fmt.Sprintf("unrecognized top-level decl type %T", decl),
    })
```

Real reason strings in use include `copy_unresolved:<book>`, `copy_cycle:<book>`,
`unrecognized_statement: <token>`, `lambda or method reference not modelled`,
`python_syntax_error: ...`, `body_not_parsed`, and `destructuring_declaration`. They are surfaced
to the human by `Module.UnknownNodes()` (`unified_ir.go:246`) and rendered in the Studio.

`body_not_parsed` is load-bearing rather than cosmetic: the CFG builder matches that exact string
to decide when a body is opaque (`flow/cfg_ts_py.go:59`).

## Symbol IDs

```go
func SymbolID(filePath string, kind SymbolKind, qualName string) string {
	return filePath + "#" + string(kind) + ":" + qualName
}
```

`symbols/symbol.go:131`. For example
`backend/internal/user/handler.go#func:CreateUser`, or `#method:Repo.Save` for a method, where the
qualified name is `receiver.name`. Ten kinds exist: `func`, `method`, `type`, `var`, `const`,
`field`, `param`, `module`, `import`, `procedure`.

The path used is the absolute one from `Module.FilePath`, so IDs are machine-specific. That is
fine inside one run and worth knowing before comparing IDs across machines.

## Resolution and its priority

`ResolveAll` (`symbols/index.go:166`) is idempotent and only touches edges whose `CalleeID` is
still empty. `resolveByNameLocked` (`index.go:343`) strips any qualifier before the last dot, then
prefers, in order:

1. a project symbol that is exported
2. any project symbol
3. an exported external symbol (standard library or third party)
4. any external symbol

Unresolved edges are counted rather than guessed at. Two counters exist for different purposes:
`runUnderstandVerify` (`agent/verify_understand.go:111`) reports resolution over project-tier
callers during generation, and `ResolutionRecall` (`bench/resolution.go:43`) splits the population
into internal and external so the benchmark can report an honest internal recall alongside the
overall number.

**A determinism bug lived here, and it is worth knowing what it was.** Until v26.25 this function
ranged over `idx.allSymbols` directly and kept the first match in each tier. `allSymbols` is a map,
and Go randomizes map iteration, so when two symbols in the *same* tier shared a bare name, the
winner varied between runs on identical input. The call graph varied with it. Everything else in
the Understand half is deterministic by construction, which is exactly why this was easy to miss:
nothing downstream was random, so nothing downstream looked wrong.

The fix collects the candidates, sorts them by their unique and stable symbol ID, and only then
applies the tier preference. Tier order is unchanged.

**Then the same bug turned out to have a second home.** `resolveByNameLocked` was not the only
resolver. A second one, `resolveByName`, sat next to it and was reached from
`resolveCallEdges`, which is how `BuildFromModules` resolves an index — the path `cmd/bench` uses.
It read the symbol map through `ByName` in the same randomized order, and it had **no provenance
tier at all**, so a standard-library symbol could capture a project call there. That is precisely
the defect the v23.32 comment above describes as fixed. It had been fixed in one of the two paths.

The first round of tests did not catch it because they went through `ResolveAll`, which uses the
fixed resolver. A test can pass, prove nothing, and read as reassuring, which is the more useful
lesson than the bug itself.

v26.26 removes the second resolver outright rather than repairing it: `resolveCallEdges` now takes
the lock and calls the same `resolveByNameLocked` everything else uses. Two functions answering the
same question differently is how the divergence happened in the first place. `ByName` also sorts
before returning, because it is a public accessor whose result several callers walk in order,
including the derived-view builders in step 15.

Five tests in `index_determinism_test.go` pin all of it: an ambiguous name resolved identically
across fifty fresh indexes, the documented tie-break asserted directly, and stable call graphs
through **both** `ResolveAll` and `BuildFromModules`, plus a stable `ByName` ordering. Each one
fails against the code it replaced, which is the only reason to trust any of them.

**A footnote on the dead branch.** `projectExported` was declared and never assigned, so its arm of
the final switch could not execute. The reason is worth stating because it is the same shape of
mistake as the map ordering: an earlier edit added the tier variables, then optimized the
project-exported case into an early `return` inside the loop and left the now-unreachable branch
behind. The code read as though it implemented a four-way preference while implementing three. Both
bugs survived because the surrounding code *looked* right; neither was the kind of thing a compiler
or a passing test would tell you about. The variable is now assigned and its branch is live.

## Control-flow graphs

`flow.Build` (`flow/flow.go:65`) produces one CFG per function, method, or procedure, keyed by
symbol ID. Blocks carry a free-text `Label` rather than an enum: `entry`, `then`, `else`, `merge`,
`case`, `try_body`, `catch`, `try_merge`, `loop_header`, `loop_body`, `loop_exit`, and
`entry+body+exit` for a body that could not be decomposed.

Three lanes (`flow.go:97`): Go bodies with children get the full builder, non-Go bodies with
structural nodes get `buildStructuralCFG`, and anything else collapses to a single block.

Unreachable blocks are pruned by `pruneUnreachable` (`flow.go:159`) with survivors renumbered so
that index-based block lookup stays valid. Note that pruning is called **only** from the Go path
(`flow.go:140`).

Dead code is reported separately by `AnalyzeDataFlow` (`flow.go:504`), which flags any non-entry
block with no predecessors. Two honest caveats: it is not part of `LoadContext` at all, its only
caller being the `/api/present/flow` handler; and because Go CFGs are already pruned, it can only
fire on the structural languages. The `DataFlowIssue` doc comment mentions `use_before_def` and
`unread_var` kinds; no analysis produces them.

## Effect inference

`InferEffects` (`flow.go:564`) walks the AST rather than the CFG and matches callee names against
substring pattern lists for `db`, `fs`, and `network`. The conservative rule is the important part:

```go
if !efx.Effects[EffectDB] && !efx.Effects[EffectFS] && !efx.Effects[EffectNetwork] {
    if len(callees) == 0 {
        efx.Effects[EffectPure] = true
    } else {
        efx.Effects[EffectUnknown] = true
    }
}
```

`pure` requires zero calls. A routine that calls something the patterns do not recognise is
`unknown`, never `pure`. Effects are a set, so a routine can carry `db` and `network` at once.

Because it is substring matching on names, it is a heuristic and it is treated as one downstream:
this is exactly the inference the belief layer lets a human correct.

## A COBOL copybook, end to end

Copybooks are configured three ways, in precedence order: the `CopybookPath` option, the
`GEN_COBOL_COPYBOOK_PATH` environment variable (split on the platform list separator), or the
`--copybooks` flag on `understand-server`, `gen-server`, and `genbench`.

Expansion happens before structural parsing (`cobol_frontend.go:100`) so that line numbers stay
meaningful: copybook lines are inlined as if they had appeared at the `COPY` site, each carrying
the line number of that site.

Given a program:

```cobol
       IDENTIFICATION DIVISION.
       PROGRAM-ID. CUSTLOOKUP.
       DATA DIVISION.
       WORKING-STORAGE SECTION.
       COPY CUSTREC.
       PROCEDURE DIVISION.
       MAIN-PARA.
           PERFORM LOAD-CUSTOMER.
```

and `copybooks/CUSTREC.cpy`:

```cobol
       01 CUSTOMER-RECORD.
          05 CUST-ID    PIC 9(6).
          05 CUST-NAME  PIC X(40).
```

With the copybook path configured, `CUSTOMER-RECORD` is indexed as a struct with its fields, and
`ctx.Index.ByName("CUSTOMER-RECORD")` returns it. `findCopybook` (`cobol_frontend.go:333`) tries
the bare name, then each configured extension (`.cpy`, `.cbl`, `.cob`), then the lowercased form,
across each directory in order.

Without it, nothing fails. The `COPY` line is kept and an unknown node is attached with the reason
`copy_unresolved:CUSTREC`, while `MAIN-PARA` and `LOAD-CUSTOMER` are still indexed normally. A
copybook that includes itself is caught by a visited-set and reported as `copy_cycle:<book>`.
`COPY BOOK REPLACING ==PREFIX== BY ==WS-CUSTOMER==` is supported.

The worked test is `internal/context/cobol_wiring_test.go`, which exercises all three
configuration routes plus the unconfigured negative control.

## Why it is built this way

Every step above is deterministic and offline, with one narrow exception recorded honestly under
resolution. That is what lets the same engine verify the generated code afterwards: the Generate
half writes a backend, and the Understand half re-parses it and reports unresolved calls, orphan
operations, and read-named operations that write. A checker that shared the generator's
assumptions would not catch anything.
