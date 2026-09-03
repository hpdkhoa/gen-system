#!/usr/bin/env bash
# bench/run-understand.sh — the Understand benchmark on public repos (replaces
# the retired Aider head-to-head, decision 3.6). For each pinned target in
# bench/tasks.json: clone at the pinned commit, then run the deterministic
# parser-path suites (parse, resolution, cfg). No model, no GPU.
#
# CobolCraft additionally verifies against the frozen committed baseline via
# `genbench regression --baseline --strict`.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

build_genbench
require_env_json

CORPUS_DIR="${RESULTS}/corpus"
mkdir -p "${CORPUS_DIR}"

TARGETS_TSV="${RESULTS}/understand-targets.tsv"
python3 - "${BENCH_DIR}/tasks.json" > "${TARGETS_TSV}" <<'PY'
import json, sys
for t in json.load(open(sys.argv[1]))["understand_targets"]:
    print(f'{t["name"]}\t{t["url"]}\t{t["commit"]}\t{t["language"]}')
PY

while IFS=$'\t' read -r name url commit lang; do
  echo "════ understand: ${name} (${lang}) @ ${commit} ════"
  dest="${CORPUS_DIR}/${name}"
  if [ ! -d "${dest}" ]; then
    git clone --quiet "${url}" "${dest}"
  fi
  ( cd "${dest}" && git checkout --quiet "${commit}" )
  got="$(cd "${dest}" && git rev-parse HEAD)"
  if [ "${got}" != "${commit}" ]; then
    echo "FATAL: ${name} commit mismatch (${got} != ${commit})" >&2
    exit 2
  fi

  CB_ARGS=()
  if [ "${lang}" = "cobol" ]; then
    CB="$(find "${dest}" -type d -name '_copybooks' | sort -u | paste -sd: -)"
    [ -n "${CB}" ] && CB_ARGS=(--copybooks "${CB}")
  fi

  "${GENBENCH}" parse      --root "${dest}" "${CB_ARGS[@]+"${CB_ARGS[@]}"}" --out "${RAW}/understand-${name}-parse.json"      || true
  "${GENBENCH}" resolution --root "${dest}" "${CB_ARGS[@]+"${CB_ARGS[@]}"}" --out "${RAW}/understand-${name}-resolution.json" || true
  "${GENBENCH}" cfg        --root "${dest}" "${CB_ARGS[@]+"${CB_ARGS[@]}"}" --out "${RAW}/understand-${name}-cfg.json"        || true
done < "${TARGETS_TSV}"

# CobolCraft: also verify against the frozen baseline (drift gate).
echo "════ understand: CobolCraft baseline verify ════"
"${REPO_ROOT}/generator/internal/bench/baselines/freeze-cobol-baseline.sh" verify \
  && echo "baseline verify: OK" | tee "${RAW}/understand-cobolcraft-baseline-verify.txt" \
  || echo "baseline verify: DRIFT (see above)" | tee "${RAW}/understand-cobolcraft-baseline-verify.txt"

echo
echo "Understand raw data in ${RAW}. Next: python3 bench/summarise.py --results ${RESULTS}"
