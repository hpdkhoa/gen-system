# Model contact

Everything the model touches, and every wall around it. If you are auditing this system for what a
language model can and cannot do to your code, this is the file to read.

## One door

Every request to Ollama goes through `doRequest` in `generator/internal/ai/ollama.go`. There is no
second HTTP client, no library call that bypasses it, and no place where a prompt is sent from
outside the `ai` package. Fourteen call sites reach that door; thirty-one functions in the codebase
touch the model at all. Everything else is deterministic.

The benchmark package has its own client on purpose (`internal/bench/perf_gen.go`) so it can target
an arbitrary model tag and read Ollama's own timing fields. It sends the same options the
production door sends.

## Determinism pins

Every request carries the same three values (`ollama.go`, `DefaultOptions`):

```go
var DefaultOptions = &OllamaOptions{
	Temperature: 0.0,
	Seed:        42,
	TopP:        1.0,
}
```

There is no code path that raises the temperature, and no caller can override the seed. The
`determinism` benchmark suite issues the same prompt five times and counts distinct outputs; the
contract is one.

Transport is not allowed to change that. When `OLLAMA_STREAM=1` is set, the streaming reader
reassembles the response by concatenating fragments, which produces exactly the string the
buffered path returns. A unit test asserts one unique output across five identical streamed
responses.

## Runtime knobs

Read from the environment, sent only when set, so a machine that sets none of them produces
byte-identical requests to the pre-v26.24 client:

| Variable | Effect |
|---|---|
| `OLLAMA_HOST` | Endpoint. Default `http://localhost:11434` |
| `OLLAMA_TIMEOUT` | Per-request timeout in seconds. Default 600 |
| `OLLAMA_KEEP_ALIVE` | Passed through to Ollama, keeps a model resident between calls |
| `OLLAMA_MODEL_PLAN` | Planning role. Default `qwen3:14b` |
| `OLLAMA_MODEL_CODE` | Code role. Default `qwen2.5-coder:14b` |
| `OLLAMA_NUM_GPU` | Layers offloaded to the GPU |
| `OLLAMA_NUM_CTX` | Context window size |
| `OLLAMA_NUM_BATCH` | Prompt-processing batch size |
| `OLLAMA_STREAM` | `1` or `true` enables streaming reads. Default off |
| `GEN_LOGIC` | `ollama` uses the live model for business logic. Anything else uses the replay provider |

The 600 second default timeout is not padding. Two 14B-class models share one card, so a cold load
of 30 to 90 seconds can land in the middle of a run on top of generation time.

## The two roles

`ModelFor` (`ai/model.go:68`) resolves a role to a model tag:

- plan: `OLLAMA_MODEL_PLAN`, then `OLLAMA_MODEL`, then `qwen3:14b`
- code: `OLLAMA_MODEL_CODE`, then `OLLAMA_MODEL`, then `qwen2.5-coder:14b`

**There is no scheduler.** "Sequential scheduling with keep-alive" means Ollama's own keep-alive,
these two roles, and measurement of what swapping costs. No scheduling code exists in this
repository. `gen doctor` checks that a second load is much faster than the first, which is how you
confirm keep-alive is actually working rather than assuming it.

## Schema-constrained calls

Calls that must return structured data attach a JSON Schema as Ollama's `format` field, so the
model cannot return almost-JSON. `AskLLMJSON` (`ollama.go:89`) takes the schema; with none supplied
it falls back to plain JSON mode.

The operations schema (`ai/logic.go:58`) is the clearest example: an object envelope containing an
array of operations, each requiring `name`, `http_method` from a fixed enum, and `body`. Schema
constraint is used for the planning calls, the operations call, and the continue-delta call.

Parsing is still defensive after that. `ParseOperations` (`ai/logic.go:132`) strips code fences,
tries the envelope, then falls back to a bare array, and returns an error rather than a guess if
neither works.

## The body-only contract

When the model repairs a function, it returns a bare function body. Nothing else.

