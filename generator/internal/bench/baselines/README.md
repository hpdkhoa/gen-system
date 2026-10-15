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
- ~30,300 lines of COBOL: 632 programs in 206 `.cob` files, plus 62 `.cpy`
  copybooks. 97 files hold more than one program.
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
- `call_edges_total = 4512`: every `CALL`, `PERFORM` and intrinsic `FUNCTION`
  the parser found.
- `call_edges_internal = 4018`: calls to a program of the corpus, or to a
  paragraph or section of the caller's own program. All 4018 resolve
  (`resolution_recall_internal = 1.000`), to the exact program or paragraph.
- `call_edges_external = 494`: intrinsic functions (`TRIM`, `LENGTH`,
  `NUMVAL`), GnuCOBOL runtime routines (`CBL_OPEN_FILE` and the others), the C
  helpers in `cpp/` (sockets, gzip, directories), and `CALL` through a program
  pointer. None of these has a COBOL definition in the corpus, so the resolver
  leaves them unresolved and does not invent one.
- The `symbols_have_definedin` case passes: 0 symbols carry an empty `DefinedIn`,
  so every resolved symbol traces to a real file:line.

Recall here checks the resolver against the parser's own symbol index. It says
nothing about calls the parser never saw. For that I compared the edges with a
plain text scan of the source on 2026-10-15: of 3037 `CALL "..."` and
`PERFORM <paragraph>` lines, gen-system found 3023 at the same file and line.
The other 14 sit in the code generation templates in `codegen/templates/`,
which are fragments with placeholders, not programs, and two lines where the
scan mistook `PERFORM X(I) TIMES` for a call. That script is a one-off and not
in the repository yet. No other COBOL parser or compiler cross reference has
checked these edges yet.

### cfg_correctness
- `cfgs_total = 463` control-flow graphs, one per paragraph and section,
  `cfgs_with_issue = 0`. All four structural invariants (entry-in-range,
  exits-in-range, no-orphan-blocks, successors-in-range) hold across every CFG.
  A program with no paragraphs has no CFG yet; its calls still count above.

### Determinism
The parser path is **content-deterministic**. Repeated runs produce
byte-identical cases and metrics; I checked this across repeated runs. Only the
wall-clock `duration_ns` and the throughput rates vary, so the frozen contract
leaves both out. The frozen JSON pins canonical timestamps and host fields, so the
file itself stays stable in version control.

## Reproduce / verify on your box

From anywhere inside the repo:

```bash
# verify a fresh run matches the frozen baseline (exits 1 on any change)
generator/internal/bench/baselines/freeze-cobol-baseline.sh verify

# or, equivalently, by hand:
genbench parse      --root /path/to/CobolCraft --copybooks <dirs> --out p.json
genbench resolution --root /path/to/CobolCraft --copybooks <dirs> --out r.json
genbench cfg        --root /path/to/CobolCraft --copybooks <dirs> --out c.json
# (merge the three suites, then:)
genbench regression --baseline --new merged.json --strict --exact
```

`--baseline` points `--old` at this directory's `cobolcraft_v23.24.json` on its
own (it walks up from `--root` / cwd). `--strict` makes a drift exit non-zero.
`--exact` fails on any change in either direction and on a missing metric;
without it the check allows 5% drift and only in the worse direction. CI runs
the script on every push, with `--exact`.

## Moving the baseline

Any change to a count fails the check, even an improvement. Move the baseline on
purpose, in its own commit, and add a line to the history below:

```bash
generator/internal/bench/baselines/freeze-cobol-baseline.sh freeze
git diff generator/internal/bench/baselines/cobolcraft_v23.24.json   # review
```

## History

| Date | Internal calls | External calls | CFGs | Why it moved |
|---|---|---|---|---|
| frozen at v23.24 | 302 | 761 | 330 | First baseline. |
| 2026-09-29 | 294 | 1381 | 329 | `END-PERFORM.` no longer opens a false paragraph. Statements under `WHEN` and a second `COPY` of a book are no longer dropped, so 583 calls appeared. The baseline was not refrozen then, and the 5% gate hid part of the drift. |
| 2026-10-15 | 4018 | 494 | 463 | Every program of a file is indexed, not only the first: 97 files hold several. Statements before the first paragraph now belong to their program; before, their calls had no caller and did not count at all. Together, most of the 2,517 calls between CobolCraft's programs were either missing or counted as external. A `PERFORM` resolves only inside the caller's program; before, 66 of them resolved to a paragraph of another file. Calls in `ELSE` branches became edges. Paragraphs of the same name in one file no longer overwrite each other's CFG. |

The first two rows overstated the result: "100% internal recall" held only for
the calls the parser saw, and most calls between CobolCraft's own programs were
counted as runtime calls.
