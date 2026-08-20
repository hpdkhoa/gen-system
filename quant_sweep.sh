#!/usr/bin/env bash
# quant_sweep.sh — run genbench across quantization levels, sampling peak VRAM,
# and record each as a quantization_sweep row in the portfolio's measured.json.
#
# Usage (from the gen-system repo root):
#   MEASURED=/path/to/portfolio/benchmarks/results/measured.json ./quant_sweep.sh
#
# Edit MODELS to the tags you have pulled in ollama (`ollama list`).
set -euo pipefail

MEASURED="${MEASURED:?set MEASURED=/path/to/measured.json}"
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="${ROOT:-.}"

declare -A MODELS=(
  # label            ollama tag
  [Q4_K_M]="deepseek-coder-v2:16b-lite-instruct-q4_K_M"
  [Q5_K_M]="deepseek-coder-v2:16b-lite-instruct-q5_K_M"
  [Q8_0]="deepseek-coder-v2:16b-lite-instruct-q8_0"
)

go build -o /tmp/genbench ./generator/cmd/genbench

sample_vram() {  # background sampler -> writes peak MiB to $1
  local out="$1"; : > "$out"
  while true; do
    nvidia-smi --query-gpu=memory.used --format=csv,noheader,nounits >> "$out" 2>/dev/null || true
    sleep 0.5
  done
}

for label in "${!MODELS[@]}"; do
  tag="${MODELS[$label]}"
  echo "=== $label ($tag) ==="
  for m in $(ollama ps --format '{{.Name}}' 2>/dev/null || ollama ps | awk 'NR>1{print $1}'); do
    ollama stop "$m" || true
  done
  sleep 3
  vram_log="/tmp/vram-$label.log"
  sample_vram "$vram_log" & sampler=$!
  trap 'kill $sampler 2>/dev/null || true' EXIT

  /tmp/genbench all --root "$ROOT" --model "$tag" --out "/tmp/run-$label.json" || true

  kill "$sampler" 2>/dev/null || true; trap - EXIT
  peak_mib=$(sort -n "$vram_log" | tail -1); peak_gb=$(awk "BEGIN{printf \"%.1f\", ${peak_mib:-0}/1024}")

  if [[ -f "/tmp/run-$label.json" ]]; then
    python3 "$HERE/genbench_to_measured.py" "/tmp/run-$label.json" \
        --label "$label" --vram-gb "$peak_gb" --measured "$MEASURED" \
        ${STRATEGY_LABEL:+--strategy "$STRATEGY_LABEL"}
  else
    echo "!! genbench wrote no output for $label — check the run above, skipping"
  fi
done

echo "Sweep complete. Now run the portfolio's tools/fill_portfolio.py to inject."
