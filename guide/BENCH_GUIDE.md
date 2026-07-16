# BENCH_GUIDE — gen-system reliability + performance bench

`genbench` is a new binary, separate from the existing `bench` (v15 capability
suite which stays as-is). It produces the four reliability numbers and the
four performance numbers that an incubator demo or a design-partner
conversation will ask for.

This guide is the runbook for an **RTX 4060 Ti 16 GB** running
`deepseek-coder-v2:16b-lite-instruct-q5_K_M`. Other hardware works; the
numbers move.

---

## 0. One-time setup

### Ollama and the model

```bash
# Install Ollama if you don't have it (Linux):
curl -fsSL https://ollama.com/install.sh | sh

# Pull the model we locked. ~10 GB on disk, ~10–11 GB resident VRAM.
ollama pull deepseek-coder-v2:16b-lite-instruct-q5_K_M

# Keep it resident — otherwise the second LLM call pays a 4–6 s reload cost
# every time and you'll never get deterministic output.
export OLLAMA_KEEP_ALIVE=-1

# Restart the daemon so the env var takes effect, then warm the model.
systemctl --user restart ollama        # or `ollama serve` in a terminal
ollama run deepseek-coder-v2:16b-lite-instruct-q5_K_M "ok" >/dev/null
```

### Build genbench

```bash
go build -o genbench ./generator/cmd/genbench
```

Sanity:

```bash
./genbench help
./genbench model
```

The `model` subcommand checks that Ollama is reachable, the requested model
is present, and that **second_load_ms** is much smaller than
**first_load_ms** — if it isn't, `OLLAMA_KEEP_ALIVE` isn't doing its job.

---

## 1. Recommended workflow

The order matters: cheap and local first, AI last, so you only spend the GPU
budget when everything else is green.

```bash
# Repo-only suites (no Ollama needed). About a minute on a real codebase.
./genbench all --root /path/to/your/repo --skip-ai \
    --copybooks /path/to/copybooks \
    --out bench-baseline.json

# Then the GPU suites separately so a flaky LLM run doesn't lose the
# reliability numbers.
./genbench gen --runs 5 --out bench-gen.json
./genbench determinism --runs 10 --out bench-determinism.json
```

The `all` driver runs everything in one shot when you're confident the LLM is
warm:

```bash
./genbench all \
    --root /path/to/your/repo \
    --copybooks /path/to/copybooks \
    --labels labels.effects.json \
    --skill .gen/project.skill \
    --model deepseek-coder-v2:16b-lite-instruct-q5_K_M \
    --runs 5 \
    --out bench-$(date +%Y%m%d-%H%M).json
```

Every subcommand writes a JSON to `--out` (default `bench-<ts>.json`) and
prints a terminal report. Exit code is non-zero if any case failed.

---

## 2. Subcommand reference

### `genbench model` — Ollama doctor

What it checks:

- Daemon reachable at `--host` (default `http://localhost:11434`)
- `--model` is present in `ollama list`
- Model stays resident: second call's `load_duration` should be near zero.

Run this first whenever you suspect anything LLM-related. If
`model_stays_resident_between_calls` fails, set
`OLLAMA_KEEP_ALIVE=-1` and restart the daemon.

### `genbench resolution` — symbol-resolution recall

The headline reliability number. Of every call edge the index sees, what
percentage resolved to a concrete symbol (CalleeID non-empty)?

```bash
./genbench resolution \
    --root /path/to/banking-codebase \
    --copybooks /path/to/copybooks \
    --threshold 0.97
```

Targets:

- **>97 %** overall: ship-ready for the COBOL demo.
- **90–96 %**: usable; expect the human-in-the-loop UI to absorb the rest.
- **<90 %**: the COBOL frontend has real gaps. Look at the per-language
  recall metrics in the output — they tell you which frontend is failing.

The suite also reports `recall_<lang>` for each language present, with a
looser per-language floor (>85 %).

### `genbench cfg` — CFG correctness

Four structural invariants, run over every CFG `flow.Build` produced:

1. `Entry` is a valid block ID
2. Every `Exits[i]` is a valid block ID
3. No orphan blocks (every non-entry block has ≥1 predecessor)
4. Every successor referenced from any block is a valid block ID

Any violation is a parser/CFG-builder bug — the graph view in the Studio
can't render an invalid CFG without breaking, and downstream analyses
(dataflow, slicing) produce nonsense. The `Notes` field shows up to five
example violations.

