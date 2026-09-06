#!/usr/bin/env bash
# bench/run-partial.sh — v26.35: run only the drivers you name, and fill the
# other tables from earlier results directories without re-dating them.
#
#   bench/run-partial.sh --run strategies,swebench:60 --reuse results/2026-09-05 --reuse results/2026-09-04
#
# --run     comma separated drivers, any of:
#             understand  stream  offload  quant  strategies  swebench[:N]
#           swebench:N runs N tasks (default 60; 0 = all 500)
# --reuse   an earlier results/<date> directory, repeatable, tried in the
#           order given. A table this run did not produce is taken from the
#           first reuse directory that has raw data for it, and keeps THAT
#           directory's date and commit in its provenance plus a reused_from
#           field. Nothing is re-dated. Default when omitted: every other
#           results/* directory, newest first.
#
# Everything else matches run-all.sh: env.json must exist, the tree must be
# clean, raw files land in results/<date>/raw/, and the run ends with
# summarise.py, the SWE-bench summariser, and attest.py. The attestation lists
# which tables were measured here and which were reused from where.
#
# MEASURED=/path/to/my-portfolio/benchmarks/results/measured.json merges rows.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${HERE}/lib.sh"

RUN=""; REUSE=()
while [ $# -gt 0 ]; do
  case "$1" in
    --run)   RUN="$2"; shift 2 ;;
    --reuse) REUSE+=("$2"); shift 2 ;;
    -h|--help) sed -n '2,24p' "${BASH_SOURCE[0]}"; exit 0 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done
if [ -z "${RUN}" ]; then
  echo "usage: bench/run-partial.sh --run understand,stream,offload,quant,strategies,swebench[:N] [--reuse results/<date>]..." >&2
  exit 2
fi

if [ ! -f "${HERE}/env.json" ]; then
  echo "bench/env.json missing — run freeze-env.sh and commit it first." >&2
  exit 2
fi
if [ -n "$(cd "${REPO_ROOT}" && git status --porcelain 2>/dev/null)" ]; then
  echo "FATAL: working tree dirty. Commit first; results must reference a commit." >&2
  exit 2
fi

# Default reuse set: every other results directory, newest first.
if [ ${#REUSE[@]} -eq 0 ]; then
  for d in $(ls -1d "${REPO_ROOT}"/results/*/ 2>/dev/null | sort -r); do
    d="${d%/}"
    [ "${d}" = "${RESULTS}" ] && continue
    [ -d "${d}/raw" ] || continue
    REUSE+=("${d}")
  done
fi
REUSE_ARGS=()
for d in "${REUSE[@]}"; do REUSE_ARGS+=(--reuse "${d}"); done

SWEBENCH_N=""
IFS=',' read -r -a DRIVERS <<< "${RUN}"
for drv in "${DRIVERS[@]}"; do
  case "${drv}" in
    understand) "${HERE}/run-understand.sh" ;;
    stream)     "${HERE}/run-stream.sh" ;;
    offload)    "${HERE}/run-offload.sh" ;;
    quant)      "${HERE}/run-quant-sweep.sh" ;;
    strategies) "${HERE}/run-strategies.sh" ;;
    swebench)   SWEBENCH_N=60; "${HERE}/swebench/run_swebench_localize.sh" --limit 60 ;;
    swebench:*) SWEBENCH_N="${drv#swebench:}"; "${HERE}/swebench/run_swebench_localize.sh" --limit "${SWEBENCH_N}" ;;
    *) echo "unknown driver: ${drv}" >&2; exit 2 ;;
  esac
done

python3 "${HERE}/summarise.py" --results "${RESULTS}" ${MEASURED:+--measured "${MEASURED}"} "${REUSE_ARGS[@]+"${REUSE_ARGS[@]}"}"
python3 "${HERE}/swebench/summarise_swebench.py" --results "${RESULTS}" ${MEASURED:+--measured "${MEASURED}"} "${REUSE_ARGS[@]+"${REUSE_ARGS[@]}"}"
python3 "${HERE}/attest.py" --results "${RESULTS}"

echo
echo "Partial campaign complete. Ran: ${RUN}. Reuse order: ${REUSE[*]:-none}"
echo "Attestation: ${RESULTS}/ATTESTATION.md (measured here versus reused, per table)"
echo "Commit the results directory, then run the portfolio's tools/fill_portfolio.py."
