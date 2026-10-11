# The Understand pipeline

The pipeline turns a directory on disk into a queryable model of the code. No step calls a model.
The entry point is `context.LoadContext(root)`, which hands off at once to `LoadContextWithOpts`
(`generator/internal/context/context.go:201`).

If you count distinct mechanisms, there are fifteen steps. Steps 1 to 11 run per file inside the
directory walk; 12 to 15 run once over the whole project.

## The steps

**1. Resolve the copybook configuration.** `cobolFrontendFor` (`context.go:690`) takes the
explicit option, else `GEN_COBOL_COPYBOOK_PATH`, else nothing. A nil return here has a meaning.
With no copybook path set, COBOL files fall through to the generic cached parse.

**2. Load the previous IR snapshot.** `compiler.LoadPreviousIR` (`context.go:221`). The pipeline uses it for diffing.

**3. Walk and skip.** `filepath.Walk` (`context.go:234`) with `shouldSkip` (`context.go:654`),
which excludes `.git`, `.gen`, `node_modules`, `vendor`, `output`, `frontend`, `mobile`,
`design-manifest`, and `db/migrations`.

**4. Admit by extension, cap by size.** `isSourceFile` (`context.go:731`). The walk counts and
skips files over 64 KiB (`context.go:250`). Some admitted extensions have no frontend at all
(`.sql`, `.json`, `.tmpl`, `.js`). The walk reads those into the file map and counts them as
`filesNoFrontend`; they do not fail.

**5. Read into `ctx.Files`,** with the path relative to the root as the key (`context.go:261`).

**6. Go import pre-pass.** `analyzeGoFile` (`context.go:482`) parses with `parser.ImportsOnly` to
fill the package map and the Go portion of the import graph.

**7. Parse.** Either the copybook-aware COBOL frontend, or `DefaultCache.ParseOrCache`
(`context.go:276`). The section on parse caching below covers the cache.

**8. Build the symbol table.** `Index.AddModule` calls `BuildTable` (`symbols/table.go:67`).

**9. Extract call edges, unresolved.** `extractCallEdges` (`symbols/index.go:117`) records the
callee *name* as written and leaves `CalleeID` empty for later.

**10. Language-agnostic import graph.** `collectModuleImports` (`context.go:757`).

**11. Build the control flow graph.** `flow.Build` (`flow/flow.go:65`).

**12. Infer effects.** `flow.InferEffects` (`flow/flow.go:641`).

**13. Ingest the standard library,** when asked. `DetectStdlibRoots` then `loadStdlibRoots`
(`context.go:336`). It runs after the project walk and before resolution, on purpose.

**14. Resolve every call edge.** `Index.ResolveAll` (`symbols/index.go:181`).

**15. Build derived views.** `buildEntityUsage` (`context.go:512`) and `buildArchSummary`
(`context.go:542`).

People assume two things are in this pipeline that are not. `LoadContext` never writes the index
to disk, and the pipeline does not run dead-code analysis (see step 11 below).

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

Five are pure Go with no external toolchain. Python is the exception. It shells out to
`python3` with an embedded analysis script. When the interpreter is absent, it emits a single
unknown node with the reason `python_runtime_unavailable` (`py_frontend.go:451`) and does not
fail the parse. If you quote offline or air-gapped behaviour, this is the one asterisk.

A Java bytecode frontend exists (`ast/java_sidecar.go`). Nothing registers it, and it reports
itself unavailable.

## Parse caching

The key is `filePath + "#" + sha256hex(bytes)` (`ast/cache.go:48`, hash at `cache.go:116`). Two tiers:
an in-memory map under a read-write mutex, and a JSON file per entry under `.gen/ast_cache`.

Three details that matter in practice:

- The code creates `DefaultCache` with the **relative** path `.gen/ast_cache` (`cache.go:207`).
  The disk cache lands relative to the working directory and not the project root.
- The cache does not persist `RawSource`, on purpose (`cache.go:144`). A module restored from the
  disk cache cannot serve the lossless emit path.
- The COBOL branch in `LoadContextWithOpts` calls the frontend directly and **skips the cache**
  (`context.go:273`). The cache never stores a copybook-aware parse.

## Explicit ignorance: `NodeUnknown`

The IR header states the rule (`ast/unified_ir.go:14`): constructs outside the supported subset
become `NodeUnknown` with a reason. A frontend never drops them silently. When the kind is
`NodeUnknown`, the frontend always fills both `RawSource` and `UnknownReason`
(`unified_ir.go:196`).

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
`python_syntax_error: ...`, `body_not_parsed`, and `destructuring_declaration`.
`Module.UnknownNodes()` (`unified_ir.go:248`) passes them to the human, and the Studio shows them.

