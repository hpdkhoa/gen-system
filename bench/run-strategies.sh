#!/usr/bin/env bash
# bench/run-strategies.sh — WP3: measure all three two-model serving strategies
# on the SAME frozen task set (bench/tasks.json), n runs each.
#
#   A  sequential + keep-alive  — production pair (qwen3:14b plan,
#      qwen2.5-coder:14b code), models swap in and out; keep-alive absorbs the
#      reload. Swap cost = doctor first/second load delta.
#   B  both resident            — OLLAMA_KEEP_ALIVE=-1, both models warmed and
#      kept on the GPU. Measures whether they fit (peak VRAM + `ollama ps`),
#      swap cost ~0, and tok/s under contention (alternating calls).
#   C  one shared model         — qwen2.5-coder:14b for BOTH roles. No swap at
#      all; genpipe shows what planning quality costs (stubs, heals, entity
#      over-generation show up in ops/entities metrics).
#
# Raw JSON per strategy in results/<date>/raw/. Aggregate with summarise.py.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

N="${N:-3}"
PLAN_MODEL="${PLAN_MODEL:-qwen3:14b}"
CODE_MODEL="${CODE_MODEL:-qwen2.5-coder:14b}"

build_genbench
require_env_json
export GEN_LOGIC=ollama

run_strategy() {
  local label="$1"
  local keep_resident="${2:-}"
  echo "════ strategy ${label} (plan=${OLLAMA_MODEL_PLAN} code=${OLLAMA_MODEL_CODE} keep_alive=${OLLAMA_KEEP_ALIVE}) ════"
  # Strategy B pre-warms both models; unloading would destroy the setup.
  if [ "${keep_resident}" != "keep" ]; then
    unload_all_models
  fi

  # swap-cost probes: load times for each role's model
  "${GENBENCH}" model --model "${OLLAMA_MODEL_CODE}" --out "${RAW}/strategy-${label}-doctor-code.json" || true
  "${GENBENCH}" model --model "${OLLAMA_MODEL_PLAN}" --out "${RAW}/strategy-${label}-doctor-plan.json" || true

  for i in $(seq 1 "${N}"); do
    echo "──── ${label} run ${i}/${N}: genpipe ────"
    vram_start "${RAW}/strategy-${label}-genpipe-run${i}.vram"
    "${GENBENCH}" genpipe --ideas "${BENCH_DIR}/tasks.json" \
        --model "${OLLAMA_MODEL_CODE}" \
        --out "${RAW}/strategy-${label}-genpipe-run${i}.json" || true
    vram_stop

    echo "──── ${label} run ${i}/${N}: gen tok/s per role (contention order: code, plan) ────"
    vram_start "${RAW}/strategy-${label}-gen-run${i}.vram"
    "${GENBENCH}" gen --model "${OLLAMA_MODEL_CODE}" --runs 3 \
        --out "${RAW}/strategy-${label}-gen-code-run${i}.json" || true
    "${GENBENCH}" gen --model "${OLLAMA_MODEL_PLAN}" --runs 3 \
        --out "${RAW}/strategy-${label}-gen-plan-run${i}.json" || true
    vram_stop
  done

  ollama ps > "${RAW}/strategy-${label}-ollama-ps.txt" 2>&1 || true
}

# ── A: sequential + keep-alive (production default) ─────────────────────────
export OLLAMA_MODEL_PLAN="${PLAN_MODEL}"
export OLLAMA_MODEL_CODE="${CODE_MODEL}"
export OLLAMA_KEEP_ALIVE="30m"
run_strategy A-sequential-keepalive

# ── B: both models resident ─────────────────────────────────────────────────
export OLLAMA_KEEP_ALIVE="-1"
unload_all_models
echo "warming both models resident…"
ollama run "${PLAN_MODEL}" "say only: ok" --keepalive -1 >/dev/null 2>&1 || true
ollama run "${CODE_MODEL}" "say only: ok" --keepalive -1 >/dev/null 2>&1 || true
ollama ps || true
run_strategy B-both-resident keep

# ── C: one shared model for both roles ──────────────────────────────────────
export OLLAMA_MODEL_PLAN="${CODE_MODEL}"
export OLLAMA_MODEL_CODE="${CODE_MODEL}"
export OLLAMA_KEEP_ALIVE="30m"
run_strategy C-single-shared

echo
echo "Strategy raw data in ${RAW}. Next: python3 bench/summarise.py --results ${RESULTS}"
