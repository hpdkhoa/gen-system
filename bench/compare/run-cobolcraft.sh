#!/usr/bin/env bash
# run-cobolcraft.sh: diff gen-system's COBOL call edges against GnuCOBOL's cobc
# and against a plain text scan, on CobolCraft at the baseline's pinned commit.
#
# Usage: bench/compare/run-cobolcraft.sh [OUT_DIR]
#   OUT_DIR defaults to results/compare-cobolcraft/ in the repository.
# Needs: cobc (GnuCOBOL 3.x), python3, git, and go (or GENBENCH=/path/to/genbench).
# COBOLCRAFT_DIR=/path uses an existing checkout at the pinned commit instead of
# cloning one. STRICT=1 exits 1 on any difference against cobc (CI uses it).
#
# Writes: gen-system.jsonl, cobc.jsonl, cobc-rejected.jsonl, textscan.jsonl,
# vs-cobc.md/.json, vs-textscan.md/.json and versions.txt.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${HERE}/../.." && pwd)"
OUT="${1:-${REPO_ROOT}/results/compare-cobolcraft}"
PIN="$(sed -n 's/^PIN_COMMIT="\([0-9a-f]*\)".*/\1/p' "${REPO_ROOT}/generator/internal/bench/baselines/freeze-cobol-baseline.sh")"
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT
mkdir -p "${OUT}"
OUT="$(cd "${OUT}" && pwd)"

CORPUS="${COBOLCRAFT_DIR:-${WORK}/CobolCraft}"
if [ -z "${COBOLCRAFT_DIR:-}" ]; then
  git clone --quiet https://github.com/meyfa/CobolCraft "${CORPUS}"
  git -C "${CORPUS}" checkout --quiet "${PIN}"
fi
CORPUS="$(cd "${CORPUS}" && pwd)"
if [ "$(git -C "${CORPUS}" rev-parse HEAD)" != "${PIN}" ]; then
  echo "FATAL: ${CORPUS} is not at ${PIN}" >&2
  exit 2
fi

GENBENCH="${GENBENCH:-}"
if [ -z "${GENBENCH}" ]; then
  GENBENCH="${WORK}/genbench"
  ( cd "${REPO_ROOT}" && go build -o "${GENBENCH}" ./generator/cmd/genbench )
fi

# gen-system: the same copybook folders as the baseline. Run from the work
# folder, so the parser cache does not land in the repository.
CB="$(find "${CORPUS}" -type d -name '_copybooks' -o -type d -path '*_copybooks/*' | sort -u | paste -sd: -)"
( cd "${WORK}" && "${GENBENCH}" edges --root "${CORPUS}" --copybooks "${CB}" --out "${OUT}/gen-system.jsonl" )

# cobc: the include folders and flags of CobolCraft's Makefile. The code
# generation templates are fragments, not programs, so neither tool's view of
# them is compared.
SKIP="codegen/templates/*"
python3 "${HERE}/cobc_edges.py" --corpus "${CORPUS}" --out "${OUT}/cobc.jsonl" \
  --failures "${OUT}/cobc-rejected.jsonl" --flags "-free -DGCVERSION=31" \
  --include "=src/_copybooks,src/_copybooks/*" \
  --include "codegen/=codegen/_copybooks,codegen/_copybooks/*" \
  --include "tests/=tests/_copybooks,tests/_copybooks/*" \
  --skip "${SKIP}"
python3 "${HERE}/textscan_edges.py" --corpus "${CORPUS}" --out "${OUT}/textscan.jsonl" --skip "${SKIP}"

python3 "${HERE}/compare_cobol.py" --gen "${OUT}/gen-system.jsonl" --ref "${OUT}/textscan.jsonl" \
  --ref-name textscan --corpus "${CORPUS}" --skip "${SKIP}" \
  --md "${OUT}/vs-textscan.md" --json "${OUT}/vs-textscan.json" > /dev/null
strict=()
[ "${STRICT:-0}" = 1 ] && strict=(--require-match)
status=0
python3 "${HERE}/compare_cobol.py" --gen "${OUT}/gen-system.jsonl" --ref "${OUT}/cobc.jsonl" \
  --ref-name cobc --corpus "${CORPUS}" --skip "${SKIP}" --failures "${OUT}/cobc-rejected.jsonl" \
  --md "${OUT}/vs-cobc.md" --json "${OUT}/vs-cobc.json" "${strict[@]+"${strict[@]}"}" \
  > "${WORK}/compare.log" || status=$?
tail -n 1 "${WORK}/compare.log"

{
  echo "date:        $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "gen-system:  $(git -C "${REPO_ROOT}" rev-parse --short HEAD)$(git -C "${REPO_ROOT}" diff --quiet || echo ' (uncommitted changes)')"
  echo "CobolCraft:  ${PIN}"
  echo "cobc:        $(cobc --version | head -n1)"
  echo "python:      $(python3 --version)"
} > "${OUT}/versions.txt"
cat "${OUT}/versions.txt"
echo
sed -n '1,12p' "${OUT}/vs-cobc.md"
exit "${status}"
