# Comparing gen-system with other COBOL tools

This folder diffs gen-system's COBOL call edges against other tools that read the same
files. The first reference is GnuCOBOL's compiler, `cobc`. CobolCraft, the corpus of the
frozen baseline, is written for GnuCOBOL, so cobc's view of a call is the ground truth there.

## What it compares

For each `CALL` and `PERFORM` statement:

1. **The site.** File, line, verb and target. Sites written in the file itself are matched one
   to one. Sites a `COPY` brings in are matched by file and target. gen-system dates them to the
   `COPY` line, while cobc dates them to the line in the copybook.
2. **The caller.** The program and the paragraph or section that make the call.
3. **The resolution.** A `PERFORM` must resolve to that paragraph in the same file. A `CALL` to
   a program the corpus defines must resolve to that program. A `CALL` to anything else, such as
   a runtime routine or a C helper, must stay unresolved.

gen-system also records intrinsic `FUNCTION` calls and `EXEC` blocks. Those are not `CALL` or
`PERFORM` statements, so the compare script counts them and leaves them out.

## How each tool reports its sites

| Tool | Script | How |
|---|---|---|
| gen-system | `genbench edges` | Every call edge as JSON lines: file, line, callee, calling program and paragraph, class, resolved symbol. |
| GnuCOBOL | `cobc_edges.py` | `cobc -C` turns each file into C. The C marks every statement with its source line and verb, names each `PERFORM` target and each `CALL` target, and marks each program and paragraph. |
| text scan | `textscan_edges.py` | A regular expression for `CALL "x"` and `PERFORM x`. It shares no code with the other two, and it misses calls through pointers and anything a `COPY` brings in. It is a floor, not a reference. |

`compare_cobol.py` takes gen-system's edges and one reference, and writes a Markdown report with
every difference and its source line, plus the full lists as JSON.

## Run it

```bash
bench/compare/run-cobolcraft.sh            # writes results/compare-cobolcraft/
STRICT=1 bench/compare/run-cobolcraft.sh   # exit 1 on any difference against cobc
```

It needs `cobc` (GnuCOBOL 3.x; `apt install gnucobol3` on Ubuntu), `python3`, `git`, and Go, or
`GENBENCH=/path/to/genbench`. It clones CobolCraft at the baseline's pinned commit. Set
`COBOLCRAFT_DIR` to use an existing checkout at that commit instead. It compiles each file with
the flags and include folders of CobolCraft's Makefile. The 28 code generation templates in
`codegen/templates/` are fragments with placeholders, not programs, so neither tool's view of
them is compared. CI runs it with `STRICT=1` on every push.

## Results

On 2026-10-15, with GnuCOBOL 3.1.2, on the 178 files cobc compiles:

| | cobc | gen-system | both |
|---|---|---|---|
| `CALL` and `PERFORM` sites written in the files | 3,054 | 3,054 | 3,054 |
| sites from copybooks, by file and target | 1,078 | 1,078 | 1,078 |

Both name the same calling program and paragraph on all 3,054 sites. Every `PERFORM` resolves to
its paragraph, and every `CALL` to one of CobolCraft's own programs resolves to that program
(2,514). 83 calls to runtime routines and C helpers stay unresolved, as they should. 34 calls go
through a pointer or a data item, so neither tool can name the target.

The first run did not agree. It found two gen-system bugs:

- The tokenizer chose fixed or free format line by line. A long free format line was cut at
  column 72, so `FUNCTION STORED-CHAR-LENGTH` became a call to `STORED-CHAR-LE`, and the calls
  after column 72 were lost. A line with `D` in column 7 was dropped as a debug line. The format
  is now decided once per file.
- `PERFORM TAGS-REGISTRY-TAG-LENGTH(I, J) TIMES` split its subscript over two tokens, so `TIMES`
  was not the third token and the loop count read as a paragraph.

The run also found bugs in the extractor itself, fixed before these numbers: calls to a program
of the same file, and system routines that cobc renames.

## Limits

- One corpus, one dialect. CobolCraft is free format GnuCOBOL. There are no numbers yet on IBM
  Enterprise COBOL, fixed format code with sequence numbers, or CICS and SQL heavy programs.
- cobc is a compiler, not a call graph tool. The comparison covers what its C output marks:
  `CALL` and `PERFORM` sites, their targets and their callers. It does not check data flow,
  control flow graphs or beliefs.
- Other parsers (ProLeap, Eclipse Che4z, tree-sitter-cobol) are not wired in yet. Each would be
  one more script that writes the same JSON lines as `cobc_edges.py`.
