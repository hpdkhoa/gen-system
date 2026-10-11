# Model contact

This page lists everything the model touches and every wall around it. If you audit this system
for what a language model can and cannot do to your code, read this file.

## One door

Every request to Ollama goes through `doRequest` in `generator/internal/ai/ollama.go`. There is no
second HTTP client and no library call that goes around it. No code outside the `ai` package sends
a prompt. Fourteen call sites reach that door; thirty-one functions in the codebase touch the model
at all. Everything else is deterministic.

The benchmark package has its own client on purpose (`internal/bench/perf_gen.go`). It can target
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

No code path raises the temperature, and no caller can override the seed. The `determinism`
benchmark suite sends the same prompt five times and counts distinct outputs; the contract is one.

The transport may not change that. When you set `OLLAMA_STREAM=1`, the streaming reader joins the
fragments back together. The result is the same string the buffered path returns. A unit test
asserts one unique output across five identical streamed responses.

## Runtime knobs

The client reads these from the environment and sends each one only when it has a value. A machine that
sets none of them produces byte identical requests to the pre-v26.24 client:

| Variable | Effect |
|---|---|
| `OLLAMA_HOST` | Endpoint. Default `http://localhost:11434` |
| `OLLAMA_TIMEOUT` | Per-request timeout in seconds. Default 600 |
| `OLLAMA_KEEP_ALIVE` | The client passes it through to Ollama; it keeps a model resident between calls |
| `OLLAMA_MODEL_PLAN` | Planning role. Default `qwen3:14b` |
| `OLLAMA_MODEL_CODE` | Code role. Default `qwen2.5-coder:14b` |
| `OLLAMA_NUM_GPU` | Layers Ollama offloads to the GPU |
| `OLLAMA_NUM_CTX` | Context window size |
| `OLLAMA_NUM_BATCH` | Prompt-processing batch size |
| `OLLAMA_STREAM` | `1` or `true` enables streaming reads. Default off |
| `GEN_LOGIC` | `ollama` uses the live model for business logic. Anything else uses the replay provider |

The 600 second default timeout leaves room for model loads. Two 14B-class models share one card,
so a cold load of 30 to 90 seconds can land in the middle of a run on top of generation time.

## The two roles

`ModelFor` (`ai/model.go:68`) resolves a role to a model tag:

- plan: `OLLAMA_MODEL_PLAN`, then `OLLAMA_MODEL`, then `qwen3:14b`
- code: `OLLAMA_MODEL_CODE`, then `OLLAMA_MODEL`, then `qwen2.5-coder:14b`

**There is no scheduler.** "Sequential scheduling with keep alive" means Ollama's own keep alive,
these two roles, and a measurement of what swapping costs. This repository has no scheduling code.
`genbench model` (the `ollama_doctor` suite) checks that a second load is much faster than the
first. That check confirms keep alive works; you do not have to assume it. (`gen doctor` itself
only checks that `.gen`, `generator` and `go.mod` exist.)

## Schema-constrained calls

Calls that must return structured data attach a JSON Schema as Ollama's `format` field, so the
model cannot return almost-JSON. `AskLLMJSON` (`ollama.go:206`) takes the schema. With no schema, it
falls back to plain JSON mode.

The operations schema (`ai/logic.go:58`) is the clearest example. It is an object envelope with an
array of operations, and each operation requires `name`, `http_method` from a fixed enum, and
`body`. The planning calls, the operations call and the continue-delta call use a schema.

Parsing stays defensive after that. `ParseOperations` (`ai/logic.go:132`) strips code fences,
tries the envelope, then falls back to a bare array. If neither works, it returns an error and
does not guess.

## The body-only contract

When the model repairs a function, it returns a bare function body. Nothing else.

`ExtractBodyOnly` and `ValidateFunctionBody` enforce the shape. Then `ast.RewriteFunction` replaces
the body at the AST level. **The model never touches the signature.** The function's name,
parameters, and return types come from the deterministic generator and stay there.

Only Go and Python have rewriters. Every other language returns `ErrRewriterNotImplemented`, so
the heal loop cannot change source that it is unable to parse back and verify.

## What happens to a model written operation

1. The model proposes operations for an entity under the schema above.
2. The engine puts each body into a realistic service package in a temp directory, then
   **compiles it alone** with `go build`. It builds against the real store interface and a stand
   in model struct. That struct carries the same fields the generated model gets
   (`engine/service_logic.go`).
3. If the build fails, the engine sends the build error back to the model, up to
   `maxRepairAttempts`, which is **2**.
4. If it still fails, the engine replaces the body with
   `return nil, errors.New("not implemented: <Name>")` and counts the outcome.

Because of that last step, "the generated service always compiles" says almost nothing on its own:
a stub compiles. The metrics that matter are `ops_stubbed` and the repair counts, which the
`gen_pipeline` benchmark reports.

People mix up two different caps. There are **2** repair attempts at the operation level
(`engine.maxRepairAttempts`) and **3** at the task level in the self-heal loop
(`agent.maxHealAttempts`). A document that says "3 repairs then a stub" describes the wrong one.

## The self-heal loop

`FixTask` (`agent/healer.go`) runs when a whole task fails to build. Each attempt adds more
context:

| Attempt | Context added |
|---|---|
| 1 | Error text, existing code, and a classification hint |
| 2 | Plus the architecture summary and up to three retrieved framework-doc chunks |
| 3 | Plus related files from the system graph, under 1500 characters each |

`go build` checks every attempt, and the loop **reverts it on failure** before the next one runs.
A failed repair cannot leave the tree worse than it found it. The loop compiles a cached fix from
the repair memory before it uses it. It evicts that fix when its failures reach twice its
successes. Three consecutive unhealable tasks restore the pre-run snapshot.

## Advisory only

The model performs a security review of generated handlers and services. It warns and never
blocks. If the model is unreachable, the review fails open with a warning. Treat its output as a
prompt for human attention and not as a gate. The gates are the compiler, the tests, and the
Understand pass.

## Running with no model at all

The default logic provider is a replay fixture table (`ai.ReplayProvider`). The whole generation
pipeline runs offline and deterministically with no Ollama present. You opt in to the live model
with `GEN_LOGIC=ollama`.

This makes the pipeline testable. The offline `gen_pipeline` benchmark runs plan, generate,
compile gate, heal, and Understand verify without a GPU. Its unit test asserts that the pipeline
stubs nothing, because the fixtures compile as written.

The planner degrades in a different way. Its entity step has no deterministic fallback, so with
no reachable model it returns zero entities. The benchmark falls back to a fixed schema and
**counts it** in a `planner_fallbacks` metric in the output. Nobody can mistake an offline run for
a measured one.

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

The model only proposes. Extraction, validation, the compile gate, vet, build, test and the
Understand pass are deterministic. Each one checks the model's output before the next stage uses
it.
