# bench: the measurement campaign (WP0 to WP4)

The scripts here tie every number in the portfolio to a commit, a date, a task set, and n runs
with a range. Nobody types a number in the writeups by hand; `summarise.py` generates the rows.

## One-time setup (before any measured run)

```
./bench/freeze-env.sh          # writes bench/env.json (commit, GPU, ollama list)
git add bench/env.json bench/tasks.json
git commit -m "bench: freeze environment and task set"
```

The commit timestamp shows that the tasks existed before the results. `run-all.sh` refuses to
run on a dirty tree.

The campaign needs these models in ollama: `qwen3:14b`, `qwen2.5-coder:14b`, and the three
quant-study tags (`ollama pull deepseek-coder-v2:16b-lite-instruct-q4_K_M` and so on. See
tasks.json).

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
| `swebench/run_swebench_localize.sh` | swebench_verified_localization (recall, not a solve rate; see below). `run-all.sh` runs it with `SWEBENCH_LIMIT` tasks (default 60, `0` = all 500, `skip` = leave out) | minutes for 60 tasks, hours for 500 |
| `summarise.py` | measured.rows.json → measured.json (with `--measured`) | seconds |
| `attest.py` | MANIFEST.sha256 + ATTESTATION.md: what ran, what did not, what the summary dropped (v26.32) | seconds |
| `run-partial.sh` | only the drivers you name; it reuses every other table from an earlier results directory without re-dating (v26.35) | depends on the drivers |

`verify-postgres.sh` fills no table. It generates an app with the Postgres backend, applies its
migrations to a live database, runs CRUD over HTTP and checks that the foreign key rejects an
orphan row. Run it on a box with a Postgres you can write to; `--help` lists the options.

## What changed vs the old quant_sweep.sh (now in scripts/legacy/)

The old sweep's quality axis was compile pass over the generation gates. That was always 100
percent, because the stub path guarantees a green build. The new main number comes from
`genbench genpipe` (v26.24): stub rate, op-repair attempts, heal attempts, go-test pass over 5
frozen ideas, per quant. `bench/run-quant-sweep.sh` replaces `scripts/legacy/quant_sweep.sh`
(formerly at the repo root).

## SWE-bench Verified, localization only

`bench/swebench/run_swebench_localize.sh` measures whether the Understand graph reaches the
files a gold patch touched, from seed identifiers in the issue text, on all 500 SWE-bench
Verified tasks. That is the file recovery line of DeepDiscovery (arXiv 2606.22906). It is
not a solve rate. gen-system has no issue to patch pipeline, so you cannot compare the 78.6
percent solve rate in that paper with anything this produces. The folder README states the
boundary in full, and every result prints it.

## Provenance

Every table `summarise.py` writes carries `_provenance` (date, gen-system commit, task set,
GPU). It also carries per-row `n` plus min/max where the raw data has them. A config with no raw
data gets no row. If a value is null, the harness did not measure it. The harness keeps the null
and does not fill it in.

**`summarise.py` drops runs that measured nothing and does not average them (v26.32).** A
`genpipe` run in live mode that completed zero model requests carries build and test results
from the offline fallback schema. Those results say nothing about the model. `summarise.py`
leaves such runs out of the quality columns and counts them in `runs_not_measured`. `genpipe`
itself marks every such idea as a failed case with `NOT MEASURED` in the note. The 2026-09-04
campaign found this the hard way. Strategy B set `OLLAMA_KEEP_ALIVE=-1`, and the door sent it
as the string "-1". Ollama rejected every request, and the row would have read as a 0 percent
stub rate. The door now sends bare integers as numbers.

## Partial runs and reuse (v26.35)

`bench/run-partial.sh --run strategies,swebench:60 --reuse results/2026-09-05` runs only the
drivers you name, into today's directory. Then it fills every other table from the reuse
directories in the order you give. A reused table keeps its own date and commit in
`_provenance` and gains a `reused_from` field, so nothing gets a new date. `summarise.py` and
`summarise_swebench.py` take the same `--reuse` flag on their own. The attestation for the new
directory lists every reused table with its source directory. That directory's own manifest
lists the raw files behind a reused table. With no `--reuse`, the script tries every other
`results/*` directory, newest first. `run-all.sh` remains the full campaign from fresh.

## The results directory and the date

`run-all.sh` and `run-partial.sh` name the results directory after the day the campaign starts
and export that date to every driver. A campaign that runs past midnight stays in one
directory (v26.36). A single driver run on its own uses the clock at that moment. To choose
the name yourself: `RUN_DATE=2026-09-10 ./bench/run-all.sh`.

## Attestation

`bench/attest.py --results results/<date>` writes two files. `MANIFEST.sha256` lists every
raw file with its hash. `ATTESTATION.md` states the environment, the task set hash, which
driver produced how many raw files, the runs the summary dropped as not measured, and the row
count of every table. It carries the SWE-bench section with its boundary next to the number. It
ends with the standing caveats: `go_test_pass` is 5 of 5 by construction, `heal_attempts` is 0
because the op gate stubs first, and pinned sampling still varies across runs. `run-all.sh`
runs it last. Commit both files with the results.
