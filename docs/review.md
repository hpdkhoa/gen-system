# Review a change

`understand-review` (terminal, CI) and the MCP tool `review_diff` (your AI agent) read a change with
the parsers and list what a reviewer should look at. No model takes part.

## What it checks

| Level | It says | How it knows |
|---|---|---|
| May break | a removed function is still called | calls in the current code to a name that no longer exists |
| May break | a function changed its signature and has callers outside the change | the call graph |
| New side effects | a function now touches the database, files or the network | the calls in the function, before and after |
| No test reaches these | no test calls the function within 3 calls | the call graph, from test files (`_test.go`, `test_*.py`, `*.test.ts`, `*Test.java`) |
| Grew | a function has 2 or more new branches | the control flow graph, before and after |

It also lists every function the change touched, with its callers outside the change.

## Run it

```bash
understand-review --root .                         # your uncommitted work (base HEAD)
understand-review --root . --base origin/main      # a branch
understand-review --root . --json                  # for a tool
understand-review --root . --fail-on break         # exit 1 when something may break (CI)
understand-review --root . --diff change.patch     # a diff file; checks callers and tests only
```

In an agent: call `review_diff` with `base` (default `HEAD`) or `diff`, and `format` `markdown` or
`json`. The MCP server keeps the index in memory, so a review takes well under a second. The
terminal command reads the whole repo first.

New files git does not track yet count as added. Files in a language gen-system cannot read are
listed as not parsed.

## Limits

- Callers come from the call graph. A call through a function value or an interface, or a method
  call on a value whose type the code does not show, is not a caller.
- "Removed but still called" matches the name of the removed function. In Go it also checks the
  package and, when the code shows it, the receiver's type. In other languages a common name can
  match an unrelated call.
- "No test reaches these" also follows calls it could not resolve, by name. So a function can count
  as tested when only a same-named function is.
- Effects come from call names: `os.ReadFile`, `http.Get`, `db.Exec`. A plain verb such as `Get`
  or `Open` counts only on a receiver whose name says what it is (`client`, `file`, `db`).
- A test that reaches a function through more than 3 calls is not seen. Change it with `--hops`.
- With `--diff` and no git base, nothing before the change is known: no signature, effect, branch
  or removal checks.

## How well it works

`genbench review` measures it on a Go repo. It edits a copy of the repo with a known answer and
lets Go's own type checker be the judge:

| Edit | The right answer |
|---|---|
| add a parameter to a function | every caller the type checker rejects |
| delete a function | every place the type checker reports |
| insert a file, network or database call | that effect, and nothing else |
| insert a call that touches nothing (`cache.Get`, `list.Remove`, ...) | no new effect |
| insert two `if` statements | 2 new branches |
| none, with `--coverage` | for each function: does `go test` coverage agree with "no test reaches it"? |
| none, real commits | how many points a real change gets |

```bash
genbench review --repo ./some-go-repo --n 20 --commits 20 --coverage --out review.json --md review.md
```

On 4 Go repos (go-chi, gorilla/mux, spf13/cobra, gen-system):

| Check | Result |
|---|---|
| callers of a changed signature | precision 0.98 to 1.00, recall 0.87 to 1.00 |
| callers of a removed function | precision 0.96 to 1.00, recall 0.88 to 1.00 |
| new file, network, database calls and branches | all 306 found |
| harmless calls flagged as effects | 0 of 400 |
| "no test reaches it" agrees with `go test` coverage | 0.81 to 0.99 |

Full tables: [results.md](results.md#review_diff).
