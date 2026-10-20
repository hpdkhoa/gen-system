<h1 align="center">gen-system</h1>

<p align="center"><strong>Review code changes with facts from a parser, not guesses from a model.</strong></p>

<p align="center">
  <a href="https://gen-system.500kv.co/"><img alt="demo: live" src="https://img.shields.io/badge/demo-live-2ea44f"></a>
  <a href="https://github.com/hpdkhoa/gen-system/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/hpdkhoa/gen-system/actions/workflows/ci.yml/badge.svg"></a>
  <a href="LICENSE"><img alt="license: Apache 2.0" src="https://img.shields.io/badge/license-Apache%202.0-blue"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/hpdkhoa/gen-system?label=Go&logo=go&logoColor=white"></a>
  <a href="https://www.npmjs.com/package/gen-understand"><img alt="npm: gen-understand" src="https://img.shields.io/npm/v/gen-understand?label=npm&logo=npm&color=cb3837"></a>
  <a href="https://registry.modelcontextprotocol.io/v0/servers?search=gen-understand"><img alt="MCP Registry: gen-understand" src="https://img.shields.io/badge/MCP%20Registry-gen--understand-6f42c1"></a>
  <a href="docs/understand-pipeline.md#the-six-parsers"><img alt="parsers: 6 languages" src="https://img.shields.io/badge/parsers-6%20languages-orange"></a>
</p>

## WHAT IT DOES

AI writes more code than people can read. gen-system reads your code with parsers and tells you, for
each change:

- **what may break**: a removed function that is still called, a changed signature with callers
  outside the change;
- **what it now touches**: the database, files or the network;
- **whether any test reaches it**;
- **how much it grew**: new branches.

No model takes part. The same change always gets the same answer.

```text
$ understand-review --base main

## May break (2)
- store.go:8 Save changed its signature from (name string) error to (name string, force bool) error;
  1 caller outside this change: Main (main.go:3)
- store.go:9 Old was removed but is still called from 1 place: Main (main.go:3)

## New side effects (2)
- store.go:8 Save now touches files
- store.go:25 Fresh is new and touches the network

## No test reaches these (2)
- store.go: Save, Fresh

## Grew (1)
- store.go:12 Helper has 3 more branches (0 to 3)
```

## USE IT

| Where | How |
|---|---|
| Terminal or CI | `understand-review --root . --base main`. Add `--fail-on break` to stop a merge. |
| Your AI agent | `claude mcp add gen-understand -- npx -y gen-understand --root .`, then ask it to review your changes (tool `review_diff`). |
| Browser | `understand-server --root .` opens the Studio: call graphs and control flow graphs. |

It reads **Go, Python, TypeScript, Java, COBOL with copybooks and CA Gen**, one hand written parser
each. [docs/review.md](docs/review.md) explains every check and its limits.

![gen-system-demo](docs/img/gen-system-demo.gif)

