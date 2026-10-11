#!/usr/bin/env bash
# freeze-cobol-baseline.sh, v23.25 (task F): reproduce / verify the frozen
# genbench baseline on the public MIT-licensed CobolCraft corpus.
#
# Two modes:
#   ./freeze-cobol-baseline.sh verify (default): regenerate a fresh run and
#       diff it against the committed baseline via `genbench regression --baseline`.
#       Exits non-zero (via --strict) if any content metric drifted.
#   ./freeze-cobol-baseline.sh freeze: overwrite the committed baseline JSON
#       with a freshly canonicalized run. Use only when you intend to move the
#       baseline (e.g. after a deliberate parser improvement).
#
# Requires: git, go (or a prebuilt genbench on PATH), python3.
# Network: clones github.com/meyfa/CobolCraft at the pinned commit.
#
# The corpus is not vendored into this repo (it is third-party MIT code); this
# script fetches it on demand so the baseline stays reproducible from source.

set -euo pipefail

# ── config ───────────────────────────────────────────────────────────
REPO_URL="https://github.com/meyfa/CobolCraft"
PIN_COMMIT="c6f34f90ecdff94c3171c2b67334f9a5dc6acffb"   # 2026-03-08
MODE="${1:-verify}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASELINE_JSON="${SCRIPT_DIR}/cobolcraft_v23.24.json"

# Repo root = four levels up from generator/internal/bench/baselines/.
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../../.." && pwd)"

WORK="$(mktemp -d)"
CORPUS="${WORK}/cobolcraft"
trap 'rm -rf "${WORK}"' EXIT

echo "── freeze-cobol-baseline (${MODE}) ──────────────────────────────────"
echo "repo root : ${REPO_ROOT}"
echo "baseline  : ${BASELINE_JSON}"
echo "corpus    : ${REPO_URL} @ ${PIN_COMMIT}"

# ── 1. fetch corpus at the pinned commit ─────────────────────────────
git clone --quiet "${REPO_URL}" "${CORPUS}"
( cd "${CORPUS}" && git checkout --quiet "${PIN_COMMIT}" )
GOT_COMMIT="$(cd "${CORPUS}" && git rev-parse HEAD)"
if [ "${GOT_COMMIT}" != "${PIN_COMMIT}" ]; then
  echo "FATAL: corpus commit mismatch (${GOT_COMMIT} != ${PIN_COMMIT})" >&2
  exit 2
fi

# ── 2. assemble the copybook search path (all _copybooks dirs) ───────
CB="$(find "${CORPUS}" -type d -name '_copybooks' -o -type d -path '*_copybooks/*' \
      | sort -u | paste -sd: -)"
echo "copybooks : $(echo "${CB}" | tr ':' '\n' | wc -l) dirs"

# ── 3. locate or build genbench ──────────────────────────────────────
GENBENCH="$(command -v genbench || true)"
if [ -z "${GENBENCH}" ]; then
  echo "building genbench…"
  GENBENCH="${WORK}/genbench"
  ( cd "${REPO_ROOT}" && go build -o "${GENBENCH}" ./generator/cmd/genbench )
fi
echo "genbench  : ${GENBENCH}"

# ── 4. run the three deterministic parser-path suites into one JSON ──
RAW="${WORK}/raw.json"
P="${WORK}/parse.json"; R="${WORK}/res.json"; C="${WORK}/cfg.json"
"${GENBENCH}" parse      --root "${CORPUS}" --copybooks "${CB}" --out "${P}" >/dev/null
"${GENBENCH}" resolution --root "${CORPUS}" --copybooks "${CB}" --out "${R}" >/dev/null
"${GENBENCH}" cfg        --root "${CORPUS}" --copybooks "${CB}" --out "${C}" >/dev/null

python3 - "${P}" "${R}" "${C}" "${RAW}" "${PIN_COMMIT}" <<'PY'
import json, sys
parse, res, cfg, out, commit = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5]
suites = []
for p in (parse, res, cfg):
    suites += json.load(open(p))['suites']
VOLATILE = {"files_per_sec_cobol","files_per_sec_overall","kb_per_sec_cobol","kb_per_sec_overall"}
run = {
  "schema": "genbench/v1",
  "started_at": "2026-01-01T00:00:00Z",
  "finished_at": "2026-01-01T00:00:00Z",
  "host": {"os":"linux","arch":"amd64","num_cpu":0,"go_version":"frozen","git_commit":""},
  "model": {"name":"n/a (parser-path baseline)","endpoint":"n/a"},
  "project_root": "meyfa/CobolCraft@"+commit,
  "suites": suites,
}
for s in run["suites"]:
    s["duration_ns"] = 0
    for m in s.get("metrics", []):
        if m["name"] in VOLATILE:
            m["value"] = 0.0
            if "(env-dependent" not in m.get("unit",""):
                m["unit"] = m.get("unit","") + " (env-dependent; excluded from regression)"
json.dump(run, open(out,"w"), indent=2)
print("merged + canonicalized:", out)
PY

# ── 5. freeze or verify ──────────────────────────────────────────────
if [ "${MODE}" = "freeze" ]; then
  cp "${RAW}" "${BASELINE_JSON}"
  echo "FROZEN → ${BASELINE_JSON}"
  echo "review the git diff before committing."
  exit 0
fi

# verify: diff fresh run against the committed baseline, fail on drift.
echo "── regression vs committed baseline (strict) ────────────────────────"
"${GENBENCH}" regression --baseline --new "${RAW}" --root "${REPO_ROOT}" --strict
echo "VERIFY OK — fresh CobolCraft run matches the frozen baseline."
