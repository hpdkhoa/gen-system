#!/usr/bin/env bash
# bench/run-all.sh — the whole measurement campaign, in dependency order.
# Run on the benchmark box (RTX 4060 Ti, Ollama up, quant tags pulled).
#
#   0. freeze-env  (WP0 — must be committed before this script runs)
#   1. understand  (deterministic, no model — fastest feedback)
#   2. stream      (WP2)
#   3. offload     (WP2)
#   4. quant sweep (WP1, n=3 — the long one)
#   5. strategies  (WP3, n=3)
#   6. summarise   (WP4 — rows with provenance)
#
# Set MEASURED=/path/to/my-portfolio/benchmarks/results/measured.json to merge
# rows directly; omit it to review results/<date>/measured.rows.json first.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${HERE}/lib.sh"

if [ ! -f "${HERE}/env.json" ]; then
  echo "bench/env.json missing — running freeze-env.sh now. COMMIT env.json +"
  echo "tasks.json before re-running this script; git timestamps are the proof."
  "${HERE}/freeze-env.sh"
  exit 2
fi

if [ -n "$(cd "${REPO_ROOT}" && git status --porcelain 2>/dev/null)" ]; then
  echo "FATAL: working tree dirty. Commit first; results must reference a commit." >&2
  exit 2
fi

"${HERE}/run-understand.sh"
"${HERE}/run-stream.sh"
"${HERE}/run-offload.sh"
"${HERE}/run-quant-sweep.sh"
"${HERE}/run-strategies.sh"

python3 "${HERE}/summarise.py" --results "${RESULTS}" ${MEASURED:+--measured "${MEASURED}"}

echo
echo "Campaign complete. Raw: ${RAW}"
echo "Rows: ${RESULTS}/measured.rows.json"
echo "Commit the results directory, then run the portfolio's tools/fill_portfolio.py."
