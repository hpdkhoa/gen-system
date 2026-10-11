#!/usr/bin/env bash
# refresh.sh (v24): rebuild + restart in one command (install-kit open item #9).
#
# Auto-detects how gen-system is running and does the right thing:
#   • systemd service present  → rebuild, then `systemctl restart`
#   • otherwise (manual run)   → rebuild, then re-exec the server in the foreground
#
# After a parser change the on-disk AST cache (<root>/.gen/ast_cache) is keyed on
# file checksum, not parser version, so a stale parse can survive a rebuild. This
# script clears it by default; pass --keep-cache to preserve it.
#
# Usage:
#   ./refresh.sh                       # rebuild (internal), clear cache, restart
#   ./refresh.sh --understand          # rebuild the enterprise binary instead
#   ./refresh.sh --root DIR --port N   # manual-run passthrough to the server
#   ./refresh.sh --keep-cache          # do not clear <root>/.gen/ast_cache
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"

MODE="--internal"
KEEP_CACHE=0
ROOT=""
declare -a PASS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --understand) MODE="--understand"; shift ;;
    --internal)   MODE="--internal";   shift ;;
    --keep-cache) KEEP_CACHE=1;        shift ;;
    --root)       ROOT="${2:-}"; PASS+=("$1" "${2:-}"); shift 2 ;;
    *)            PASS+=("$1"); shift ;;
  esac
done

echo "→ refresh: building ($MODE)…"
./build.sh "$MODE"

# Clear the AST cache unless asked not to (safe default after a parser change).
if [[ "$KEEP_CACHE" -eq 0 ]]; then
  # Clear the cache under --root if given, else any .gen/ast_cache under CWD.
  if [[ -n "$ROOT" && -d "$ROOT/.gen/ast_cache" ]]; then
    echo "→ clearing AST cache: $ROOT/.gen/ast_cache"
    rm -rf "$ROOT/.gen/ast_cache"
  else
    while IFS= read -r -d '' d; do
      echo "→ clearing AST cache: $d"
      rm -rf "$d"
    done < <(find . -type d -path '*/.gen/ast_cache' -print0 2>/dev/null || true)
  fi
fi

# Restart: systemd if a unit exists, otherwise re-exec in the foreground.
SERVICE=""
for name in gen-server gen-system understand-server; do
  if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files 2>/dev/null | grep -q "^${name}\.service"; then
    SERVICE="$name"; break
  fi
done

if [[ -n "$SERVICE" ]]; then
  echo "→ systemd unit '$SERVICE' found — restarting"
  sudo systemctl restart "$SERVICE"
  sudo systemctl --no-pager --lines=5 status "$SERVICE" || true
else
  echo "→ no systemd unit — starting the server in the foreground"
  BIN="./gen-server"
  [[ "$MODE" == "--understand" ]] && BIN="./understand-server"
  if [[ ! -x "$BIN" ]]; then
    echo "  (expected $BIN after build; adjust if your build output path differs)"
  fi
  exec "$BIN" "${PASS[@]}"
fi
