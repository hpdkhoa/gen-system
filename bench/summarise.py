#!/usr/bin/env python3
"""
bench/summarise.py — WP4: aggregate raw genbench JSON into measured.json rows
WITH provenance. Tables are generated, never typed.

Reads results/<date>/raw/*.json (+ .vram sidecars, MiB peaks) written by the
bench/run-*.sh drivers, aggregates the n runs per config (mean / min / max),
and writes rows into the portfolio's measured.json:

  gen_system.quantization_sweep   [quant, tokens_per_sec, vram_gb, ops_total, stub_rate_pct,
                                   op_repair_attempts, heal_attempts, go_test_pass,
                                   n, tok_s_min, tok_s_max]
  gen_system.offload_curve        [gpu_layers, tokens_per_sec, vram_gb, n, tok_s_min, tok_s_max]
  gen_system.streaming            [config, ttft_ms, total_latency_s, n, ttft_min, ttft_max]
  gen_system.two_model_strategies [strategy, reload_cost_s, tokens_per_sec, ops_total, stub_rate_pct,
                                   heal_attempts, go_test_pass, n]
  gen_system.understand_public_repos [repo, files, parse_errors, internal_recall_pct,
                                      call_edges_internal, cfgs_total, cfgs_with_issue]

Every table gets a "_provenance" object: {date, gen_system_commit, task_set,
env}. Every value comes from the raw files; a config with no raw data writes
no row. Never invents numbers.

Usage:
  python3 bench/summarise.py --results results/2026-08-30 \
      --measured /path/to/my-portfolio/benchmarks/results/measured.json
  (omit --measured for a dry run: prints the rows, writes measured.rows.json
   next to raw/)
"""

import argparse, json, re, statistics
from pathlib import Path


def load_runs(raw_dir, pattern):
    """Yield (path, parsed Run) for raw files matching the regex."""
    rx = re.compile(pattern)
    for p in sorted(raw_dir.glob("*.json")):
        if rx.match(p.name):
            try:
                yield p, json.loads(p.read_text(encoding="utf-8"))
            except json.JSONDecodeError:
                print(f"  !! {p.name}: not valid JSON, skipped")


def suite(run, name):
    for s in run.get("suites", []):
        if s.get("name") == name:
            return s
    return None


def metric(s, name):
    if not s:
        return None
    for m in s.get("metrics", []):
        if m.get("name") == name:
            return m.get("value")
    return None


def vram_gb(path):
    """Peak VRAM from the .vram sidecar next to a raw JSON, in GB (1 decimal)."""
    side = path.with_suffix(".vram")
    if not side.exists():
        return None
    try:
        mib = float(side.read_text().strip() or 0)
        return round(mib / 1024, 1) if mib > 0 else None
    except ValueError:
        return None


def agg(values):
    values = [v for v in values if v is not None]
    if not values:
        return None, None, None
    return (round(statistics.fmean(values), 1),
            round(min(values), 1), round(max(values), 1))


def r1(v):
    return None if v is None else round(v, 1)


def pipeline_measured(gp):
    """v26.32: a gen_pipeline suite that never completed a model request did
    not measure generation quality, whatever build and test say (the fallback
    schema builds green). Such a run is dropped from the quality columns and
    counted in the invalid column instead of averaging in as 0 percent stubs.
    Rule: ops_total == 0 AND model_requests == 0 (works for raw files written
    before ideas_no_model_contact existed). Raw files carrying the newer
    ideas_no_model_contact metric are dropped when any idea had no contact."""
    if gp is None:
        return False
    if (metric(gp, "ideas_no_model_contact") or 0) > 0:
        return False
    if (metric(gp, "ops_total") or 0) == 0 and (metric(gp, "model_requests") or 0) == 0:
        return False
    return True


# ───────────────────────────── table builders ────────────────────────────────

