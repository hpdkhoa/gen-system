# bench: the measurement campaign (WP0 to WP4)

Everything here exists so that every number in the portfolio traces to a
commit, a date, a task set, and n runs with a range. Nothing in the writeups
is typed by hand; `summarise.py` generates the rows.

## One-time setup (before ANY measured run)

```
./bench/freeze-env.sh          # writes bench/env.json (commit, GPU, ollama list)
git add bench/env.json bench/tasks.json
git commit -m "bench: freeze environment and task set"
```

The commit timestamp is the proof the tasks were fixed before the results
existed. `run-all.sh` refuses to run on a dirty tree.

Models needed in ollama: `qwen3:14b`, `qwen2.5-coder:14b`, and the three
quant-study tags (`ollama pull deepseek-coder-v2:16b-lite-instruct-q4_K_M`
and so on. See tasks.json).

## The whole campaign

```
MEASURED=~/code/my-portfolio/benchmarks/results/measured.json ./bench/run-all.sh
```

Or piecewise (each writes raw JSON to `results/<date>/raw/`):

| Script | Fills | Time (rough) |
|---|---|---|
| `run-understand.sh` | understand_public_repos (4 pinned repos, parser path, no GPU) | minutes |
| `run-stream.sh` | streaming (TTFT stream on/off, n=3) | minutes |
| `run-offload.sh` | offload_curve (tok/s + VRAM vs OLLAMA_NUM_GPU) | ~30 min |
| `run-quant-sweep.sh` | quantization_sweep (genpipe 5 ideas + tok/s, n=3 per quant + production) | hours |
| `run-strategies.sh` | two_model_strategies (A sequential+keepalive, B both resident, C single shared, n=3) | hours |
| `summarise.py` | measured.rows.json → measured.json (with `--measured`) | seconds |

## What changed vs the old quant_sweep.sh (repo root)

The old sweep's quality axis was compile pass over the generation gates. That was always 100
percent, because the stub path guarantees a green build. The new headline
comes from `genbench genpipe` (v26.24): stub rate, op-repair attempts, heal
attempts, go-test pass over 5 frozen ideas, per quant. `quant_sweep.sh` at the
repo root is superseded by `bench/run-quant-sweep.sh`.

## Provenance

Every table `summarise.py` writes carries `_provenance` (date, gen-system
commit, task set, GPU) and per-row `n` plus min/max where the raw data has
them. A config with no raw data gets no row. If a value is null, the harness
did not measure it. That is intended behaviour, not a bug to paper over.