### `genbench effects` — effect-inference precision/recall

You need a labels file. Format:

```json
{
  "labels": [
    {
      "file": "generator/internal/symbols/index.go",
      "symbol": "Save",
      "kind": "method",
      "receiver": "Index",
      "effects": ["fs"]
    },
    {
      "file": "generator/internal/ai/ollama.go",
      "symbol": "doRequest",
      "kind": "func",
      "effects": ["network"]
    }
  ]
}
```

Notes:

- Paths in `file` are **repo-relative** with forward slashes. `genbench`
  matches them as a suffix against the absolute path each Symbol carries,
  so the labels file works whether you invoke with `--root .` or
  `--root /abs/path`.
- `kind` is `func`, `method`, or `procedure`. Methods require `receiver`.
- `effects` is any subset of `db`, `fs`, `network`, `pure`.

Score:

```bash
./genbench effects --root . --labels labels.effects.json
```

The suite reports precision and recall per effect kind, plus the
`label_match_rate` (>90 % must match a real symbol — anything lower means
the labels are stale).

**To get to a meaningful number you need ≥50 labels.** Pick a mix of
functions you have already audited; rotate when frontends change.

### `genbench diff` — schema diff regression

Locks in the v19 ghost-entry fix and the basic add/remove/type-change
behaviours. No project root needed. Runs in milliseconds. Run after any
patch to `compiler/diff/`.

### `genbench parse` — cold parse throughput per language

Walks the repo, groups files by language, parses each group with a fresh
cache. Reports `files_per_sec_<lang>`, `kb_per_sec_<lang>`, and overall
throughput. Treat the first run as the baseline; use `regression` to detect
slowdowns later.

```bash
./genbench parse --root /path/to/repo
```

### `genbench loadctx` — LoadContext end-to-end latency

Runs `LoadContext` in four configurations and times each:

- `plain`: vanilla `LoadContext(root)`
- `with_copybook`: `LoadContextWithOpts` with the copybook path supplied
- `warm_cache`: a second plain run after the disk cache has been populated
- `skill_warmup`: `LoadContextWithSkill` when `--skill` points at a real file

This is the "open my project" latency. Target: under 30 seconds for a
10 k-file repo on this hardware. If `warm_cache` is not significantly faster
than `plain`, the disk cache isn't doing its job.

### `genbench incremental` — watcher event latency

Writes a Go file into a temp dir, runs a watcher, modifies the file, and
measures:

- `first_event_latency_ms`: write → first `Event` on the channel
- `index_updated_latency_ms`: write → `idx.ByName("NewSymbol")` returns a hit

Floor is bounded by the watcher's poll interval (set to 100 ms in this
suite) plus the 200 ms debounce window. Expect ~300–500 ms end-to-end on a
healthy box.

### `genbench gen` — LLM generation latency

Runs a small fixed CRUD-shaped prompt N times. Reports:

- `tokens_per_sec_mean`
- `eval_ms_mean` (time spent in token generation only)
- `total_ms_mean` (wall-clock including prompt eval)
- `eval_count_mean` (tokens generated per response)

Expected on a 4060 Ti 16 GB with the locked model: **~50–65 tok/s**.

Anything below 30 tok/s for this model means the GPU is being shared or the
model spilled to CPU — check `nvidia-smi` during a run.

### `genbench determinism` — temp=0 determinism check

Issues the same prompt N times at `temperature=0`, `seed=42`. With those
settings every output **must** be byte-identical. They often aren't (KV
cache state, batch composition leak entropy) — this surfaces the gap.

```bash
./genbench determinism --runs 10
```

- `all_outputs_identical`: ideal pass. One distinct SHA-256 across all runs.
- `dominant_output_at_least_80%`: weaker target — at least 8 of 10 runs
  produce the same text. If you can't even hit 80 %, putting "deterministic"
  on a pitch deck is a problem; fix the request path before claiming it.

### `genbench all` — full bench

Runs everything in this order:

1. `diff` (cheap, no I/O)
2. `parse` (filesystem only)
3. `resolution` (parse + index)
4. `cfg` (parse + index + flow)
5. `loadctx` (full context load, four configurations)
6. `incremental` (watcher in a temp dir)
7. `effects` (only if `--labels` given)
8. `model` (Ollama doctor)
9. `gen` (LLM)
10. `determinism` (LLM)

