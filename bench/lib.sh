#!/usr/bin/env bash
# bench/lib.sh: shared helpers for the WP1 to WP3 benchmark drivers.
# Source this; do not run it.

BENCH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${BENCH_DIR}/.." && pwd)"
RUN_DATE="${RUN_DATE:-$(date +%F)}"
RESULTS="${REPO_ROOT}/results/${RUN_DATE}"
RAW="${RESULTS}/raw"
mkdir -p "${RAW}"

GENBENCH="${GENBENCH:-${RESULTS}/genbench}"

build_genbench() {
  if [ ! -x "${GENBENCH}" ]; then
    echo "building genbench → ${GENBENCH}"
    ( cd "${REPO_ROOT}" && go build -o "${GENBENCH}" ./generator/cmd/genbench )
  fi
}

require_env_json() {
  if [ ! -f "${BENCH_DIR}/env.json" ]; then
    echo "FATAL: bench/env.json missing. Run bench/freeze-env.sh first (and commit it" >&2
    echo "together with bench/tasks.json BEFORE any measured run)." >&2
    exit 2
  fi
  cp -f "${BENCH_DIR}/env.json" "${RESULTS}/env.json"
}

# ── VRAM sampling ────────────────────────────────────────────────────────────
# vram_start <sidecar-path>: begin sampling nvidia-smi memory.used every 0.5s.
# vram_stop: stop sampling and write the peak (MiB, one number) to the sidecar.
_VRAM_LOG=""
_VRAM_PID=""
_VRAM_OUT=""

vram_start() {
  _VRAM_OUT="$1"
  _VRAM_LOG="$(mktemp)"
  (
    while true; do
      nvidia-smi --query-gpu=memory.used --format=csv,noheader,nounits >> "${_VRAM_LOG}" 2>/dev/null || true
      sleep 0.5
    done
  ) &
  _VRAM_PID=$!
}

vram_stop() {
  if [ -n "${_VRAM_PID}" ]; then
    kill "${_VRAM_PID}" 2>/dev/null || true
    wait "${_VRAM_PID}" 2>/dev/null || true
  fi
  local peak
  peak="$(sort -n "${_VRAM_LOG}" 2>/dev/null | tail -1)"
  echo "${peak:-0}" > "${_VRAM_OUT}"
  rm -f "${_VRAM_LOG}"
  _VRAM_PID=""; _VRAM_LOG=""; _VRAM_OUT=""
}

# unload_all_models: ask ollama to stop every resident model, then settle.
unload_all_models() {
  for m in $(ollama ps --format '{{.Name}}' 2>/dev/null || ollama ps 2>/dev/null | awk 'NR>1{print $1}'); do
    ollama stop "$m" 2>/dev/null || true
  done
  sleep 3
}