*The Studio: what a routine calls, and its control flow graph. [Try it in your browser](https://gen-system.500kv.co/demo/), no install.*

## SYNOPSIS

```
understand-review --root DIR [--base REV] [--diff FILE] [--json] [--fail-on LEVEL]
understand-mcp    --root DIR [--copybooks DIRS] [--allow-corrections] [--no-watch]
understand-server --root DIR [--copybooks DIRS] [--port 28080] [--stdlib auto|none|LIST]
genbench          SUBCOMMAND --root DIR [--out FILE]
gen               idea --out DIR "WHAT TO BUILD"     (experiment, see below)
```

## COMMANDS

| Command | What it does |
|---|---|
| `understand-review` | Reviews a change: prints the points above, as Markdown or JSON. |
| `understand-mcp` | The facts over MCP (stdio), for Claude Code, Codex or Cursor. Also on npm as `gen-understand`. |
| `understand-server` | The Studio on http://localhost:28080: architecture, search, call graph, control flow graphs, source, beliefs. |
| `genbench` | Measurements, including the `review` benchmark. |
| `gen-enrich` | Asks a local model to describe each routine. Its answers never override the parser. |
| `gen`, `gen-server` | The generator experiment (below). |

`understand-review`, `understand-mcp` and `understand-server` contain no model client. A test fails
the build if that changes.

## MCP TOOLS

| Tool | Answers |
|---|---|
| `review_diff` | the review above, for uncommitted work (default) or against any git base |
| `architecture` | languages, top packages, entry points, call resolution, what the parsers could not read |
| `search_symbols` | symbols by name, with the ids the other tools take |
| `call_graph` | callers or callees of a symbol, up to 4 hops |
| `control_flow` | a routine's basic blocks, edges and source lines |
| `source`, `file_symbols` | the code of a symbol; every symbol in a file |
| `effects` | routines grouped by what they touch: database, files, network, pure |
| `beliefs` | what gen-system holds about each routine, from the parser, a model or a person |

`correct_belief` appears only with `--allow-corrections`. See [docs/mcp.md](docs/mcp.md).
`review_diff` reaches `npx gen-understand` with the next release; until then build `understand-mcp`
from source.

## THE GENERATOR (EXPERIMENT)

`gen` tests one idea: does the parser's call graph help a local model write code that works? It
plans a Go service with `qwen3:14b`, writes each operation with `qwen2.5-coder:14b`, and each
operation must compile. One that still fails after two repairs becomes a `not implemented` stub,
and the run counts it. It needs Ollama and a 16 GB GPU. It is not the main product.

## INSTALL

```bash
git clone https://github.com/hpdkhoa/gen-system.git && cd gen-system
./install.sh --understand   # only understand-server, for an air gapped review
./install.sh                # everything: also Ollama, two 14B models, one 16 GB GPU
```

For the MCP server alone, you need nothing but Node or Go:

```bash
claude mcp add gen-understand -- npx -y gen-understand --root ./your-repo
go install github.com/hpdkhoa/gen-system/generator/cmd/understand-mcp@latest
```

## EXAMPLES

Review your uncommitted work, then a branch against main, and fail CI on a break:

```bash
understand-review --root .
understand-review --root . --base origin/main --fail-on break
```

Explore a repository in the browser:

```bash
understand-server --root ./your-repo
understand-server --root examples/payroll --copybooks examples/payroll/copybooks
```

Ask your agent "who calls CALC-TAX, and which paragraphs touch the database?" on the COBOL
example `examples/payroll`. It calls `call_graph` and `effects`, and gets (shortened):

```jsonc
"edges":     [ { "from": "…#func:PROCESS-EMPLOYEE", "to_name": "CALC-TAX" },
               { "from": "…#func:MAIN-PARA",        "to_name": "PROCESS-EMPLOYEE" } ]
"by_effect": { "db":   [ { "name": "RECORD-RUN", "line": 78 } ],
               "pure": [ { "name": "CALC-GROSS" }, { "name": "CALC-TAX" } ] }
```

## ENVIRONMENT

| Variable | Meaning |
|---|---|
| `GEN_COBOL_COPYBOOK_PATH` | where the COBOL parser looks for copybooks |
| `GEN_STDLIB` | standard library roots, as for `--stdlib` |
| `OLLAMA_HOST` | the Ollama address, default `http://127.0.0.1:11434` |
| `OLLAMA_MODEL_PLAN`, `OLLAMA_MODEL_CODE` | the planning model (`qwen3:14b`) and the code model (`qwen2.5-coder:14b`) |

`gen-system.env.example` lists the rest. Copy it to `.env`; your shell wins over the file.

## FILES

| Path | Holds |
|---|---|
| `DIR/.gen/beliefs.json` | the beliefs and your corrections; attach it to a bug report |
| `DIR/.gen/ast_cache/` | the parser cache; safe to delete |

## ACCURACY

On four public repositories (405 files, 268 of them COBOL) the parsers report 0 parse errors. In
CobolCraft, a Minecraft server in COBOL, all 4,018 calls between its own programs and paragraphs
resolve. GnuCOBOL's compiler agrees with gen-system on all 4,132 `CALL` and `PERFORM` sites, and CI
checks that on every push. In four Go repositories, Go's own type checker agrees with 99% to 100% of
gen-system's Go call links, and the links cover 97% to 100% of the calls into the project.
On the same four repositories, Go's type checker and `go test` judged review_diff on edits with a
known answer. It found the callers a signature change breaks with precision 0.98 to 1.00 and
recall 0.87 to 1.00. It found all 306 inserted file, network and database calls and branches, and
flagged 0 of 400 harmless calls.
[docs/results.md](docs/results.md) has the method, the limits and two corrections to earlier claims.

## LIMITS

- Six languages. Other files show as unknown, with no symbols.
- The review finds callers through the call graph. A call through a function value or an interface
  it cannot resolve is not a caller, and "no test reaches it" means no test within the call depth
  it checks. [docs/review.md](docs/review.md) lists the measured limits.
- COBOL is checked on one free format GnuCOBOL codebase; no numbers yet for IBM Enterprise COBOL.
- Generation writes Go, on one GPU. Body rewriting works for Go and Python only.
- The model's security review warns and never blocks.

## DOCUMENTATION

| Document | Covers |
|---|---|
| [Review](docs/review.md) | what `review_diff` and `understand-review` check, how to run them, the benchmark |
| [Understand pipeline](docs/understand-pipeline.md) | how a source file becomes a symbol index, a call graph and control flow graphs |
| [Beliefs](docs/beliefs.md) | the belief record, a worked example, corrections and staleness |
| [MCP server](docs/mcp.md) | setup for Claude Code, Codex and Cursor, the flags and every tool |
| [Style gate](docs/style-gate.md) | team style profiles and how generation checks them |
| [Model contact](docs/model-contact.md) | the single path to the model and its settings |
| [Adding a language](docs/adding-a-language.md) | every file a new parser touches, with its tests |
| [Results](docs/results.md) | the measurements, the GnuCOBOL comparison and a correction |
| [Benchmarks](bench/README.md) | the benchmark drivers and how to rerun a campaign |

## CONTRIBUTING

Found code that gen-system reads wrong? Open an issue and attach `.gen/beliefs.json`.
[CONTRIBUTING.md](CONTRIBUTING.md) covers bug reports, setup, tests and pull requests. Everyone
follows the [code of conduct](CODE_OF_CONDUCT.md).

## SECURITY

Report a vulnerability privately, as [SECURITY.md](SECURITY.md) describes, not in a public issue.

## LICENSE

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE). The only external Go dependency is
`github.com/lib/pq` (MIT).
