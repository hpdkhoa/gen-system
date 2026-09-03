#!/usr/bin/env bash
# bench/run-quant-sweep.sh — WP1: quantization sweep, n=3 per config, same seed.
#
# For each quant of the study model (deepseek-coder-v2 16B lite at Q4_K_M /
# Q5_K_M / Q8_0) ON THE CODER ROLE, planner fixed at qwen3:14b:
#   - genbench genpipe (5 frozen ideas, live model): ops_stubbed, repairs,
#     heals, go_test_pass, verify findings, prompt tokens  ← the headline
#   - genbench gen (raw tokens/sec)
#   - genbench model (doctor: load times → warm-swap cost)
#   - peak VRAM sampled around each run (sidecar .vram file, MiB)
#
# Also runs the production pair (qwen2.5-coder:14b coder / qwen3:14b planner)
# as its own config so the sweep table and the production row share provenance.
#
# Raw JSON goes to results/<date>/raw/. Aggregate with bench/summarise.py.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

N="${N:-3}"
PLAN_MODEL="${PLAN_MODEL:-qwen3:14b}"

# label:tag pairs (bash 3 compatible, ordered)
CONFIGS=(
  "Q4_K_M:deepseek-coder-v2:16b-lite-instruct-q4_K_M"
  "Q5_K_M:deepseek-coder-v2:16b-lite-instruct-q5_K_M"
  "Q8_0:deepseek-coder-v2:16b-lite-instruct-q8_0"
  "production:qwen2.5-coder:14b"
)

build_genbench
require_env_json

export GEN_LOGIC=ollama
export OLLAMA_MODEL_PLAN="${PLAN_MODEL}"
export OLLAMA_KEEP_ALIVE="${OLLAMA_KEEP_ALIVE:-30m}"

for cfg in "${CONFIGS[@]}"; do
  label="${cfg%%:*}"
  tag="${cfg#*:}"
  echo "════ quant ${label} (coder=${tag}, planner=${PLAN_MODEL}, n=${N}) ════"
  export OLLAMA_MODEL_CODE="${tag}"

  unload_all_models

  # doctor once per config: load times / residency
  "${GENBENCH}" model --model "${tag}" --out "${RAW}/quant-${label}-doctor.json" || true

  for i in $(seq 1 "${N}"); do
    echo "──── ${label} run ${i}/${N}: genpipe ────"
    vram_start "${RAW}/quant-${label}-genpipe-run${i}.vram"
    "${GENBENCH}" genpipe --ideas "${BENCH_DIR}/tasks.json" \
        --model "${tag}" --out "${RAW}/quant-${label}-genpipe-run${i}.json" || true
    vram_stop

    echo "──── ${label} run ${i}/${N}: gen (tok/s) ────"
    vram_start "${RAW}/quant-${label}-gen-run${i}.vram"
    "${GENBENCH}" gen --model "${tag}" --runs 3 \
        --out "${RAW}/quant-${label}-gen-run${i}.json" || true
    vram_stop
  done
done

echo
echo "Sweep raw data in ${RAW}. Next: python3 bench/summarise.py --results ${RESULTS}"