The code depends on `body_not_parsed`. The CFG builder matches that exact string to decide when a
body is opaque (`flow/cfg_ts_py.go:59`).

This policy has outside support. A 2026 review of 87 graph based vulnerability studies
(Information 17(8):730) lists graph construction errors as a main reason reported results do not
hold up. It also asks for uncertainty aware graph construction. An unknown node that carries its
reason applies that idea. The graph records what it could not model. It does not guess
a structure that looks plausible.

## Symbol IDs

```go
func SymbolID(filePath string, kind SymbolKind, qualName string) string {
	return filePath + "#" + string(kind) + ":" + qualName
}
```

`symbols/symbol.go:136`. For example
`backend/internal/user/handler.go#func:CreateUser`, or `#method:Repo.Save` for a method, where the
qualified name is `receiver.name`. There are ten kinds: `func`, `method`, `type`, `var`, `const`,
`field`, `param`, `module`, `import`, `procedure`.

The ID uses the absolute path from `Module.FilePath`, so IDs are machine-specific. That is fine
inside one run. Keep it in mind before you compare IDs across machines.

## Resolution and its priority

`ResolveAll` (`symbols/index.go`) is idempotent and only touches edges whose `CalleeID` is
still empty. Since v26.34 every edge goes through `resolveEdgeLocked` first. When the callee is
package qualified and its first segment is an import alias in the caller's file, the resolver
considers only symbols defined under that import path. If the package holds none, the edge stays
unresolved. This rule stops a project function named `New` from capturing `errors.New`. A callee
whose first segment is not an import (a receiver variable, `s.store.Get`) falls through to
`resolveByNameLocked`. That function strips the qualifier and prefers, in order:

1. an exported project symbol
2. any project symbol
3. an exported external symbol (standard library or third party)
4. any external symbol

The Go frontend names a callee by the shape of the call and not by its source text (v26.34).
It names an anonymous function called in place `func literal`, and that edge never resolves. A
chained call keeps its receiver chain without argument lists, `json.NewDecoder().Decode`, so the
first segment is always the identifier the resolver needs.

The resolver counts unresolved edges and does not guess. One rule in `symbols/classify.go` decides
what an edge is, and both counters use it. An edge is one of three things. A literal is an
anonymous function called in place. An external edge resolved to a non project symbol. Or it is
unresolved and package qualified through an import that holds no project symbol. Or its name
matches nothing any project symbol defines. Everything else is internal. Internal recall
is resolved internal over internal. It does not move when you attach a standard library. A
stdlib call is external either way: before, it is unresolved through its import; after, it
resolves to a stdlib symbol. `runUnderstandVerify` (`agent/verify_understand.go`) reports it
during generation and lists only internal unresolved edges as findings. `ResolutionRecall`
(`bench/resolution.go`) reports it for the benchmark next to the overall number and the external
and literal counts.

Before v26.34 the verify pass had its own rule, sixteen builtin names. It reported every stdlib
call in a generated backend as an unresolved project symbol: 234 of 287 edges, an 18.5 percent
"recall", and a halt in stop mode over nothing. The generation path also never attached a
standard library. It now loads with stdlib auto detection (`agent.LoadOpts`, `--stdlib` on the
gen CLI, `GEN_STDLIB`), the same detection understand-server uses.

**A determinism bug lived here.** Until v26.25 this function
ranged over `idx.allSymbols` directly and kept the first match in each tier. `allSymbols` is a map
and Go randomizes map iteration. So when two symbols in the *same* tier shared a bare name, the
winner varied between runs on identical input. The call graph varied with it. Everything else in
the Understand half is deterministic by construction. That made this bug easy to miss: nothing
downstream was random, so nothing downstream looked wrong.

The fix collects the candidates, sorts them by their unique and stable symbol ID, and only then
applies the tier preference. The tier order stays the same.

**The same bug had a second home.** `resolveByNameLocked` was not the only resolver. A second
one, `resolveByName`, sat next to it. `resolveCallEdges` called it, and `resolveCallEdges` is how
`BuildFromModules` resolves an index. That is the path `cmd/bench` uses. It read the symbol map
through `ByName` in the same randomized order. It also had **no provenance tier at all**, so a
standard library symbol could capture a project call there. That is the defect the v23.32 comment
above describes as fixed. The fix covered only one of the two paths.