`ExtractBodyOnly` and `ValidateFunctionBody` enforce the shape, then `ast.RewriteFunction` performs
an AST-level body replacement. **The signature is never touched by the model.** The function's
name, parameters, and return types come from the deterministic generator and stay there.

Rewriters exist only for Go and Python. Every other language returns
`ErrRewriterNotImplemented`, so the heal loop cannot mutate source it is unable to parse back and
verify. A language without a rewriter fails loudly instead of being edited blindly.

## What happens to a model-written operation

1. The model proposes operations for an entity under the schema above.
2. Each body is assembled into a realistic service package in a temp directory and **compiled
   alone** with `go build`, against the real store interface and a stand-in model struct carrying
   the same fields the generated model gets (`engine/service_logic.go`).
3. If it fails, the build error goes back to the model, up to `maxRepairAttempts`, which is **2**.
4. If it still fails, the body is replaced with `return nil, errors.New("not implemented: <Name>")`
   and the outcome is counted.

That last step is why "the generated service always compiles" is a near-worthless claim on its own:
a stub compiles. The metrics that matter are `ops_stubbed` and the repair counts, which the
`gen_pipeline` benchmark reports.

Note the two different caps, which are easy to conflate: **2** repair attempts at the operation
level (`engine.maxRepairAttempts`), and **3** at the task level in the self-heal loop
(`agent.maxHealAttempts`). Any document saying "3 repairs then a stub" is describing the wrong one.

## The self-heal loop

`FixTask` (`agent/healer.go`) runs when a whole task fails to build, with escalating context:

| Attempt | Context added |
|---|---|
| 1 | Error text, existing code, and a classification hint |
| 2 | Plus the architecture summary and up to three retrieved framework-doc chunks |
| 3 | Plus related files from the system graph, under 1500 characters each |

Every attempt is verified by `go build` and **reverted on failure** before the next one runs, so a
failed repair cannot leave the tree worse than it found it. A cached fix from the repair memory is
compile-verified rather than trusted, and evicted when its failures reach twice its successes.
Three consecutive unhealable tasks restore the pre-run snapshot.

## Advisory, not blocking

The model performs a security review of generated handlers and services. It warns, it never blocks,
and it fails open with a warning if the model is unreachable. Treat its output as a prompt for
human attention, not as a gate. The gates are the compiler, the tests, and the Understand pass.

## Running with no model at all

The default logic provider is a replay fixture table (`ai.ReplayProvider`), so the entire
generation pipeline runs offline and deterministically with no Ollama present. The live model is
opt-in through `GEN_LOGIC=ollama`.

This is what makes the pipeline testable: the offline `gen_pipeline` benchmark exercises plan,
generate, compile gate, heal, and Understand-verify end to end without a GPU, and its unit test
asserts that nothing gets stubbed, because the fixtures compile as written.

The planner degrades differently and it is worth knowing. Its entity step has no deterministic
fallback, so with no reachable model it returns zero entities. The benchmark handles that by
falling back to a fixed schema and **counting it** in a `planner_fallbacks` metric that appears in
the output, so an offline run can never be mistaken for a measured one.

## The full picture

```
prompt built from typed inputs (pure string assembly, no I/O)
        │
   ModelFor(role) ──▶ model tag
        │
   AskLLMJSON ──▶ JSON Schema attached as `format`
        │
   doRequest ──▶ temp 0, seed 42, top_p 1, keep_alive, optional GPU knobs
        │        3 retries with backoff, 600s timeout
        │
   extract ──▶ fences stripped, outermost JSON object or array, typed unmarshal
        │
   validate ──▶ body-only contract, AST rewrite, signature untouched
        │
   compile alone ──▶ up to 2 repairs ──▶ else an explicit stub
        │
   vet, build, test ──▶ up to 3 heal rounds, each reverted on failure
        │
   Understand re-parses the result ──▶ unresolved calls, orphan ops, effect mismatches
```

The model is a proposer at every one of those arrows. Nothing it returns is trusted until
something deterministic has checked it.
