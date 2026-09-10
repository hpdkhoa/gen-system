# Benchmark attestation

Results directory: `results/2026-09-09`  
Written: 2026-09-10T01:23:36Z by `bench/attest.py`  
Raw files: 194, listed with SHA-256 in `MANIFEST.sha256`

## What this is

A statement of what was measured, on what, and what was not. Every number the portfolio
shows for this campaign comes from the files in the manifest and from no other source.
A reader who holds the repository can recompute every table with `bench/summarise.py`
and check every file against the manifest.

## Environment (from env.json, frozen before the runs)

- gen-system commit: `cfb0bff217481e13bb3de720ddeb672a73167027`
- working tree at freeze time: clean
- GPU: NVIDIA GeForce RTX 4060 Ti, 16380 MiB, 595.71.05
- Ollama: ollama version is 0.24.0
- Go: go version go1.25.0 linux/amd64
- frozen at: 2026-09-09T16:52:09Z

## Task set

- `bench/tasks.json` sha256 `426fd51ac0b0874eb19c512a8ba8b73b1e7f742480a68252ade9b44266922ef8`, frozen date recorded inside the file: 2026-08-27
- generate ideas: 5; understand targets: 4 pinned public repositories
- The proof that the task set predates the results is the git history: the commit that adds
  `bench/tasks.json` must come before the commit that adds this results directory. Check with
  `git log --format='%h %ad %s' --date=short -- bench/tasks.json results/`.

## What ran

| Driver | Raw files | Script |
|---|---|---|
| understand | 12 | `bench/run-understand.sh` |
| stream | 1 | `bench/run-stream.sh` |
| offload | 4 | `bench/run-offload.sh` |
| quant sweep, generation quality (genpipe) | 12 | `bench/run-quant-sweep.sh` |
| quant sweep, tokens per second (gen) | 12 | `bench/run-quant-sweep.sh` |
| strategies, generation quality (genpipe) | 9 | `bench/run-strategies.sh` |
| strategies, tokens per second (gen) | 18 | `bench/run-strategies.sh` |
| SWE-bench Verified localization (per task) | 60 | `bench/swebench/run_swebench_localize.sh` |
| SWE-bench Verified readability (per repo) | 4 | `bench/swebench/run_swebench_localize.sh` |

## Tables produced (rows)

- `gen_system.quantization_sweep`: 4 row(s)
- `gen_system.offload_curve`: 4 row(s)
- `gen_system.streaming`: 2 row(s)
- `gen_system.two_model_strategies`: 3 row(s)
- `gen_system.understand_public_repos`: 4 row(s)

## SWE-bench Verified

**Localization recall only. Not a solve rate.** gen-system has no issue to patch pipeline, so a
SWE-bench solve rate cannot be produced and is not claimed. The number here answers one question:
from identifiers in the issue text, does the call graph neighbourhood reach the files the gold
patch touched?

| hops | file_recall_mean | hit_rate_pct | mean_neighbourhood_files | median_neighbourhood_files | n_tasks |
|---|---|---|---|---|---|
| 0 | 0.615 | 63.3 | 201.933 | 92.5 | 60 |
| 1 | 0.615 | 63.3 | 262.5 | 177.5 | 60 |
| 2 | 0.674 | 70.0 | 464.8 | 421.5 | 60 |

Repositories read: 2 of the 12 in SWE-bench Verified.

## Boundaries stated once, here

- `go_test_pass` counts ideas whose generated backend passes its own tests. Stubs compile and pass,
  so this is 5/5 by construction; the quality signal is `stub_rate_pct` and the repair counts.
- `heal_attempts` is the task level repair loop. It is 0 in every run of this campaign: no task
  failed outright, because the operation level gate stubs a failing body before the task can fail.
- Sampling was pinned (temperature 0, seed 42, top_p 1). Runs still differ; the n and range columns exist
  because pinned sampling on a local server does not give byte identical generations across runs.
- `ops_total` is the number of model proposed operations that reached the compile gate. Rows with a
  small `ops_total` rest on few observations; compare `stub_rate_pct` between rows with that in view.

