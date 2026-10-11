# gen-system

gen-system reads codebases and generates Go services with local models. It builds a deterministic
graph of your code, writes down what the model assumes about it, checks each assumption against
the graph, and lets you correct any of them. Every generated operation must compile before it
ships.

It runs offline on one 16 GB GPU, with no token cost.

## Features

- **Deterministic code graph.** Hand written parsers for Go, COBOL with copybooks, CA Gen, Java,
  TypeScript and Python build the AST, symbol index, call graph and control flow graph, down to
  the standard library. No model takes part.
- **Correctable beliefs.** For each routine, gen-system lists the inputs, the calls, the data it
  writes and the action its name suggests. A local model can add claims. gen-system marks any
  claim that contradicts the parser and never uses it. Only beliefs a person verified steer
  generation.
- **Gated generation.** Each model written operation compiles on its own before it enters the
  service file. Then `go vet`, `go build` and `go test` must pass, and the Understand half rereads
  the result.
- **Repeatable runs.** Every model call uses temperature 0 and seed 42. The determinism suite runs
  the same prompt N times and reports how many distinct outputs came back.
- **Team style profiles.** Your conventions go into every prompt, and the parser checks the same
  rules on the output.
- **Air gapped review.** The read only Understand half installs with no model dependency.
- **MCP server.** `understand-mcp` gives Claude Code, Codex, Cursor and other agents the same
  parser facts: architecture, call graphs, control flow, effects, source and beliefs.

## Quickstart

```bash
git clone https://github.com/hpdkhoa/gen-system.git
cd gen-system
./install.sh                    # Go, Ollama, model pulls, optional Postgres and Node
```

`./install.sh --understand` installs only the read only half, for an air gapped review of an
existing codebase.

The installer pulls the two default models:

```bash
ollama pull qwen3:14b           # planning role
ollama pull qwen2.5-coder:14b   # code role
```

Check the machine before you trust a number from it:

```bash
genbench model                  # is Ollama up, is the model loaded, does it stay resident
```

### Read an existing codebase

```bash
understand-server --root ./your-repo    # comprehension API plus the Studio UI
```

gen-system writes the beliefs to `./your-repo/.gen/beliefs.json`. Correct one in the Studio, or
POST to `/api/present/beliefs/correct`. Your correction wins from then on. If the source later
changes under it, gen-system flags the belief as out of date and shows your value next to the new
one.

### Give your agent the facts (MCP)

```bash
claude mcp add gen-understand -- understand-mcp --root ./your-repo
```

`understand-mcp` serves the same Understand engine over the Model Context Protocol, read only.
Your agent can search symbols, walk call graphs, read control flow and effects, and list the
beliefs, each answer straight from the parser. `docs/mcp.md` covers Codex, Cursor and the tool list.

### Generate a new service

```bash
gen idea --out ./generated-app "a library lending service with members, books and loans"
```

gen-system attaches the standard library of every language in the project, import scoped, so the
verify pass knows `net/http` and `encoding/json` when it rereads the output. `--stdlib none` opts
out, `--stdlib go,java=/opt/jdk/java.base` sets it explicitly, and `GEN_STDLIB` is the environment
form.

### Apply a team style profile

```bash
gen style derive ~/code/team-repo --out gen-style.json   # measure an existing repo, with evidence per rule
gen style print > gen-style.json                          # or start from the default and edit
gen idea --style gen-style.json --style-mode gate "A support ticket tracker..."
gen style check ./generated-app/backend --profile gen-style.json
```

A profile covers naming, folder layout, required and banned patterns, size limits, error wrapping,
loop style and imports. The style gate checks the generated tree through the Understand index. An
operation that compiles but breaks a rule goes back to the model with the rule named, under its own
repair cap. Only the compile gate can turn an operation into a stub. See `docs/style-gate.md`.

## How it works

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

`qwen3:14b` plans and `qwen2.5-coder:14b` writes code. gen-system calls them one after the other,
and Ollama's keep alive holds a model resident between calls. The repository has no scheduler of
its own; the benchmarks measure what the model swap costs.

An architecture test (`internal/archtest`) forbids the Understand server from linking any
generation package, so the read only half cannot gain a model dependency.

## Benchmarks

The harness, the frozen task set and the drivers are in `bench/`. The repository commits the task
set before any run.

```bash
./bench/freeze-env.sh           # record commit, GPU, driver, model list
./bench/run-all.sh              # the full campaign from fresh
./bench/run-partial.sh --run strategies,swebench:60 --reuse results/<earlier date>   # rerun some drivers, reuse the rest
python3 bench/summarise.py --results results/<date>
python3 bench/attest.py --results results/<date>   # manifest and attestation for the campaign
```

Each campaign ends with `results/<date>/ATTESTATION.md`, which records what ran, on which commit
and machine, and which runs it dropped and why. `MANIFEST.sha256` lists every raw file with its
SHA-256.

The campaign measures tokens per second and peak VRAM against GPU layer offload, time to first
token with streaming on and off, three quantizations, three ways of serving two models, and the
Understand half on four public repositories pinned to a commit.

A compiling service is a weak signal here: an operation body that fails the compile gate twice
becomes a `not implemented` stub, and stubs compile. Read the stub rate and the repair counts.

Published results live in `benchmarks/results/measured.json` in
[engineering-notes](https://github.com/hpdkhoa/engineering-notes), with the date, commit, task set,
n and a range for each row.

## Supported languages

Go, COBOL (with copybooks), CA Gen, Java, TypeScript and Python. Python needs `python3` 3.9 or
newer on the host. The other five are pure Go with no external toolchain. A construct the parsers
cannot model becomes an unknown node that carries the reason.

To add a language, see `docs/adding-a-language.md`.

## Documentation

- `docs/understand-pipeline.md`: the parse pipeline from source file to control flow graph.
- `docs/mcp.md`: the Understand half as an MCP server, its tools and how to register it.
- `docs/beliefs.md`: the belief record, a worked example, and how staleness spreads.
- `docs/model-contact.md`: the single path to the model, its determinism pins and runtime knobs.
- `docs/style-gate.md`: what a style profile can say, and how the gate checks it.
- `docs/adding-a-language.md`: the frontend contract and a template for a new parser.

## Limitations

- One GPU. No multi GPU or multi node support.
- Six languages. Other files parse as unknown nodes, with no symbol resolution.
- Body rewriting works for Go and Python only. Other languages return a `not implemented` error.
- After two failed repairs an operation becomes a `not implemented` stub. The service builds, and
  the stub rate counts it.
- The model's security review warns and never blocks.
- The CLI summarizes beliefs at service level only. The code for module and project summaries
  exists but does not run yet.

## Roadmap

Feed the rendered control flow graphs to the model as repair context, behind a flag, and measure
the effect as an ablation, with the result published either way.

## Contributing

Open an issue and attach the belief document. It shows where the understanding went wrong.

## License

Apache License 2.0. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).

The only external Go dependency is `github.com/lib/pq` (MIT). The repository vendors no benchmark
corpus; the COBOL baseline fetches CobolCraft (MIT) on demand at a pinned commit.
