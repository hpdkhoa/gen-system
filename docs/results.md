# Results

What I measured, how, and where the raw files are. The README keeps only the headline numbers.

## Parsers on four public repositories

I measured the parsers on 2026-10-15, on four public repositories pinned to a commit: 405 files,
268 of them COBOL, with 0 parse errors.

- **CobolCraft**, a Minecraft server written in COBOL: all 4,018 calls between its programs and
  paragraphs resolve. The 524 calls left go to intrinsic functions, the GnuCOBOL runtime and C
  helpers, which have no COBOL definition.
- **go-chi, gorilla/mux, spf13/cobra**: 98.0% to 99.9% of the calls between the project's own
  symbols resolve. The rest are calls through function values, which have no static target.
- The parsers built 1,697 control flow graphs, and none had an issue.

Measured again on 2026-10-20. The loader used to skip every source file over 64 KB without a
word, and three test files were that big (`mux_test.go`, cobra's `command_test.go` and
`completions_test.go`). The limit is now 512 KB, and a file past it shows in the scan log.
gorilla/mux now has 228 control flow graphs (was 158) and 99.92% recall (was 99.76%); spf13/cobra
has 595 (was 403) and 98.04% (was 98.00%). go-chi and CobolCraft did not change.

CI checks the CobolCraft counts on every push and fails on any change. The
[baseline README](../generator/internal/bench/baselines/README.md) explains each number.

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
