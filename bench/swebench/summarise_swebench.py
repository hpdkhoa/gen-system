#!/usr/bin/env python3
"""
summarise_swebench.py: aggregate the per task localization results into tables.

Reads results/<date>/raw/swebench-<instance>.json (one genbench Run each) and the
per repository swebench-repo-*-parse.json / -resolution.json files. Writes
results/<date>/swebench_localization.json, and with --measured merges two tables into
the portfolio's measured.json:

  gen_system.swebench_verified_localization
      [hops, file_recall_mean, hit_rate_pct, mean_neighbourhood_files,
       median_neighbourhood_files, n_tasks]
  gen_system.swebench_verified_repos
      [repo, files_parsed, parse_errors, internal_recall_pct, call_edges_internal]

Every value comes from a raw file. A hop with no data writes no row.

The boundary, printed with every run: this is localization recall, the file recovery
line of DeepDiscovery (arXiv 2606.22906). It is not a solve rate, and gen-system has no
issue to patch pipeline, so the 78.6 percent solve rate in that paper is not comparable
to anything here.

Usage:  python3 bench/swebench/summarise_swebench.py --results results/<date> [--measured PATH]
"""
import argparse, json, re, statistics
from pathlib import Path

BOUNDARY = ("Localization recall only. Not a solve rate. gen-system has no issue to patch "
            "pipeline, so a SWE-bench solve rate cannot be produced and is not claimed.")