Pass `--skip-ai` to skip steps 8–10 if Ollama isn't available.

### `genbench regression` — compare two runs

```bash
./genbench regression \
    --old bench-20260601-120000.json \
    --new bench-20260608-120000.json \
    --tolerance 0.05
```

Diffs metric-by-metric. Exits non-zero if any metric is worse than `--old`
by more than `--tolerance` (relative). "Worse" respects each metric's
`higher_is_better` flag — `tokens_per_sec_mean` is higher-is-better,
`eval_ms_mean` is lower-is-better.

Use this in CI: after a patch lands, run `genbench all --skip-ai`, then
`genbench regression` against the last good run. Any metric drop fails the
build.

---

## 3. Suggested first-week sequence on the 4060 Ti

1. **Day 1.** Pull the model. Set `OLLAMA_KEEP_ALIVE=-1`. Run
   `genbench model` until it's all green. Record a baseline run:
   `genbench all --root . --copybooks ./demo-cobol/copybooks --skip-ai
   --out bench-baseline-noai.json`
2. **Day 1.** Run `genbench gen` and `genbench determinism`. If
   determinism is below 100 %, look at the request path for entropy
   sources (map iteration order, timestamps, non-deterministic sort
   keys) before claiming determinism anywhere.
3. **Day 2.** Run `genbench resolution` against
   `demo-cobol/`. Confirm it's at or above 97 %. If not, look at the
   per-language recall metrics — the language with the lowest score is
   where to spend the next patch.
4. **Day 2.** Write a labels file with 50 functions across the demo
   COBOL codebase and the engine's own Go code. Run `genbench effects`.
   Target P>=0.85 and R>=0.85 per kind. Below that, the effect inference
   rules in `flow/flow.go` need tightening.
5. **Day 3.** Re-run `genbench all` and freeze the JSON as
   `bench-week1.json`. From here on, every patch's CI runs
   `genbench all --skip-ai` and then `regression --old bench-week1.json
   --tolerance 0.05`.

That's the regression net. With it in place, the engine improvements
become measurable, and the UI work in Wave 2 of the redesign cannot
silently break the engine.

---

## 4. Output format

Every run writes one JSON file. Schema:

```json
{
  "schema": "genbench/v1",
  "started_at": "2026-06-08T...",
  "finished_at": "...",
  "host":  { "os": "linux", "arch": "amd64", "num_cpu": 16, "go_version": "go1.25.0" },
  "model": { "name": "deepseek-coder-v2:16b-lite-instruct-q5_K_M",
             "endpoint": "http://localhost:11434" },
  "project_root": "/abs/path",
  "suites": [
    {
      "name": "resolution_recall",
      "cases":   [ { "name": "...", "pass": true, "got": "...", "want": "..." } ],
      "metrics": [ { "name": "resolution_recall", "value": 0.97, "unit": "ratio",
                     "higher_is_better": true } ],
      "duration_ns": 12345,
      "notes": "symbols=12345 edges=6789"
    }
  ]
}
```

Two views: the **terminal report** that the binary prints (failures first,
then passing cases, then metrics) and the **JSON** that `regression`
consumes. Pipe the terminal report to a file if you want both.

---

## 5. Footguns

- Don't compare runs across different `host.go_version`, OS, or CPU count.
  The host info is recorded; trust it.
- Don't compare runs across different `model.name`. Pick one and stay there
  until you have a reason to switch.
- The `model` doctor's `model_stays_resident_between_calls` flag is the
  single most common cause of bad-looking LLM numbers. If you didn't set
  `OLLAMA_KEEP_ALIVE=-1`, fix that before claiming the GPU is slow.
- `incremental` writes a temp dir under the system temp path. If your
  temp dir is on a filesystem with second-precision timestamps (some
  network mounts), the suite explicitly bumps mtime forward by 2 s so the
  poll picks the change up — but a clock skew between writer and watcher
  can still mask the change. Use a local SSD-backed temp dir.
- `parse` honours a small skip list (`.git`, `.gen`, `node_modules`,
  `vendor`, `output`, `build`, `dist`, `.idea`, `.vscode`). It does **not**
  honour the engine's own `shouldSkip`, which is unexported — close enough
  for benchmarking; off by a few files in either direction is noise at
  this scale.
