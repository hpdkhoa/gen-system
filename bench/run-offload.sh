#!/usr/bin/env bash
# bench/run-offload.sh — WP2: tokens/sec and peak VRAM vs GPU offload layers.
#
# Loops OLLAMA_NUM_GPU over STEPS (default: 20 30 40) plus one run with the
# knob unset (Ollama's own default = all layers that fit), running
# `genbench gen --runs 3` per step on the coder model. The knob reaches Ollama
# through the same request options production sends (v26.24). Peak VRAM is
# sampled to a .vram sidecar per step. Fills the offload_curve table.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

MODEL="${MODEL:-qwen2.5-coder:14b}"
STEPS="${STEPS:-20 30 40}"
RUNS="${RUNS:-3}"

build_genbench
require_env_json

for layers in ${STEPS} default; do
  echo "════ offload num_gpu=${layers} (${MODEL}) ════"
  unload_all_models
  if [ "${layers}" = "default" ]; then
    unset OLLAMA_NUM_GPU || true
  else
    export OLLAMA_NUM_GPU="${layers}"
  fi
  vram_start "${RAW}/offload-${layers}.vram"
  "${GENBENCH}" gen --model "${MODEL}" --runs "${RUNS}" \
      --out "${RAW}/offload-${layers}.json" || true
  vram_stop
done
unset OLLAMA_NUM_GPU || true

echo
echo "Offload raw data in ${RAW}. Next: python3 bench/summarise.py --results ${RESULTS}"
