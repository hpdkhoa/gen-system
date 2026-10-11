# genbench baselines

This directory holds frozen, reproducible benchmark numbers for gen-system's
code-intelligence (Understand) side. The harness measured them on real third-party
legacy code. The main artifact here is a resolution % and a determinism pass on a
real COBOL codebase. This directory freezes both, and a regression check gates them.

## The corpus

`genbench` produced `cobolcraft_v23.24.json` against **CobolCraft**
(`https://github.com/meyfa/CobolCraft`), a Minecraft server written entirely in
COBOL, **MIT licensed** (© 2024 to 2025 Fabian Meyer).

- Pinned commit: `c6f34f90ecdff94c3171c2b67334f9a5dc6acffb` (2026-03-08)
- ~30,300 lines of COBOL across 206 `.cob` programs + 62 `.cpy` copybooks
- Real-world constructs: `CALL`/`PERFORM`/`COPY` graph, `REDEFINES`, `LINKAGE`
  sections, a custom copybook-based unit-test framework, and one `.cpp` interop
  file (the parser skips it because there is no frontend for it).

This repository does **not vendor** the corpus. `freeze-cobol-baseline.sh` fetches
the third-party code on demand at the pinned commit. You can reproduce the baseline
from source, and the repository does not redistribute someone else's code.

Why CobolCraft and not a hand-written demo: it is the largest realistic,
clearly-licensed, publicly reproducible COBOL codebase available. A number from a
toy shows little. A number from 30k LOC of someone else's COBOL is a claim an
incubator or design partner can re-run on their own.

## What the numbers mean

The baseline freezes three deterministic, parser-path suites (no model, no GPU):

### parse_throughput
- `files_total_cobol = 268`, `parse_errors = 0`: the parser read every COBOL file
  in the corpus with zero errors. (It saw 269 files. The 1 non-COBOL `.cpp` file
  has no frontend, so the suite counts it as skipped and not as failed.)
- `files_per_sec_*` / `kb_per_sec_*`: throughput. These come from wall-clock time,
  so they depend on the environment. The frozen JSON sets them to zero, and the
  regression check leaves them out, so a faster or slower box never trips a false
  drift. The contract covers the counts and not the rates.

### resolution_recall
- `resolution_recall_internal = 1.000`: the resolver resolves **100% of internal
  COBOL call edges** (302 / 302) to their exact definitions. It resolves these symbols at compiler
  accuracy and does not guess.
- `call_edges_total = 1063`, `call_edges_internal = 302`,
  `call_edges_external = 761`.
- `resolution_recall_overall = 0.284`. 761 of 1063 edges are calls into the
  GnuCOBOL runtime or system intrinsics (`CALL "..."` targets that have no
  definition inside the corpus). The system classifies them as *external* and does
  not invent a definition. Overall recall is low because the resolver refuses to
  make up a resolution for a symbol whose source it never received. Quote the
  **internal recall (100%)** together with the external ratio (71.6%).
- The `symbols_have_definedin` case passes: 0 symbols carry an empty `DefinedIn`,
  so every resolved symbol traces to a real file:line.

### cfg_correctness
- `cfgs_total = 330` control-flow graphs, `cfgs_with_issue = 0`. All four
  structural invariants (entry-in-range, exits-in-range, no-orphan-blocks,
  successors-in-range) hold across every CFG.

### Determinism
The parser path is **content-deterministic**. Repeated runs produce
byte-identical cases and metrics; I checked this across repeated runs. Only the
wall-clock `duration_ns` and the throughput rates vary, so the frozen contract
leaves both out. The frozen JSON pins canonical timestamps and host fields, so the
file itself stays stable in version control.

## Reproduce / verify on your box

From anywhere inside the repo:

```bash
# verify a fresh run matches the frozen baseline (exits 1 on drift)
generator/internal/bench/baselines/freeze-cobol-baseline.sh verify

# or, equivalently, by hand:
genbench parse      --root /path/to/CobolCraft --copybooks <dirs> --out p.json
genbench resolution --root /path/to/CobolCraft --copybooks <dirs> --out r.json
genbench cfg        --root /path/to/CobolCraft --copybooks <dirs> --out c.json
# (merge the three suites, then:)
genbench regression --baseline --new merged.json --strict
```

`--baseline` points `--old` at this directory's `cobolcraft_v23.24.json` on its
own (it walks up from `--root` / cwd). `--strict` makes a drift exit non-zero, so
it can gate CI. Without `--strict` it prints the diff and exits 0.

## Moving the baseline

Move it only when a parser change improves a number on purpose:

```bash
generator/internal/bench/baselines/freeze-cobol-baseline.sh freeze
git diff generator/internal/bench/baselines/cobolcraft_v23.24.json   # review
```

A baseline move should be its own reviewed commit. It should never be a side effect
of another change.
