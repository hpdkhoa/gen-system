# Results

What I measured, how, and where the raw files are. The README keeps only the headline numbers.

## Parsers on four public repositories

I measured the parsers on 2026-10-15, on four public repositories pinned to a commit: 405 files,
268 of them COBOL, with 0 parse errors.

- **CobolCraft**, a Minecraft server written in COBOL: all 4,018 calls between its programs and
  paragraphs resolve. The 524 calls left go to intrinsic functions, the GnuCOBOL runtime and C
  helpers, which have no COBOL definition.
- **go-chi, gorilla/mux, spf13/cobra**: see [Go calls against Go's type checker](#go-calls-against-gos-type-checker).
  The 98.0% to 99.9% this page showed before counted calls linked to *a* symbol, not to the right
  one.
- The parsers built 1,697 control flow graphs, and none had an issue.

Measured again on 2026-10-20. The loader used to skip every source file over 64 KB without a
word, and three test files were that big (`mux_test.go`, cobra's `command_test.go` and
`completions_test.go`). The limit is now 512 KB, and a file past it shows in the scan log.
gorilla/mux now has 228 control flow graphs (was 158), and spf13/cobra has 595 (was 403). go-chi
and CobolCraft did not change.

CI checks the CobolCraft counts on every push and fails on any change. The
[baseline README](../generator/internal/bench/baselines/README.md) explains each number.

## Go calls against Go's type checker

Measured on 2026-10-20. `genbench calls` type-checks every package of a Go module and asks Go
which function each call really reaches. Then it scores each edge of gen-system's call graph:

- **right**: it links that function;
- **wrong**: it links another one;
- **missed**: it links nothing;
- **outside, linked by mistake**: the call goes to the standard library or a dependency, but the
  edge links a project function.

Calls through an interface or a function value have no single target, so they are not scored.

| Repo | Project calls | Before: precision / recall | After: precision / recall | Outside calls linked by mistake, before / after |
|---|---|---|---|---|
| go-chi | 1,130 | 0.49 / 0.58 | 1.00 / 0.99 | 233 / 5 |
| gorilla/mux | 1,273 | 0.75 / 0.74 | 1.00 / 0.97 | 13 / 0 |
| spf13/cobra | 2,207 | 0.89 / 0.95 | 1.00 / 1.00 | 170 / 0 |
| gen-system | 7,490 | 0.80 / 0.87 | 1.00 / 1.00 | 704 / 10 |

Precision is right / (right + wrong + outside linked by mistake). Recall is right / project calls.

**A second correction.** This page and the README said 98.0% to 99.9% of internal Go calls
resolve. That number counted an edge as resolved when it linked any project symbol of that name.
The type checker shows that many of those links were wrong. On go-chi, 463 of 1,130 project calls
linked the wrong function, and `w.Write` on an `http.ResponseWriter` linked chi's own `Write`.

What changed in the resolver:

- A bare call `f()` stays in its own package.
- A method call follows the type of the value it is called on, when the code shows that type:
  - the receiver, a parameter, or a local (`x := &T{}`, `var x T`, `x := NewT()`, `x := y.Sub()`,
    `for _, x := range y.items`, a function literal's parameter);
  - through fields and method results along a chain (`c.Flags().String`).
- A type from outside the project gives no project target.
- When the type is not shown, only a single project method of that name may match. Two or more
  give no link instead of a guess.

The raw files are in `results/2026-10-20/calls/` (`before` is the resolver at the last commit).

```bash
genbench calls --out calls.json --md calls.md ./repo-a ./repo-b
```

## review_diff

Measured on 2026-10-20 with `genbench review` ([how it works](review.md#how-well-it-works)). Each
edit is made in a copy of the repo, and Go's type checker or `go test` gives the right answer.
20 cases per edit kind, seed 1.

| Repo | Signature change: callers found (precision / recall) | Removed function: callers found | File, network, database calls and new branches found | Harmless calls flagged | "No test reaches it" agrees with coverage |
|---|---|---|---|---|---|
| go-chi | 1.00 / 0.87 | 0.96 / 0.88 | 72 of 72 | 0 of 100 | 0.83 |
| gorilla/mux | 1.00 / 0.99 | 0.96 / 0.99 | 79 of 79 | 0 of 100 | 0.99 |
| spf13/cobra | 0.98 / 0.98 | 0.96 / 0.98 | 79 of 79 | 0 of 100 | 0.85 |
| gen-system | 1.00 / 1.00 | 1.00 / 1.00 | 76 of 76 | 0 of 100 | 0.81 |

- A harmless call is one that touches nothing, such as `cache.Get`, `list.Remove` or `len`. The
  effect cases skip functions that already had that effect.
- The test check looks 3 calls up. At 1 call, agreement falls to 0.51 to 0.72. At 6 calls, it is
  0.84 to 0.99, but it says "tested" more often for functions that are not (gen-system: 153
  functions instead of 70).
- Callers in files the type checker does not see (`_examples/`) are not counted either way.
- A review takes 0.07 to 0.16 s once the index is loaded, as in the MCP server. The terminal
  command loads the index first: under 1 s on chi, mux and cobra, 19 s on gen-system.
- On 20 real commits per repo, the median commit got 0 or 1 points. Between 9 and 16 of the 20
  commits got none. The most was 13 on chi and 38 on gen-system.

The raw files are in `results/2026-10-20/review/`. The `*.before-resolver-fix.*` files are the first
run, before the Go resolver changes above.

## COBOL against GnuCOBOL's compiler

`cobc` translates each CobolCraft file to C, which marks every `CALL` and `PERFORM` with its line,
its target and its caller. [`bench/compare/`](../bench/compare/README.md) diffs that with
gen-system's edges. On the 178 files cobc compiles, the two agree on:

- all 3,054 call sites written in the files;
- all 1,078 call sites that copybooks bring in;
- the calling program and paragraph of each site;
- every resolution.

The first run did not agree. It found two parser bugs, now fixed: free format lines were cut at
column 72, and `PERFORM X(I, J) TIMES` read the loop count as a paragraph. CI runs the comparison
on every push. It covers one corpus in one dialect, free format GnuCOBOL. There are no numbers yet
for IBM Enterprise COBOL. The raw edges and reports are in `results/compare-cobolcraft/`.

## A correction

An earlier version of the README said 100% internal recall for all four repositories. That run
missed most calls between CobolCraft's programs: the parser read only the first program in each
file. [The baseline history](../generator/internal/bench/baselines/README.md#history) lists what
changed and why.

## Localization on SWE-bench Verified

On 2026-09-09, on 60 SWE-bench Verified tasks, gen-system took the names in each issue and walked
two hops through the call graph. The walk reached 0.674 of the files the real fix touched, and a
median of 422 files per task. Zero hops already reach 0.615. That is localization recall, not a
solve rate: gen-system writes no fix. The raw files are in `results/2026-09-09/`.

## Generation

The 2026-09-09 campaign ran on one RTX 4060 Ti with 16 GB. Each campaign ends with
`results/<date>/ATTESTATION.md`, which records what ran, on which commit and machine, and which
runs the harness dropped and why. `MANIFEST.sha256` lists every raw file with its SHA-256.
[bench/README.md](../bench/README.md) explains each driver.

A service that compiles is a weak signal here. A body that fails the compile gate twice becomes a
stub, and stubs compile. Read the stub rate and the repair counts. The two models run one after
the other; swapping between them took about 2 seconds.

## Screenshots

![The Studio's Beliefs page for the COBOL payroll example: inputs and data written for each paragraph](img/studio-cobol-beliefs.png)

*The Beliefs page for `examples/payroll`. A COBOL paragraph has no parameters, so gen-system reads
its inputs and the data it writes from its statements: `CALC-TAX` reads the grade, the region, the
gross pay and the rate, and writes the tax and the rate. Each line has Verify and Correct.*

![The Studio's Architecture page for CobolCraft: 1,617 symbols in 268 files, all COBOL](img/studio-cobolcraft-architecture.png)

*The Architecture page for [CobolCraft](https://github.com/meyfa/CobolCraft). "Edge resolution
94%" counts every call edge, including 256 intrinsic calls that resolve to the built-in runtime
catalog. The calls left go to GnuCOBOL runtime routines, the C helpers and intrinsics the catalog
lacks. Every call between CobolCraft's own programs and paragraphs resolves.*
