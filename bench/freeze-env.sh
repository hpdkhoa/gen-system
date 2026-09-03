#!/usr/bin/env bash
# bench/freeze-env.sh — WP0: freeze the ground before any measured run.
# Writes bench/env.json with everything a reader needs to compare two runs:
# gen-system commit (and whether the tree was dirty), GPU + driver, Ollama
# version + installed models, Go version, date. Every results row produced by
# summarise.py references this file's commit.
#
# Commit bench/env.json and bench/tasks.json BEFORE the first run.
set -euo pipefail

BENCH_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${BENCH_DIR}/.." && pwd)"
OUT="${BENCH_DIR}/env.json"

commit="$(cd "${REPO_ROOT}" && git rev-parse HEAD 2>/dev/null || echo unknown)"
dirty=false
if [ -n "$(cd "${REPO_ROOT}" && git status --porcelain 2>/dev/null)" ]; then
  dirty=true
  echo "WARNING: working tree is dirty. Commit before measuring — git timestamps are the proof." >&2
fi

gpu="$(nvidia-smi --query-gpu=name,memory.total,driver_version --format=csv,noheader 2>/dev/null || echo 'nvidia-smi not available')"
ollama_version="$(ollama --version 2>/dev/null || echo 'ollama not available')"
go_version="$(go version 2>/dev/null || echo 'go not available')"

models="$(ollama list 2>/dev/null | python3 -c 'import sys,json; print(json.dumps(sys.stdin.read()))' || echo '"ollama not available"')"

cat > "${OUT}" <<EOF
{
  "frozen_at": "$(date -u +%FT%TZ)",
  "gen_system_commit": "${commit}",
  "working_tree_dirty": ${dirty},
  "gpu": "${gpu}",
  "ollama_version": "${ollama_version}",
  "go_version": "${go_version}",
  "ollama_list": ${models}
}
EOF

echo "wrote ${OUT}"
python3 -m json.tool "${OUT}" > /dev/null && echo "env.json is valid JSON"
echo
echo "Next: git add bench/env.json bench/tasks.json && git commit -m 'bench: freeze environment and task set'"