The first round of tests did not catch it because they went through `ResolveAll`, which uses the
fixed resolver. A passing test can prove nothing and still look reassuring.

v26.26 removes the second resolver instead of repairing it. `resolveCallEdges` now takes the lock
and calls the same `resolveByNameLocked` everything else uses. Two functions that answered the
same question differently caused the divergence in the first place. `ByName` also sorts before it
returns. It is a public accessor, and several callers walk its result in order, including the
derived-view builders in step 15.

Five tests in `index_determinism_test.go` pin all of it. They cover an ambiguous name that
resolves the same way across fifty fresh indexes, a direct assertion of the documented tie-break,
stable call graphs through **both** `ResolveAll` and `BuildFromModules`, and a stable `ByName`
ordering. Each one fails against the code it replaced.

**A footnote on the dead branch.** The code declared `projectExported` and never assigned it, so
its arm of the final switch could not execute. The cause has the same shape as the map ordering
mistake. An earlier edit added the tier variables. Then it turned the project-exported case into
an early `return` inside the loop and left the now-unreachable branch behind. The code read like a
four-way preference but implemented three. Both bugs survived because the surrounding code
*looked* right. A compiler or a passing test would not tell you about either of them. The code now
assigns the variable, and its branch is live.

## Control-flow graphs

`flow.Build` (`flow/flow.go:65`) produces one CFG per function, method, or procedure, keyed by
symbol ID. Blocks carry a free-text `Label` rather than an enum: `entry`, `then`, `else`, `merge`,
`case`, `try_body`, `catch`, `try_merge`, `loop_header`, `loop_body`, `loop_exit`, and
`entry+body+exit` for a body the builder could not split into blocks.

Three lanes (`flow.go:99`): Go bodies with children get the full builder, non-Go bodies with
structural nodes get `buildStructuralCFG`, and anything else collapses to a single block.

`pruneUnreachable` (`flow.go:161`) removes unreachable blocks and renumbers the survivors, so
index-based block lookup stays valid. Two builders call it: the Go path (`flow.go:142`) and the
CA Gen procedure builder `buildProcedureCFG` (`flow.go:488`). The others never prune.

`AnalyzeDataFlow` (`flow.go:569`) reports dead code separately. It flags any non-entry block with
no predecessors. It is not part of `LoadContext` at all. Its only caller is the
`/api/present/flow` handler. Go CFGs come out already pruned, so it can only fire on the
structural languages. The `DataFlowIssue` doc comment mentions `use_before_def` and
`unread_var` kinds; no analysis produces them.

## Effect inference

`InferEffects` (`flow.go:641`) walks the AST rather than the CFG and matches callee names against
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

It matches substrings of names, so it is a heuristic, and the code downstream treats it as one.
The belief layer lets a human correct this inference.

## A COBOL copybook, step by step

You can set the copybook path three ways, in precedence order: the `CopybookPath` option, the
`GEN_COBOL_COPYBOOK_PATH` environment variable (split on the platform list separator), or the
`--copybooks` flag on `understand-server`, `gen-server`, and `genbench`.

The frontend expands copybooks before structural parsing (`cobol_frontend.go:103`), so line
numbers keep their meaning. It inlines copybook lines as if they had appeared at the `COPY` site,
and each line carries the line number of that site.

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

With the copybook path set, the index holds `CUSTOMER-RECORD` as a struct with its fields, and
`ctx.Index.ByName("CUSTOMER-RECORD")` returns it. `findCopybook` (`cobol_frontend.go:338`) tries
the bare name, then each configured extension (`.cpy`, `.cbl`, `.cob`), then the lowercased form,
across each directory in order.

Without it, nothing fails. The frontend keeps the `COPY` line and attaches an unknown node with
the reason `copy_unresolved:CUSTREC`. The index still holds `MAIN-PARA` and `LOAD-CUSTOMER` as
normal. A visited-set catches a copybook that includes itself, and the frontend reports it as
`copy_cycle:<book>`. The frontend supports `COPY BOOK REPLACING ==PREFIX== BY ==WS-CUSTOMER==`.

The worked test is `internal/context/cobol_wiring_test.go`, which exercises all three
configuration routes plus the unconfigured negative control.

## Why the pipeline works this way

Every step above is deterministic and offline, except one narrow case that this page records under
resolution. That is what lets the same engine verify the generated code afterwards: the Generate
half writes a backend, and the Understand half re-parses it and reports unresolved calls, orphan
operations, and read-named operations that write. A checker that shared the generator's
assumptions would not catch anything.
