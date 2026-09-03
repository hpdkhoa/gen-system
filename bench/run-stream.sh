#!/usr/bin/env bash
# bench/run-stream.sh — WP2: time-to-first-token and total latency, stream on
# vs off, same prompt, n runs each (genbench stream suite, v26.24). Fills the
# streaming table.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

MODEL="${MODEL:-qwen2.5-coder:14b}"
RUNS="${RUNS:-3}"

build_genbench
require_env_json

unload_all_models
# warm once so TTFT measures generation, not model load
"${GENBENCH}" model --model "${MODEL}" --out "${RAW}/stream-doctor.json" || true

"${GENBENCH}" stream --model "${MODEL}" --runs "${RUNS}" \
    --out "${RAW}/stream.json"

echo
echo "Streaming raw data in ${RAW}. Next: python3 bench/summarise.py --results ${RESULTS}"