def quant_rows(raw):
    labels = sorted({m.group(1) for p in raw.glob("quant-*-run*.json")
                     for m in [re.match(r"quant-(.+)-(?:gen|genpipe)-run\d+\.json", p.name)] if m})
    rows = []
    for label in labels:
        toks, vrams = [], []
        for p, run in load_runs(raw, rf"quant-{re.escape(label)}-gen-run\d+\.json"):
            toks.append(metric(suite(run, "generation_latency"), "tokens_per_sec_mean"))
            v = vram_gb(p)
            if v:
                vrams.append(v)
        stubs, repairs, heals, gtp, ops, n_pipe, invalid = [], [], [], [], [], 0, 0
        for p, run in load_runs(raw, rf"quant-{re.escape(label)}-genpipe-run\d+\.json"):
            gp = suite(run, "gen_pipeline")
            if not gp:
                continue
            if not pipeline_measured(gp):
                invalid += 1
                v = vram_gb(p)
                if v:
                    vrams.append(v)
                continue
            n_pipe += 1
            ops.append(metric(gp, "ops_total"))
            stubs.append(metric(gp, "stub_rate_pct"))
            repairs.append(metric(gp, "op_repair_attempts"))
            heals.append(metric(gp, "heal_attempts_total"))
            gtp.append(metric(gp, "go_test_pass"))
            v = vram_gb(p)
            if v:
                vrams.append(v)
        if not toks and not n_pipe:
            continue
        tok_mean, tok_min, tok_max = agg(toks)
        rows.append([label, tok_mean, max(vrams) if vrams else None,
                     agg(ops)[0], agg(stubs)[0], agg(repairs)[0], agg(heals)[0], agg(gtp)[0],
                     max(len(toks), n_pipe), tok_min, tok_max, invalid])
    return rows


def gen_completed_runs(gl):
    """Completed-run count from the generation_latency suite's own case line
    ('x/y' in the at_least_one case). None if absent — never guessed."""
    for c in (gl or {}).get("cases", []):
        if c.get("name", "").startswith("at_least_one_of_"):
            m = re.match(r"(\d+)/(\d+)", c.get("got", ""))
            if m:
                return int(m.group(1))
    return None


def offload_rows(raw):
    rows = []
    for p, run in load_runs(raw, r"offload-.+\.json"):
        m = re.match(r"offload-(.+)\.json", p.name)
        gl = suite(run, "generation_latency")
        tok = metric(gl, "tokens_per_sec_mean")
        if tok is None:
            continue
        # gen reports a mean over its runs; per-call min/max are not in the raw
        # data, so those columns stay null rather than invented.
        rows.append([m.group(1), r1(tok), vram_gb(p),
                     gen_completed_runs(gl), None, None])
    return rows


def streaming_rows(raw):
    rows = []
    for p, run in load_runs(raw, r"stream\.json"):
        s = suite(run, "streaming_latency")
        if not s:
            continue
        for key, label in [("stream_off", "stream off (buffered)"),
                           ("stream_on", "stream on (NDJSON)")]:
            n = metric(s, f"{key}_runs")
            if not n:
                continue
            rows.append([label,
                         r1(metric(s, f"{key}_ttft_ms_mean")),
                         None if metric(s, f"{key}_total_ms_mean") is None
                         else round(metric(s, f"{key}_total_ms_mean") / 1000, 2),
                         int(n),
                         r1(metric(s, f"{key}_ttft_ms_min")),
                         r1(metric(s, f"{key}_ttft_ms_max"))])
    return rows


def strategy_rows(raw):
    labels = sorted({m.group(1) for p in raw.glob("strategy-*-genpipe-run*.json")
                     for m in [re.match(r"strategy-(.+)-genpipe-run\d+\.json", p.name)] if m})
    rows = []
    for label in labels:
        stubs, heals, gtp, toks, ops = [], [], [], [], []
        n = 0
        invalid = 0
        for p, run in load_runs(raw, rf"strategy-{re.escape(label)}-genpipe-run\d+\.json"):
            gp = suite(run, "gen_pipeline")
            if not gp:
                continue
            if not pipeline_measured(gp):
                invalid += 1
                continue
            n += 1
            ops.append(metric(gp, "ops_total"))
            stubs.append(metric(gp, "stub_rate_pct"))
            heals.append(metric(gp, "heal_attempts_total"))
            gtp.append(metric(gp, "go_test_pass"))
        for p, run in load_runs(raw, rf"strategy-{re.escape(label)}-gen-code-run\d+\.json"):
            toks.append(metric(suite(run, "generation_latency"), "tokens_per_sec_mean"))
        reload_s = None
        for p, run in load_runs(raw, rf"strategy-{re.escape(label)}-doctor-code\.json"):
            d = suite(run, "ollama_doctor")
            first, second = metric(d, "first_load_ms"), metric(d, "second_load_ms")
            if first is not None:
                reload_s = round((first - (second or 0)) / 1000, 2)
        if n == 0 and not toks and invalid == 0:
            continue
        rows.append([label, reload_s, agg(toks)[0], agg(ops)[0], agg(stubs)[0],
                     agg(heals)[0], agg(gtp)[0], n, invalid])
    return rows


