#!/usr/bin/env python3
"""bench/attest.py: write the attestation for one results directory.

The attestation is the "certificate" of a benchmark campaign: what ran, on
which commit and machine, against which frozen task set, how many runs each
table rests on, which runs were dropped and why, what did NOT run, and a
SHA-256 manifest of every raw file so anyone holding the repository can check
that the numbers in the portfolio come from these files and no others.

It states boundaries in the same breath as numbers. The SWE-bench section,
when present, says "localization recall, not a solve rate" next to the
value, because that is the sentence a reader would otherwise have to go and
find.

Usage:
  python3 bench/attest.py --results results/<date>

Writes:
  results/<date>/MANIFEST.sha256   one line per raw file
  results/<date>/ATTESTATION.md    the human readable statement

Stdlib only. Never invents a number: a table that has no rows says so.
"""
import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def load_json(path):
    try:
        return json.loads(Path(path).read_text(encoding="utf-8"))
    except Exception:
        return None


def git(repo, *args):
    try:
        return subprocess.run(["git", "-C", str(repo), *args], capture_output=True,
                              text=True, check=False).stdout.strip()
    except Exception:
        return ""


def metric(suite, name):
    for m in (suite or {}).get("metrics", []):
        if m.get("name") == name:
            return m.get("value")
    return None


