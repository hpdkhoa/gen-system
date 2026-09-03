#!/usr/bin/env python3
"""
fetch_tasks.py: download SWE-bench Verified (500 tasks) as tasks.jsonl, stdlib only.

Source: the Hugging Face datasets server, dataset princeton-nlp/SWE-bench_Verified,
split test, 100 rows per page. Fields kept: instance_id, repo, base_commit, version,
problem_statement, patch, FAIL_TO_PASS, PASS_TO_PASS.

Frozen task set discipline: the first run writes tasks.sha256 next to tasks.jsonl.
Later runs verify the download against it and refuse to continue on a mismatch,
unless --refresh is passed. Commit tasks.sha256. tasks.jsonl itself is git ignored
because it is several megabytes of public data that this file can re-fetch.

Usage:  python3 bench/swebench/fetch_tasks.py [--refresh]
"""
import hashlib, json, sys, time, urllib.request
from pathlib import Path

HERE = Path(__file__).resolve().parent
OUT = HERE / "tasks.jsonl"
SHA = HERE / "tasks.sha256"
API = ("https://datasets-server.huggingface.co/rows"
       "?dataset=princeton-nlp/SWE-bench_Verified&config=default&split=test&offset={off}&length=100")
KEEP = ["instance_id", "repo", "base_commit", "version", "problem_statement",
        "patch", "FAIL_TO_PASS", "PASS_TO_PASS"]


def fetch_page(offset):
    req = urllib.request.Request(API.format(off=offset), headers={"User-Agent": "gen-system-bench"})
    for attempt in range(4):
        try:
            with urllib.request.urlopen(req, timeout=60) as r:
                return json.load(r)
        except Exception as e:  # noqa: BLE001
            if attempt == 3:
                raise
            time.sleep(2 * (attempt + 1))


def main():
    refresh = "--refresh" in sys.argv
    if OUT.exists() and SHA.exists() and not refresh:
        have = hashlib.sha256(OUT.read_bytes()).hexdigest()
        want = SHA.read_text().split()[0]
        if have == want:
            print(f"tasks.jsonl present and matches tasks.sha256 ({want[:12]}); nothing to do")
            return 0
        print("tasks.jsonl does not match tasks.sha256; re-fetching", file=sys.stderr)

    rows, offset, total = [], 0, None
    while True:
        page = fetch_page(offset)
        total = page.get("num_rows_total", total)
        got = page.get("rows", [])
        if not got:
            break
        for item in got:
            r = item["row"]
            rows.append({k: r.get(k) for k in KEEP})
        offset += len(got)
        print(f"  fetched {offset}/{total}")
        if total is not None and offset >= total:
            break

    rows.sort(key=lambda r: r["instance_id"])  # deterministic file regardless of page order
    data = "".join(json.dumps(r, ensure_ascii=False, sort_keys=True) + "\n" for r in rows).encode("utf-8")
    digest = hashlib.sha256(data).hexdigest()

    if SHA.exists() and not refresh:
        want = SHA.read_text().split()[0]
        if digest != want:
            print(f"FATAL: downloaded task set ({digest[:12]}) differs from the frozen one "
                  f"({want[:12]}). The dataset changed upstream. Pass --refresh only if you "
                  f"intend to move the frozen task set, and commit the new tasks.sha256.", file=sys.stderr)
            return 2

    OUT.write_bytes(data)
    SHA.write_text(f"{digest}  tasks.jsonl  rows={len(rows)}\n")
    print(f"wrote {OUT} ({len(rows)} tasks) and {SHA.name} ({digest[:12]})")
    repos = sorted({r["repo"] for r in rows})
    print(f"distinct repositories: {len(repos)}")
    for rp in repos:
        print("  ", rp)
    return 0


if __name__ == "__main__":
    sys.exit(main())