def understand_rows(raw):
    names = sorted({m.group(1) for p in raw.glob("understand-*-parse.json")
                    for m in [re.match(r"understand-(.+)-parse\.json", p.name)] if m})
    rows = []
    for name in names:
        files = errors = recall = edges = cfgs = issues = None
        for p, run in load_runs(raw, rf"understand-{re.escape(name)}-parse\.json"):
            s = suite(run, "parse_throughput")
            if s:
                per_lang = [m.get("value") for m in s.get("metrics", [])
                            if m.get("name", "").startswith("files_total_")]
                files = int(sum(per_lang)) if per_lang else None
                errors = metric(s, "parse_errors")
        for p, run in load_runs(raw, rf"understand-{re.escape(name)}-resolution\.json"):
            s = suite(run, "resolution_recall")
            rec = metric(s, "resolution_recall_internal")
            recall = round(rec * 100, 1) if rec is not None else None
            edges = metric(s, "call_edges_internal")
        for p, run in load_runs(raw, rf"understand-{re.escape(name)}-cfg\.json"):
            s = suite(run, "cfg_correctness")
            cfgs = metric(s, "cfgs_total")
            issues = metric(s, "cfgs_with_issue")
        rows.append([name, files, errors, recall, edges, cfgs, issues])
    return rows


# ───────────────────────────────── main ──────────────────────────────────────

TABLES = [
    ("quantization_sweep",
     ["quant", "tokens_per_sec", "vram_gb", "ops_total", "stub_rate_pct", "op_repair_attempts",
      "heal_attempts", "go_test_pass", "n", "tok_s_min", "tok_s_max", "runs_not_measured"],
     quant_rows),
    ("offload_curve",
     ["gpu_layers", "tokens_per_sec", "vram_gb", "n", "tok_s_min", "tok_s_max"],
     offload_rows),
    ("streaming",
     ["config", "ttft_ms", "total_latency_s", "n", "ttft_min", "ttft_max"],
     streaming_rows),
    ("two_model_strategies",
     ["strategy", "reload_cost_s", "tokens_per_sec", "ops_total", "stub_rate_pct",
      "heal_attempts", "go_test_pass", "n", "runs_not_measured"],
     strategy_rows),
    ("understand_public_repos",
     ["repo", "files", "parse_errors", "internal_recall_pct",
      "call_edges_internal", "cfgs_total", "cfgs_with_issue"],
     understand_rows),
]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--results", required=True, help="results/<date> directory")
    ap.add_argument("--measured", help="portfolio measured.json to merge into (omit = dry run)")
    args = ap.parse_args()

    results = Path(args.results)
    raw = results / "raw"
    if not raw.is_dir():
        raise SystemExit(f"no raw/ under {results}")

    env = {}
    env_path = results / "env.json"
    if env_path.exists():
        env = json.loads(env_path.read_text(encoding="utf-8"))
    else:
        print("WARNING: no env.json in results dir — provenance will be incomplete")

    provenance = {
        "date": results.name,
        "gen_system_commit": env.get("gen_system_commit", "unknown"),
        "task_set": "bench/tasks.json (frozen 2026-08-27)",
        "gpu": env.get("gpu", "unknown"),
    }

    out = {}
    for name, columns, builder in TABLES:
        rows = builder(raw)
        if not rows:
            print(f"  {name}: no raw data, no rows written")
            continue
        out[name] = {"_columns": columns, "_provenance": provenance, "rows": rows}
        print(f"  {name}: {len(rows)} rows")
        for r in rows:
            print("    ", r)

    snapshot = results / "measured.rows.json"
    snapshot.write_text(json.dumps(out, indent=2, ensure_ascii=False), encoding="utf-8")
    print(f"wrote {snapshot}")

    if args.measured:
        mp = Path(args.measured)
        measured = json.loads(mp.read_text(encoding="utf-8")) if mp.exists() else {}
        gs = measured.setdefault("gen_system", {})
        for name, table in out.items():
            gs[name] = table  # replace: rows are regenerated per study, not appended

        # Strip the scaffolding the template shipped with. Once real rows exist,
        # a "fill this in" note and the _*_example placeholder rows read as an
        # unfinished file. Removing them here means it cannot be forgotten.
        for section in list(measured.values()) + [measured]:
            if not isinstance(section, dict):
                continue
            for key in [k for k in section
                        if k == "_readme" or (k.startswith("_") and k.endswith("_example"))]:
                del section[key]
                print(f"  removed template key: {key}")

        mp.write_text(json.dumps(measured, indent=2, ensure_ascii=False), encoding="utf-8")
        print(f"merged into {mp}")


if __name__ == "__main__":
    main()
