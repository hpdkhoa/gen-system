#!/usr/bin/env bash
# bench/swebench/run_swebench_localize.sh: localization recall on SWE-bench Verified.
#
# WHAT THIS MEASURES. For each task: clone the repo, check out the task's base
# commit, extract seed identifiers from the issue text and the gold files from
# the gold patch, then ask the Understand engine whether the gold files sit in
# the call graph neighbourhood of the seeds within N hops. That is the file
# recovery / Full Recall Rate line of DeepDiscovery (arXiv 2606.22906).
#
# WHAT THIS IS NOT. It is not a solve rate. gen-system has no issue to patch
# pipeline. DeepDiscovery's 78.6 percent on SWE-bench Verified is a solve rate
# and is not comparable to anything written by this script. Do not put the two
# numbers in the same sentence.
#
# Usage:
#   bench/swebench/run_swebench_localize.sh [--limit N] [--repo owner/name] [--hops H]
#
# Needs: git, go, python3 (3.9+, also used by the Python frontend), network for
# the dataset fetch and the clones. No GPU, no model. Full run is 500 checkouts
# and 500 index loads; start with --limit 20.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${HERE}/../lib.sh"

LIMIT=0; ONLY_REPO=""; HOPS=2
while [ $# -gt 0 ]; do
  case "$1" in
    --limit) LIMIT="$2"; shift 2 ;;
    --repo)  ONLY_REPO="$2"; shift 2 ;;
    --hops)  HOPS="$2"; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

build_genbench
require_env_json

SB="${RESULTS}/swebench"
mkdir -p "${SB}/repos" "${SB}/tasks"

echo "════ SWE-bench Verified: localization recall (hops=${HOPS}) ════"
python3 "${HERE}/fetch_tasks.py"

# task list: instance_id <tab> repo <tab> base_commit, filtered and limited
python3 - "${HERE}/tasks.jsonl" "${ONLY_REPO}" "${LIMIT}" > "${SB}/tasklist.tsv" <<'PY'
import json, sys
path, only, limit = sys.argv[1], sys.argv[2], int(sys.argv[3])
n = 0
for line in open(path, encoding="utf-8"):
    r = json.loads(line)
    if only and r["repo"] != only:
        continue
    print(f'{r["instance_id"]}\t{r["repo"]}\t{r["base_commit"]}')
    n += 1
    if limit and n >= limit:
        break
PY
echo "tasks selected: $(wc -l < "${SB}/tasklist.tsv")"

# One clone per repository; checkouts move between task commits.
while IFS=$'\t' read -r instance repo commit; do
  rdir="${SB}/repos/${repo//\//__}"
  if [ ! -d "${rdir}/.git" ]; then
    echo "──── cloning ${repo} ────"
    git clone --quiet "https://github.com/${repo}" "${rdir}"
  fi
  git -C "${rdir}" checkout --quiet --force "${commit}" || { echo "!! ${instance}: checkout ${commit} failed, skipping"; continue; }

  tdir="${SB}/tasks/${instance}"
  python3 "${HERE}/prep_task.py" "${instance}" "${tdir}"

  # cwd = results dir so the parse cache (.gen/ast_cache, relative to cwd)
  # lands there and is shared across the 500 checkouts, not inside the clone.
  ( cd "${RESULTS}" && "${GENBENCH}" localize \
      --root "${rdir}" --seeds-file "${tdir}/seeds.txt" --gold-file "${tdir}/gold.txt" \
      --hops "${HOPS}" --out "${RAW}/swebench-${instance}.json" ) || true
done < "${SB}/tasklist.tsv"

# Can the parsers read these repositories at all? Once per repo, at whatever
# commit is checked out last. Parse and resolution suites, no model.
echo "──── per-repository readability ────"
for rdir in "${SB}"/repos/*/; do
  name="$(basename "${rdir}")"
  ( cd "${RESULTS}" && "${GENBENCH}" parse      --root "${rdir}" --out "${RAW}/swebench-repo-${name}-parse.json" ) || true
  ( cd "${RESULTS}" && "${GENBENCH}" resolution --root "${rdir}" --out "${RAW}/swebench-repo-${name}-resolution.json" ) || true
done

python3 "${HERE}/summarise_swebench.py" --results "${RESULTS}" ${MEASURED:+--measured "${MEASURED}"}

cat <<EOF

Raw per task results: ${RAW}/swebench-<instance>.json
Seeds and gold files used: ${SB}/tasks/<instance>/
Clones are under ${SB}/repos/ and are git ignored. Everything else under results/ is evidence; commit it.
EOF