def load(p):
    try:
        return json.loads(p.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        print(f"  !! {p.name}: not valid JSON, skipped"); return None


def suite(run, name):
    for s in run.get("suites", []):
        if s.get("name") == name:
            return s
    return None


def metric(s, name):
    for m in (s or {}).get("metrics", []):
        if m.get("name") == name:
            return m.get("value")
    return None


def mean(xs):
    xs = [x for x in xs if x is not None]
    return round(statistics.fmean(xs), 3) if xs else None


def median(xs):
    xs = [x for x in xs if x is not None]
    return round(statistics.median(xs), 1) if xs else None


def repo_of(instance):
    # instance ids look like owner__name-1234
    return instance.rsplit("-", 1)[0].replace("__", "/")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--results", required=True)
    ap.add_argument("--measured")
    args = ap.parse_args()
    results = Path(args.results); raw = results / "raw"
    if not raw.is_dir():
        raise SystemExit(f"no raw/ under {results}")

    env = {}
    if (results / "env.json").exists():
        env = json.loads((results / "env.json").read_text(encoding="utf-8"))
    sha = ""
    shafile = Path(__file__).resolve().parent / "tasks.sha256"
    if shafile.exists():
        sha = shafile.read_text().split()[0][:12]
    provenance = {
        "date": results.name,
        "gen_system_commit": env.get("gen_system_commit", "unknown"),
        "task_set": f"SWE-bench Verified, princeton-nlp, frozen sha256 {sha or 'unknown'}",
        "gpu": "not used (parser path only)",
        "metric": BOUNDARY,
    }

    # ── per task ──────────────────────────────────────────────────────────
    per_task = []
    for p in sorted(raw.glob("swebench-*.json")):
        if "-repo-" in p.name:
            continue
        run = load(p)
        if not run:
            continue
        s = suite(run, "localization_recall")
        if not s:
            continue
        instance = p.name[len("swebench-"):-len(".json")]
        hops = sorted({int(m.group(1)) for mm in s.get("metrics", [])
                       for m in [re.match(r"hop(\d+)_file_recall", mm.get("name", ""))] if m})
        row = {"instance": instance, "repo": repo_of(instance),
               "seeds_total": metric(s, "seeds_total"), "seeds_matched": metric(s, "seeds_matched"),
               "gold_files": metric(s, "gold_files_total"), "hops": {}}
        for h in hops:
            row["hops"][h] = {
                "recall": metric(s, f"hop{h}_file_recall"),
                "hit": (metric(s, f"hop{h}_gold_files_hit") or 0) > 0,
                "files": metric(s, f"hop{h}_neighbourhood_files"),
            }
        per_task.append(row)

    loc_rows = []
    all_hops = sorted({h for r in per_task for h in r["hops"]})
    for h in all_hops:
        rs = [r["hops"][h] for r in per_task if h in r["hops"]]
        if not rs:
            continue
        loc_rows.append([h, mean([r["recall"] for r in rs]),
                         round(100.0 * sum(1 for r in rs if r["hit"]) / len(rs), 1),
                         mean([r["files"] for r in rs]), median([r["files"] for r in rs]), len(rs)])

    by_repo = {}
    for r in per_task:
        by_repo.setdefault(r["repo"], []).append(r)
    repo_loc = []
    top = all_hops[-1] if all_hops else None
    for repo, rs in sorted(by_repo.items()):
        if top is None:
            break
        hs = [r["hops"][top] for r in rs if top in r["hops"]]
        repo_loc.append([repo, top, mean([x["recall"] for x in hs]),
                         round(100.0 * sum(1 for x in hs if x["hit"]) / max(1, len(hs)), 1),
                         round(100.0 * sum(1 for r in rs if (r["seeds_matched"] or 0) > 0) / max(1, len(rs)), 1),
                         len(rs)])

    # ── per repository readability ────────────────────────────────────────
    repo_rows = []
    names = sorted({m.group(1) for p in raw.glob("swebench-repo-*-parse.json")
                    for m in [re.match(r"swebench-repo-(.+)-parse\.json", p.name)] if m})
    for name in names:
        files = errs = rec = edges = None
        run = load(raw / f"swebench-repo-{name}-parse.json")
        s = suite(run or {}, "parse_throughput")
        if s:
            per_lang = [m.get("value") for m in s.get("metrics", []) if m.get("name", "").startswith("files_total_")]
            files = int(sum(per_lang)) if per_lang else None
            errs = metric(s, "parse_errors")
        rp = raw / f"swebench-repo-{name}-resolution.json"
        if rp.exists():
            s = suite(load(rp) or {}, "resolution_recall")
            v = metric(s, "resolution_recall_internal")
            rec = round(100 * v, 1) if v is not None else None
            edges = metric(s, "call_edges_internal")
        repo_rows.append([name.replace("__", "/"), files, errs, rec, edges])

    out = {
        "_provenance": provenance,
        "swebench_verified_localization": {
            "_columns": ["hops", "file_recall_mean", "hit_rate_pct", "mean_neighbourhood_files",
                         "median_neighbourhood_files", "n_tasks"],
            "_provenance": provenance, "rows": loc_rows},
        "swebench_verified_localization_by_repo": {
            "_columns": ["repo", "hops", "file_recall_mean", "hit_rate_pct", "seeds_matched_pct", "n_tasks"],
            "_provenance": provenance, "rows": repo_loc},
        "swebench_verified_repos": {
            "_columns": ["repo", "files_parsed", "parse_errors", "internal_recall_pct", "call_edges_internal"],
            "_provenance": provenance, "rows": repo_rows},
        "per_task": per_task,
    }
    dst = results / "swebench_localization.json"
    dst.write_text(json.dumps(out, indent=2, ensure_ascii=False), encoding="utf-8")

    print(f"\n{BOUNDARY}\n")
    print(f"tasks with results: {len(per_task)}")
    for r in loc_rows:
        print(f"  hops={r[0]}  file_recall_mean={r[1]}  hit_rate={r[2]}%  "
              f"neighbourhood files mean={r[3]} median={r[4]}  n={r[5]}")
    print(f"repositories read: {len(repo_rows)}")
    for r in repo_rows:
        print(f"  {r[0]}: files={r[1]} parse_errors={r[2]} internal_recall={r[3]}% edges={r[4]}")
    print(f"wrote {dst}")

    if args.measured:
        mp = Path(args.measured)
        measured = json.loads(mp.read_text(encoding="utf-8")) if mp.exists() else {}
        gs = measured.setdefault("gen_system", {})
        for key in ("swebench_verified_localization", "swebench_verified_repos"):
            if out[key]["rows"]:
                gs[key] = out[key]
        mp.write_text(json.dumps(measured, indent=2, ensure_ascii=False), encoding="utf-8")
        print(f"merged into {mp}")


if __name__ == "__main__":
    main()
