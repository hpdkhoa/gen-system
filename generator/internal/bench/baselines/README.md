# genbench baselines

Frozen, reproducible benchmark numbers for gen-system's code-intelligence
(Understand) side, measured on real third-party legacy code. This directory is
the source of the single most fundable artifact: a resolution % + determinism
pass on a genuine COBOL codebase, frozen and regression-gated.

## The corpus

`cobolcraft_v23.24.json` was produced against **CobolCraft**
(`https://github.com/meyfa/CobolCraft`), a Minecraft server written entirely in
COBOL, **MIT licensed** (© 2024–2025 Fabian Meyer).

- Pinned commit: `c6f34f90ecdff94c3171c2b67334f9a5dc6acffb` (2026-03-08)
- ~30,300 lines of COBOL across 206 `.cob` programs + 62 `.cpy` copybooks
- Real-world constructs: `CALL`/`PERFORM`/`COPY` graph, `REDEFINES`, `LINKAGE`
  sections, a custom copybook-based unit-test framework, and one `.cpp` interop
  file (correctly skipped — no COBOL frontend).

The corpus is **not vendored** here; it is third-party code fetched on demand by
`freeze-cobol-baseline.sh` at the pinned commit, so the baseline stays
reproducible from source without redistributing someone else's repository.

Why CobolCraft and not a hand-written demo: it is the largest realistic,
clearly-licensed, publicly reproducible COBOL codebase available. A number
measured on a toy proves nothing; a number measured on 30k LOC of someone
else's COBOL is a claim an incubator or design partner can independently
re-run.

## What the numbers mean

Three deterministic, parser-path suites (no model, no GPU) are frozen:

### parse_throughput
- `files_total_cobol = 268`, `parse_errors = 0` — every COBOL file in the
  corpus parsed with zero errors. (269 files seen; the 1 non-COBOL `.cpp` file
  has no frontend and is skipped, not failed.)
- `files_per_sec_*` / `kb_per_sec_*` — throughput. These are wall-clock derived
  and therefore **environment-dependent**; they are zeroed in the frozen JSON
  and excluded from regression so a faster/slower box never trips a false drift.
  The counts, not the rates, are the contract.

### resolution_recall — the headline number
- `resolution_recall_internal = 1.000` — **100% of internal COBOL call edges
  resolved exactly** (302 / 302). This is the moat claim: symbols resolved at
  compiler accuracy, not heuristically.
- `call_edges_total = 1063`, `call_edges_internal = 302`,
  `call_edges_external = 761`.
- `resolution_recall_overall = 0.284` — this is **by design, not a weakness.**
  761 of 1063 edges are calls into the GnuCOBOL runtime / system intrinsics
  (`CALL "..."` targets that have no definition inside the corpus). The system
  classifies them as *external* rather than inventing a definition. Overall
  recall is low precisely because we refuse to hallucinate resolutions for
  symbols whose source we were never given — which is the human-in-the-loop,
  no-fabrication property the product sells. The number to quote is **internal
  recall (100%)** alongside the honest external-ratio (71.6%).
- `symbols_have_definedin` case passes — 0 symbols carry an empty `DefinedIn`,
  i.e. every resolved symbol traces to a real file:line.

### cfg_correctness
- `cfgs_total = 330` control-flow graphs built, `cfgs_with_issue = 0` — all four
  structural invariants (entry-in-range, exits-in-range, no-orphan-blocks,
  successors-in-range) hold across every CFG.

### Determinism
The parser path is **content-deterministic**: repeated runs produce
byte-identical cases + metrics (verified across repeated runs; only wall-clock
`duration_ns` and the throughput rates vary, which is why both are excluded from
the frozen contract). The frozen JSON pins canonical timestamps/host fields so
the file itself is stable in version control.

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

`--baseline` resolves `--old` to this directory's `cobolcraft_v23.24.json`
automatically (it walks up from `--root` / cwd). `--strict` makes a drift exit
non-zero so it can gate CI; without `--strict` it prints the diff and exits 0.

## Moving the baseline

Only when a parser change *deliberately* improves a number:

```bash
generator/internal/bench/baselines/freeze-cobol-baseline.sh freeze
git diff generator/internal/bench/baselines/cobolcraft_v23.24.json   # review
```

A baseline move should be a reviewed, intentional commit — never an incidental
side effect of another change. That is the whole point of freezing it.
