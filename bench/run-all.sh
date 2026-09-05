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
#   6. swebench    (localization recall, SWEBENCH_LIMIT tasks; v26.33)
#   7. summarise   (WP4 — rows with provenance) + attest
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

# v26.33: SWE-bench Verified localization recall (NOT a solve rate; see
# bench/swebench/README.md). SWEBENCH_LIMIT tasks, default 60; 0 = all 500
# (hours). SWEBENCH_LIMIT=skip leaves it out.
SWEBENCH_LIMIT="${SWEBENCH_LIMIT:-60}"
if [ "${SWEBENCH_LIMIT}" != "skip" ]; then
  "${HERE}/swebench/run_swebench_localize.sh" --limit "${SWEBENCH_LIMIT}"
fi

python3 "${HERE}/summarise.py" --results "${RESULTS}" ${MEASURED:+--measured "${MEASURED}"}
if [ "${SWEBENCH_LIMIT}" != "skip" ]; then
  python3 "${HERE}/swebench/summarise_swebench.py" --results "${RESULTS}" ${MEASURED:+--measured "${MEASURED}"}
fi
python3 "${HERE}/attest.py" --results "${RESULTS}"   # v26.32: manifest + attestation

echo
echo "Campaign complete. Raw: ${RAW}"
echo "Attestation: ${RESULTS}/ATTESTATION.md (what ran, what did not, what was dropped)"
echo "Rows: ${RESULTS}/measured.rows.json"
echo "Commit the results directory, then run the portfolio's tools/fill_portfolio.py."
