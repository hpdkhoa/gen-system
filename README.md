# gen-system

**An AI model is a black box. gen-system makes what it believes about your code visible. It checks
those beliefs against a deterministic graph of the code and lets you correct them. Then it
verifies the output from outside.**
Two models, one 16 GB GPU, fully offline, no token cost.

---

## Why this exists

An AI model is a black box. You cannot see what it believes about your code, so you find out when
the output is wrong, which is the most expensive moment to find out.

gen-system makes the beliefs visible and checkable. It grounds the model in a deterministic graph
of the code. Then it verifies the output with machinery that does not share the model's
assumptions. That is the whole idea. The steps:

1. **Understand** the source deterministically: AST, symbol index, call graph, control flow graph,
   down to standard library level. Six hand written offline parsers. No model involved. This graph
   is the ground truth everything else is checked against.
2. **Print what it believes** about the code it plans to touch: inputs, callees, inferred effects,
   what a routine appears to do. A local model can propose more beliefs. Each proposal is checked
   against the graph, and one that contradicts what the parser saw is marked and never used. You
   read it all and correct it.
3. **Generate** from the corrected belief, with context drawn from the graph rather than from text
   chunks. Only beliefs a human verified are allowed to steer.
4. **Gate** the output. Every model written operation compiles on its own before it can enter the
   service file. Then `go vet`, `go build`, and `go test` must pass. Then the Understand half
   rereads the result and reports what it finds.
5. **Repeat** and get the same output. Temperature 0, fixed seed, verified by a determinism suite.

The proving ground is legacy code. COBOL with copybooks and CA Gen sit next to Go, Java,
TypeScript, and Python. Legacy is where nobody can say what the system does, and where a wrong
answer costs the most. It is the hardest place to make this work, which is why it is the place to
prove it.

## What makes it different

| | Typical local AI tools | gen-system |
|---|---|---|
| Code model | Text chunks and embeddings | Deterministic AST, call graph and CFG, rendered as flowcharts for human review |
| What it thinks | Hidden | Printed, correctable, and gated against what the parser observed |
| Output acceptance | A diff, you decide | Per operation compile gate, then vet, build and test |
| Reproducibility | Best effort | Hard contract: temperature 0, seed 42, determinism suite |
| Hardware target | Whatever you have | One 16 GB consumer GPU, on purpose |
| Team conventions | A system prompt, unchecked | A style profile: rules go into the prompt, and the same rules are checked on the output by the parser, not by a model |

One clarification, because the first draft of this README got it wrong: the flowcharts are
rendered **for you**, not fed to the model. Nothing today uses a rendered flowchart as retrieval
data. Whether it would help is an open experiment, listed under Future work.

## Quickstart

```bash
git clone [TODO repo url]
cd gen-system
./install.sh                    # Go, Ollama, model pulls, optional Postgres and Node
```

`./install.sh --understand` installs the read only comprehension half with no model dependency,
which is the right choice for an air gapped review of an existing codebase.

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

## Team style profiles

A team's conventions can be given to the generator as a file, and checked on the way out.

```bash
gen style derive ~/code/team-repo --out gen-style.json   # measure an existing repo, with evidence per rule
gen style print > gen-style.json                          # or start from the default and edit
gen idea --style gen-style.json --style-mode gate "A support ticket tracker..."
gen style check ./generated-app/backend --profile gen-style.json
```

The profile covers naming, folder layout, required and banned patterns, size limits, error
wrapping, loop style, and imports. The rules go into every prompt the coder model sees. After
generation, the same rules are checked over the generated tree through the Understand index, with
no model involved. An operation body that compiles but breaks a rule is sent back to the model
with the rule named, under its own repair cap. Style never turns logic into a stub; only the
compile gate does that. `docs/style-gate.md` has the rule list, the modes, and the limits.

## Benchmarks

The harness, the frozen task set, and the drivers are in `bench/`. The task set is committed
before any run, so it cannot be tuned to the results.

```bash
./bench/freeze-env.sh           # record commit, GPU, driver, model list
./bench/run-all.sh              # the full campaign
python3 bench/summarise.py --results results/<date>
python3 bench/attest.py --results results/<date>   # manifest and attestation for the campaign
```

Every campaign ends with an attestation. `results/<date>/ATTESTATION.md` says what ran, on which
commit and machine. It says which runs were dropped as not measured and why, and what did not run.
`MANIFEST.sha256` lists every raw file with its SHA-256. Those two files are the certificate a
published number rests on.

**Numbers are not reproduced in this README.** They live in `benchmarks/results/measured.json` in
the portfolio repo, each row carrying the date, the commit, the task set, n, and a range. A README
table is a copy that goes stale the first time you rerun anything.

What the campaign measures:

