#!/usr/bin/env python3
"""
genbench_to_measured.py — map a `genbench` Run JSON into the portfolio's measured.json.

Your harness already exists (genbench: 11+ suites, frozen baselines, regression gate).
This bridge extracts the writeup-02 numbers from its output. Place anywhere; run per model:

    cd ~/code/gen-system
    go build -o /tmp/genbench ./generator/cmd/genbench

    # per quantization / model tag (see quant_sweep.sh for the loop + VRAM sampling):
    /tmp/genbench all --root . --model deepseek-coder-v2:16b-lite-instruct-q5_K_M \
        --json /tmp/run-q5.json
    python3 genbench_to_measured.py /tmp/run-q5.json \
        --label Q5_K_M --vram-gb 10.2 \
        --measured /path/to/portfolio/benchmarks/results/measured.json

Mapping (suite -> table):
  generation_latency . tokens_per_sec_mean  -> quantization_sweep.tokens_per_sec
  --vram-gb (from nvidia-smi sampling)      -> quantization_sweep.vram_gb
  pass rate across generation-gate suites   -> quantization_sweep.compile_pass_rate_pct
      (any suite whose name matches --gate-re; default: spring_generation,
       postgres_generation, webgate — the "does the generated project build" gates)
  ollama_doctor first/second_load_ms        -> two_model_strategies reload row (optional,
                                               with --strategy "sequential + keep-alive")
"""

import argparse, json, re
from pathlib import Path


def metric(suite, name):
    for m in suite.get("metrics", []):
        if m.get("name") == name:
            return m.get("value")
    return None


def append_row(measured, table, row, columns):
    sec = measured.setdefault("gen_system", {})
    tab = sec.setdefault(table, {"_columns": columns, "rows": []})
    tab.setdefault("rows", []).append(row)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("run_json", help="output of `genbench ... --json`")
    ap.add_argument("--measured", required=True)
    ap.add_argument("--label", required=True, help="quant/model label, e.g. Q5_K_M")
    ap.add_argument("--vram-gb", type=float, help="peak VRAM observed for this run (nvidia-smi)")
    ap.add_argument("--gate-re", default=r"(spring|postgres|web).*(gen|gate)|webgate",
                    help="regex matching the generation-gate suites for pass-rate")
    ap.add_argument("--strategy", help="also record an ollama_doctor reload row under this "
                                       "strategy label in two_model_strategies")
    args = ap.parse_args()

    run = json.loads(Path(args.run_json).read_text(encoding="utf-8"))
    suites = {s["name"]: s for s in run.get("suites", [])}
    mp = Path(args.measured)
    measured = json.loads(mp.read_text(encoding="utf-8")) if mp.exists() else {}

    # tokens/sec from the generation_latency suite
    tok = None
    for name in ("generation_latency", "gen"):
        if name in suites:
            tok = metric(suites[name], "tokens_per_sec_mean")
            break

    # compile-pass-rate across generation gates
    gate_re = re.compile(args.gate_re, re.I)
    passed = total = 0
    gate_names = []
    for name, s in suites.items():
        if gate_re.search(name):
            gate_names.append(name)
            for c in s.get("cases", []):
                total += 1
                passed += bool(c.get("pass") or c.get("Pass"))
    pass_pct = round(100.0 * passed / total, 1) if total else None

    if tok is None and pass_pct is None:
        raise SystemExit("Run JSON contained neither generation_latency metrics nor "
                         "gate suites — nothing to record. Did you run `genbench all`?")

    append_row(measured, "quantization_sweep",
               [args.label,
                round(tok, 1) if tok is not None else None,
                args.vram_gb,
                pass_pct],
               ["quant", "tokens_per_sec", "vram_gb", "compile_pass_rate_pct"])
    print(f"quantization_sweep[{args.label}]: tok/s={tok} vram={args.vram_gb} "
          f"pass={pass_pct}% (gates: {', '.join(gate_names) or 'none'})")

    if args.strategy and "ollama_doctor" in suites:
        d = suites["ollama_doctor"]
        first, second = metric(d, "first_load_ms"), metric(d, "second_load_ms")
        if first is not None:
            append_row(measured, "two_model_strategies",
                       [args.strategy,
                        round((first - (second or 0)) / 1000.0, 2),
                        round(tok, 1) if tok is not None else None,
                        pass_pct],
                       ["strategy", "reload_cost_s", "tokens_per_sec", "compile_pass_rate_pct"])
            print(f"two_model_strategies[{args.strategy}]: reload≈{(first-(second or 0))/1000:.2f}s")

    mp.write_text(json.dumps(measured, indent=2, ensure_ascii=False), encoding="utf-8")
    print(f"wrote {mp}")


if __name__ == "__main__":
    main()
