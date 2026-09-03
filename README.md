# gen-system

**A code-understanding engine that shows you what it believes about your code, lets you correct
it, and generates only under gates it cannot talk its way past.**
Two models, one 16 GB GPU, fully offline, no token cost.

---

## Why this exists

AI code tools generate first and let you discover afterwards whether they understood your
codebase. gen-system reverses the order:

1. **Understand** the source deterministically: AST, symbol index, call graph, control-flow graph,
   down to standard-library level. Six hand-written offline parsers. No model involved.
2. **Print what it believes** about the code it plans to touch: inputs, callees, inferred effects,
   what a routine appears to do. You read it and correct it.
3. **Generate** from the corrected belief. Only beliefs a human verified are allowed to steer.
4. **Gate** the output: every model-written operation compiles alone before it can enter the
   service file, then `go vet`, `go build`, and `go test` must pass, then the Understand half
   re-parses the result and reports what it finds.
5. **Repeat** and get the same output. Temperature 0, fixed seed, verified by a determinism suite.

## What makes it different

| | Typical local AI tools | gen-system |
|---|---|---|
| Code model | Text chunks and embeddings | Deterministic AST, call graph and CFG, rendered as flowcharts for human review |
| What it thinks | Hidden | Printed, correctable, and gated against what the parser observed |
| Output acceptance | A diff, you decide | Per-operation compile gate, then vet, build and test |
| Reproducibility | Best effort | Hard contract: temperature 0, seed 42, determinism suite |
| Hardware target | Whatever you have | One 16 GB consumer GPU, on purpose |

One clarification, because the first draft of this README got it wrong: the flowcharts are
rendered **for you**, not fed to the model. Nothing today uses a rendered flowchart as retrieval
data. Whether it would help is an open experiment, listed under Future work.

## Quickstart

```bash
git clone [TODO repo url]
cd gen-system
./install.sh                    # Go, Ollama, model pulls, optional Postgres and Node
```

`./install.sh --understand` installs the read-only comprehension half with no model dependency,
which is the right choice for an air-gapped review of an existing codebase.

Default models, both pulled by the installer:

```bash
ollama pull qwen3:14b           # planning role
ollama pull qwen2.5-coder:14b   # code role
```

Check the machine before trusting a number from it:

```bash
gen doctor                      # is Ollama up, does a model stay resident between calls
```

Read an existing codebase:

```bash
understand-server --root ./your-repo    # comprehension API plus the Studio UI
```

Beliefs land in `./your-repo/.gen/beliefs.json`. Correct one in the Studio, or POST to
`/api/present/beliefs/correct`. Your correction wins from then on, and if the source later changes
underneath it the belief is flagged out of date with both values shown: what you said, and what
the source says now.

Generate a new service:

```bash
gen idea --out ./generated-app "a library lending service with members, books and loans"
```

## Benchmarks

The harness, the frozen task set, and the drivers are in `bench/`. The task set is committed
before any run, so it cannot be tuned to the results.

```bash
./bench/freeze-env.sh           # record commit, GPU, driver, model list
./bench/run-all.sh              # the full campaign
python3 bench/summarise.py --results results/<date>
```

**Numbers are not reproduced in this README.** They live in `benchmarks/results/measured.json` in
the portfolio repo, each row carrying the date, the commit, the task set, n, and a range. A README
table is a copy that goes stale the first time you re-run anything.

What the campaign measures: tokens/sec and peak VRAM against GPU-layer offload; time to first
token with streaming on and off; the three quantizations against throughput, VRAM, and how much
generated logic survives the compile gate rather than degrading to a stub; three two-model serving
strategies on the same task set; and the Understand half against four public repositories pinned
to a commit, including 30k lines of third-party COBOL.

On the quality metric, stated plainly: **"the generated service compiles" is close to worthless as
a metric here.** When an operation body fails the compile gate twice, it is replaced with an
explicit `not implemented` stub, and stubs compile. The numbers worth reading are the stub rate
and the repair counts.

## Supported languages

Go, COBOL (with copybooks), CA Gen, Java, TypeScript, and Python. Python requires `python3` 3.9 or
newer on the host; the other five are pure Go with no external toolchain. Anything the parsers
cannot model becomes an explicit unknown node carrying a reason, never a guess.

Adding a language: see `docs/adding-a-language.md`.

## Architecture

```
source ──▶ parser ──▶ symbol index ──▶ call graph ──▶ CFG ──▶ effects
                                                        │
                                              parser beliefs
                                                        │
                          model-proposed claims ──▶ gated against parser facts
                                                        │
                          human corrections ──▶ (the only beliefs that steer)
                                                        │
   plan ──▶ generate ──▶ per-op compile gate ──▶ vet / build / test ──▶ Understand re-verifies
```

Two roles: `qwen3:14b` plans, `qwen2.5-coder:14b` writes code. They are used sequentially with
Ollama's keep-alive holding a model resident between calls.

**There is no scheduler in this repository.** "Sequential with keep-alive" means Ollama's own
keep-alive, two model roles, and measurement of what the swap costs. If you are looking for
scheduling code, there is none to find, and the benchmarks measure the swap rather than claiming
it away.

An architecture test (`internal/archtest`) forbids the Understand server from linking any
generation package, so the read-only half cannot acquire a model dependency by accident.

## Neighbours and prior art

- AST-derived graph RAG (arXiv 2601.08773): deterministic AST graphs outperform vector-only and
  LLM-built graphs on Java repositories. gen-system relies on the same finding.
- Morph: typed refactoring plans with a test gate and rollback. Python only, temperature 0.1,
  cloud-first.
- Aider, Continue, Cody: local-model capable assistants with a different goal, interactive editing.

There is no head-to-head benchmark against any of these. A fair one needs the same model, the same
hardware, and the same task set on both sides, and that has not been run. The Understand
benchmark on public repositories is what exists instead.

## Limitations

- One GPU. No multi-GPU or multi-node support.
- Six languages. Everything else parses as unknown nodes rather than failing, but you get no
  symbol resolution for it.
- Body rewriting is implemented for Go and Python only. Other languages return an explicit
  not-implemented error rather than letting the repair loop mutate source it cannot parse back.
- After two failed repairs an operation degrades to a `not implemented` stub. The service builds;
  that operation does not work. This is deliberate and it is counted, not hidden.
- The model's security review is advisory. It warns and never blocks.
- Module-level and project-level belief summaries are implemented but not yet produced by the
  shipping CLI, which currently summarizes at service level only.

## Future work

Feed the rendered control-flow graphs back to the model as repair context, behind a flag, and
measure it as an ablation. Expectation: no effect on Go repair, where the compiler already
supplies the signal, and a possible effect on COBOL and on belief enrichment. The result gets
published either way, including if it is null.

## Contributing

Open an issue with the belief document attached. That document exists so both of us can see where
the understanding went wrong.

## License

Apache License 2.0. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).

Apache-2.0 rather than MIT because it grants patent rights explicitly, which matters for the
enterprise and air-gapped deployments this is aimed at, while staying permissive.

The only external Go dependency is `github.com/lib/pq` (MIT). Benchmark corpora are not vendored;
the COBOL baseline fetches CobolCraft (MIT) on demand at a pinned commit.