- Tokens per second and peak VRAM against GPU layer offload.
- Time to first token with streaming on and off.
- The three quantizations against speed, VRAM, and how much generated logic survives the compile
  gate rather than falling back to a stub.
- Three ways of serving two models, on the same task set.
- The Understand half against four public repositories pinned to a commit, including 30,000 lines
  of third party COBOL.

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
Ollama's keep alive holding a model resident between calls.

**There is no scheduler in this repository.** "Sequential with keep alive" means Ollama's own
keep alive, two model roles, and measurement of what the swap costs. If you are looking for
scheduling code, there is none to find, and the benchmarks measure the swap rather than claiming
it away.

An architecture test (`internal/archtest`) forbids the Understand server from linking any
generation package, so the read only half cannot acquire a model dependency by accident.

## Neighbours and prior art

The pieces exist elsewhere. Here is where, and how this differs.

- **Reliable Graph-RAG for Codebases** (arXiv 2601.08773, January 2026) benchmarked deterministic
  AST derived graphs against LLM built graphs and vector search on Java repositories. It found the
  AST graphs give more reliable and cheaper grounding. That is the finding gen-system is built on.
  gen-system's first release was November 2025. It covers six languages with hand written parsers,
  rather than Tree-sitter on one. And it goes past retrieval into correctable beliefs, grounded
  generation, and verification of the output from outside. The idea arrived independently, and
  the execution went further.
- **PRAXIS** (arXiv 2608.19784, August 2026) stores knowledge the model extracted on a code
  dependency graph and surfaces it during generation. That is the same move as gen-system's
  generation half. The difference is the trust model. PRAXIS accumulates what the model derived.
  gen-system checks every model derived claim against parser facts, marks contradictions, and
  lets only human verified beliefs steer. PRAXIS has no verification tier after generation.
- **Shift-Left High-Level Synthesis Verification** (arXiv 2606.17128, June 2026) verifies hardware
  designs in two tiers. First static structural alignment, then dynamic behavioral equivalence
  with symbolic execution. It beats AST only baselines. That is the same two tier shape as
  gen-system's verify step, in another field, with a stronger dynamic tier. The lesson is the
  same. The graph alone is not enough. Behavior has to be checked.
- **From Fragments to Paths** (arXiv 2606.22906, June 2026) recovers task level context over
  repository structure for large industrial codebases. It reports 78.6 percent on SWE-bench
  Verified. That is the retrieval half, done at scale and measured end to end. gen-system has no
  SWE-bench number and does not claim one. Its differentiator is what happens after retrieval.
- **Externalization in LLM Agents** (arXiv 2604.08224, April 2026) is a review. It argues that
  agent capability is moving out of the weights and into memory, skills, protocols, and the
  harness. gen-system is an instance of that. The belief store is external memory. The graph is an
  external world model. The compile gate and the verify pass are harness engineering. One thing
  the review does not stress: the harness here is built to share none of the model's assumptions.
- **Morph**: typed refactoring plans with a test gate and rollback. Python only, temperature 0.1,
  cloud first.
- **Aider, Continue, Cody**: local model capable assistants with a different goal, interactive
  editing.

None of these have determinism as a hard contract, a single consumer GPU as the target, or COBOL
and CA Gen frontends.

There is no head to head benchmark against any of these. A fair one needs the same model, the same
hardware, and the same task set on both sides, and that has not been run. The Understand
benchmark on public repositories is what exists instead.

## Limitations

- One GPU. No multi GPU or multi node support.
- Six languages. Everything else parses as unknown nodes rather than failing, but you get no
  symbol resolution for it.
- Body rewriting is implemented for Go and Python only. Other languages return an explicit
  not implemented error rather than letting the repair loop mutate source it cannot parse back.
- After two failed repairs an operation degrades to a `not implemented` stub. The service builds;
  that operation does not work. This is deliberate and it is counted, not hidden.
- The model's security review is advisory. It warns and never blocks.
- Module level and project level belief summaries are implemented but not yet produced by the
  shipping CLI, which currently summarizes at service level only.

## Future work

Feed the rendered control flow graphs back to the model as repair context, behind a flag, and
measure it as an ablation. Expectation: no effect on Go repair, where the compiler already
supplies the signal, and a possible effect on COBOL and on belief enrichment. The result gets
published either way, including if it is null.

## Contributing

Open an issue with the belief document attached. That document exists so both of us can see where
the understanding went wrong.

## License

Apache License 2.0. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).

Apache-2.0 rather than MIT because it grants patent rights explicitly, which matters for the
enterprise and air gapped deployments this is aimed at, while staying permissive.

The only external Go dependency is `github.com/lib/pq` (MIT). Benchmark corpora are not vendored;
the COBOL baseline fetches CobolCraft (MIT) on demand at a pinned commit.
