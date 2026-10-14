# Contributing to gen-system

Thanks for looking. The most useful contribution is a case where gen-system reads real code wrong.
Code changes are welcome too. This page says what to send and how I check it.

## Report a wrong read

Open an issue with the **The parser got my code wrong** template. Attach:

- the smallest file that shows the problem, or a link to a public repository and a commit;
- the belief file, `.gen/beliefs.json`, from the run;
- what you expected: the call, the control flow graph, the effect or the belief.

The belief file lists what gen-system read for each routine, so I can see the wrong line without
running your code. If you cannot share the code, describe the construct and paste a few lines that
reproduce it with the names changed.

Before you file, try the [live demo](https://gen-system.500kv.co/demo/). It shows the Studio on a
COBOL example, a Go example and CobolCraft.

For an install problem, use the **Install or start failed** template and include your OS, the install
command and the full output.

## Set up

You need Go 1.25 or newer and git. Python files need `python3` 3.9 or newer; the other five
parsers are pure Go. The generation half needs Ollama and a GPU, but the parsers and their tests
do not.

```bash
git clone https://github.com/hpdkhoa/gen-system
cd gen-system
make test
```

`make test` runs `go test ./...` in `generator/`. Several tests write `.gen/ast_cache` folders next
to the code they parse. Git ignores `.gen/`, so they never reach a commit, but you can delete them
for a clean tree.

## Before you open a pull request

Run these in `generator/`:

```bash
gofmt -l .
go vet ./...
go test ./...
```

`gofmt -l` must print nothing. All three must pass.

If your change touches a parser, the symbol index or the control flow graphs, also run the COBOL
baseline:

```bash
generator/internal/bench/baselines/freeze-cobol-baseline.sh verify
```

It fetches CobolCraft at a pinned commit, parses it and compares the counts with the frozen
baseline: parse errors, internal call recall and control flow graph checks. It exits 1 on any
drift. If your change moves a number on purpose, say which number and why in the pull request. I
refreeze the baseline myself after review.

## What a good pull request looks like

- **One change per pull request.** A parser fix and a Studio fix go in two pull requests.
- **A test for every fix.** Put the smallest source that failed into the test, and check the exact
  node, edge or field that was wrong. The `*_v2668_test.go` files in `internal/ast` and
  `internal/symbols` show the pattern.
- **No new dependencies** without an issue first. The only external Go dependency today is
  `github.com/lib/pq`, and I want to keep it that way.
- **Deterministic output.** The same input must give byte-identical graphs, effects and beliefs.
  Sort anything that comes out of a map.
- **A parse failure is a node, not an error.** Code the parser cannot model becomes a
  `NodeUnknown` with a reason. `Parse` returns an error only when it cannot read the file.

Commit messages say what changed in plain words, for example "Fix call resolution for COBOL CALL
USING". Keep the first line under about 70 characters.

## Where things live

Paths are relative to `generator/`.

| Area | Path |
|---|---|
| Parsers, one frontend per language | `internal/ast` |
| Symbol index and call resolution | `internal/symbols` |
| Control flow graphs and data flow | `internal/flow` |
| Beliefs | `internal/belief` |
| Studio pages and its API | `internal/webui`, `internal/presentation` |
| MCP server | `cmd/understand-mcp`, `internal/mcp` |
| Benchmarks and the COBOL baseline | `internal/bench`, `../bench` |
| Landing page and static demo | `../site` |

[docs/understand-pipeline.md](docs/understand-pipeline.md) walks from a source file to a control
flow graph. [docs/adding-a-language.md](docs/adding-a-language.md) lists every file a new language
touches, in order, with the tests for each step. Read it before you start a new parser, and open an
issue first so we can agree on the scope.

## The Studio and the demo site

The Studio page lives in `generator/internal/webui/studio-v23.html`, with a copy in
`web/studio-v23.html`. Change both. `python site/build.py` rebuilds the static demo from the
current Studio page and snapshots the three example projects. Commit the rebuilt `site/public/`
with the change.

## Code generated with AI tools

You may use AI tools. You are responsible for every line you send: you read it, you run the tests,
and you can explain it in review. Do not send pull requests that you have not run.

## Conduct and security

Everyone here follows the [code of conduct](CODE_OF_CONDUCT.md). Report a security problem
privately, as [SECURITY.md](SECURITY.md) describes, and not in a public issue.

## License

gen-system is Apache-2.0. By sending a contribution you agree to license it under the same terms,
as section 5 of the [license](LICENSE) describes.