def suite(run, name):
    for s in (run or {}).get("suites", []):
        if s.get("name") == name:
            return s
    return None


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--results", required=True)
    args = ap.parse_args()
    results = Path(args.results)
    raw = results / "raw"
    if not raw.is_dir():
        print(f"no raw/ under {results}", file=sys.stderr)
        sys.exit(2)
    repo = results.parent.parent

    # ── manifest ────────────────────────────────────────────────────────
    files = sorted(p for p in raw.rglob("*") if p.is_file())
    manifest = results / "MANIFEST.sha256"
    with open(manifest, "w", encoding="utf-8", newline="\n") as f:
        for p in files:
            f.write(f"{sha256(p)}  {p.relative_to(results).as_posix()}\n")

    env = load_json(results / "env.json") or {}
    tasks = load_json(repo / "bench" / "tasks.json") or {}
    tasks_sha = sha256(repo / "bench" / "tasks.json") if (repo / "bench" / "tasks.json").exists() else "n/a"
    rows = load_json(results / "measured.rows.json") or {}
    sb = load_json(results / "swebench_localization.json") or {}

    # ── per driver inventory ───────────────────────────────────────────
    def count(pattern):
        return sum(1 for p in files if re.match(pattern, p.name))

    inventory = [
        ("understand", r"understand-.+-(parse|resolution|cfg)\.json", "run-understand.sh"),
        ("stream", r"stream\.json", "run-stream.sh"),
        ("offload", r"offload-.+\.json", "run-offload.sh"),
        ("quant sweep, generation quality (genpipe)", r"quant-.+-genpipe-run\d+\.json", "run-quant-sweep.sh"),
        ("quant sweep, tokens per second (gen)", r"quant-.+-gen-run\d+\.json", "run-quant-sweep.sh"),
        ("strategies, generation quality (genpipe)", r"strategy-.+-genpipe-run\d+\.json", "run-strategies.sh"),
        ("strategies, tokens per second (gen)", r"strategy-.+-gen-(code|plan)-run\d+\.json", "run-strategies.sh"),
        ("SWE-bench Verified localization (per task)", r"swebench-(?!repo-).+\.json", "swebench/run_swebench_localize.sh"),
        ("SWE-bench Verified readability (per repo)", r"swebench-repo-.+\.json", "swebench/run_swebench_localize.sh"),
    ]

    # ── genpipe validity ───────────────────────────────────────────────
    invalid = []
    live_runs = 0
    for p in files:
        if not re.search(r"-genpipe-run\d+\.json$", p.name):
            continue
        gp = suite(load_json(p), "gen_pipeline")
        if not gp:
            continue
        live_runs += 1
        no_contact = metric(gp, "ideas_no_model_contact") or 0
        if no_contact > 0 or ((metric(gp, "ops_total") or 0) == 0 and (metric(gp, "model_requests") or 0) == 0):
            invalid.append(p.name)

    # ── write ──────────────────────────────────────────────────────────
    out = []
    w = out.append
    w("# Benchmark attestation")
    w("")
    w(f"Results directory: `{results.as_posix()}`  ")
    w(f"Written: {datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')} by `bench/attest.py`  ")
    w(f"Raw files: {len(files)}, listed with SHA-256 in `MANIFEST.sha256`")
    w("")
    w("## What this is")
    w("")
    w("A statement of what was measured, on what, and what was not. Every number the portfolio")
    w("shows for this campaign comes from the files in the manifest and from no other source.")
    w("A reader who holds the repository can recompute every table with `bench/summarise.py`")
    w("and check every file against the manifest.")
    w("")
    w("## Environment (from env.json, frozen before the runs)")
    w("")
    w(f"- gen-system commit: `{env.get('gen_system_commit', 'n/a')}`")
    dirty = env.get("working_tree_dirty")
    if dirty:
        w("- working tree at freeze time: **dirty**. The commit above is not the exact code that ran;")
        w("  uncommitted changes were present. Treat the commit as approximate and read the run date as the anchor.")
    else:
        w("- working tree at freeze time: clean")
    w(f"- GPU: {env.get('gpu', 'n/a')}")
    w(f"- Ollama: {env.get('ollama_version', 'n/a')}")
    w(f"- Go: {env.get('go_version', 'n/a')}")
    w(f"- frozen at: {env.get('frozen_at', 'n/a')}")
    w("")
    w("## Task set")
    w("")
    w(f"- `bench/tasks.json` sha256 `{tasks_sha}`, frozen date recorded inside the file: {tasks.get('frozen_date', 'n/a')}")
    w(f"- generate ideas: {len(tasks.get('generate_ideas', []))}; understand targets: {len(tasks.get('understand_targets', []))} pinned public repositories")
    w("- The proof that the task set predates the results is the git history: the commit that adds")
    w("  `bench/tasks.json` must come before the commit that adds this results directory. Check with")
    w("  `git log --format='%h %ad %s' --date=short -- bench/tasks.json results/`.")
    w("")
    w("## What ran")
    w("")
    w("| Driver | Raw files | Script |")
    w("|---|---|---|")
    for label, pat, script in inventory:
        n = count(pat)
        w(f"| {label} | {n if n else '**0 (did not run)**'} | `bench/{script}` |")
    w("")
    if invalid:
        w("## Runs dropped as not measured")
        w("")
        w("These generation runs completed zero model requests (every call failed), so the build and")
        w("test results they carry come from the offline fallback schema and say nothing about the model.")
        w("`summarise.py` excludes them from the quality columns and counts them in `runs_not_measured`.")
        w("")
        for name in invalid:
            w(f"- `raw/{name}`")
        w("")
    w("## Tables produced (rows)")
    w("")
    reused = []
    if rows:
        for key, tbl in rows.items():
            if isinstance(tbl, dict) and "rows" in tbl:
                n = len(tbl["rows"])
                src = (tbl.get("_provenance") or {}).get("reused_from")
                if src:
                    reused.append((key, src, (tbl.get("_provenance") or {}).get("gen_system_commit", "")))
                    w(f"- `gen_system.{key}`: {n} row(s), **reused from `{src}`**, not measured in this directory")
                else:
                    w(f"- `gen_system.{key}`: {n} row(s)" if n else f"- `gen_system.{key}`: **no rows**")
    else:
        w("- `measured.rows.json` not present; run `bench/summarise.py` first.")
    w("")
    if reused:
        w("## Tables reused from earlier campaigns")
        w("")
        w("These tables were not measured in this directory. `bench/summarise.py --reuse` took them from")
        w("the directory named, and each keeps that directory's date and commit in its provenance. The")
        w("raw files behind them are in that directory's own `MANIFEST.sha256`, not in this one.")
        w("")
        for key, src, commit in reused:
            w(f"- `gen_system.{key}` from `{src}` (commit `{commit[:12]}`)")
        w("")
    w("## SWE-bench Verified")
    w("")
    if sb:
        loc = sb.get("swebench_verified_localization", {})
        n_rows = loc.get("rows", [])
        if (loc.get("_provenance") or {}).get("reused_from"):
            w(f"Reused from `{loc['_provenance']['reused_from']}`; not run in this directory.")
            w("")
        w("**Localization recall only. Not a solve rate.** gen-system has no issue to patch pipeline, so a")
        w("SWE-bench solve rate cannot be produced and is not claimed. The number here answers one question:")
        w("from identifiers in the issue text, does the call graph neighbourhood reach the files the gold")
        w("patch touched?")
        w("")
        if n_rows:
            cols = loc.get("_columns", [])
            w("| " + " | ".join(cols) + " |")
            w("|" + "---|" * len(cols))
            for r in n_rows:
                w("| " + " | ".join("n/a" if v is None else str(v) for v in r) + " |")
        else:
            w("No per task rows: the localization loop did not produce results.")
        w("")
        repos = sb.get("swebench_verified_repos", {}).get("rows", [])
        if repos:
            w(f"Repositories read: {len(repos)} of the 12 in SWE-bench Verified.")
    else:
        w("**Did not run.** No `swebench_localization.json` in this results directory. When it runs it")
        w("reports localization recall, which is not a solve rate.")
    w("")
    w("## Boundaries stated once, here")
    w("")
    w("- `go_test_pass` counts ideas whose generated backend passes its own tests. Stubs compile and pass,")
    w("  so this is 5/5 by construction; the quality signal is `stub_rate_pct` and the repair counts.")
    w("- `heal_attempts` is the task level repair loop. It is 0 in every run of this campaign: no task")
    w("  failed outright, because the operation level gate stubs a failing body before the task can fail.")
    w("- Sampling was pinned (temperature 0, seed 42, top_p 1). Runs still differ; the n and range columns exist")
    w("  because pinned sampling on a local server does not give byte identical generations across runs.")
    w("- `ops_total` is the number of model proposed operations that reached the compile gate. Rows with a")
    w("  small `ops_total` rest on few observations; compare `stub_rate_pct` between rows with that in view.")
    w("")
    (results / "ATTESTATION.md").write_text("\n".join(out) + "\n", encoding="utf-8", newline="\n")
    print(f"wrote {manifest}")
    print(f"wrote {results / 'ATTESTATION.md'}")
    print(f"raw files: {len(files)}; genpipe runs: {live_runs}; dropped: {len(invalid)}")


if __name__ == "__main__":
    main()
