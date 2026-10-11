#!/usr/bin/env bash
# uninstall.sh: remove gen-system from the server.
#
# By default, removes binaries, service files, and the service user.
# Pass --purge to also delete data directories.
#
# Usage:
#   sudo ./uninstall.sh             # keep data
#   sudo ./uninstall.sh --purge     # wipe data too
#   sudo ./uninstall.sh --purge --keep-postgres  # don't touch Postgres
#   sudo ./uninstall.sh --purge --keep-ollama    # don't touch Ollama

set -euo pipefail

INSTALL_PREFIX="/opt/gen-system"
SERVICE_USER="gensystem"
DATA_DIR="/var/lib/gen-system"
LOG_DIR="/var/log/gen-system"
ENV_DIR="/etc/gen-system"
SERVICE_FILE="/etc/systemd/system/gen-server.service"

PURGE=0
KEEP_POSTGRES=0
KEEP_OLLAMA=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --purge)           PURGE=1; shift ;;
        --keep-postgres)   KEEP_POSTGRES=1; shift ;;
        --keep-ollama)     KEEP_OLLAMA=1; shift ;;
        -h|--help)
            grep -E '^# ' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)  echo "Unknown flag: $1" >&2; exit 2 ;;
    esac
done

if [[ $EUID -ne 0 ]]; then
    echo "Must run as root. Use: sudo $0 $*" >&2
    exit 1
fi

echo "Uninstalling gen-system…"

# Service first
if command -v systemctl >/dev/null 2>&1; then
    systemctl stop    gen-server 2>/dev/null || true
    systemctl disable gen-server 2>/dev/null || true
    rm -f "$SERVICE_FILE"
    systemctl daemon-reload || true
fi

# Binaries and assets
rm -rf "$INSTALL_PREFIX"
rm -rf "$ENV_DIR"
echo "  removed $INSTALL_PREFIX and $ENV_DIR"

# Service user
if id "$SERVICE_USER" >/dev/null 2>&1; then
    userdel "$SERVICE_USER" 2>/dev/null || true
    echo "  removed user $SERVICE_USER"
fi

# Data directories (only with --purge)
if [[ $PURGE -eq 1 ]]; then
    rm -rf "$DATA_DIR" "$LOG_DIR"
    echo "  purged $DATA_DIR and $LOG_DIR"

    if [[ $KEEP_POSTGRES -eq 0 ]] && command -v psql >/dev/null 2>&1; then
        sudo -u postgres psql -c "DROP DATABASE IF EXISTS gensystem;"  2>/dev/null || true
        sudo -u postgres psql -c "DROP ROLE     IF EXISTS gensystem;"  2>/dev/null || true
        echo "  dropped postgres database and role"
    fi

    if [[ $KEEP_OLLAMA -eq 0 ]] && command -v ollama >/dev/null 2>&1; then
        ollama rm deepseek-coder 2>/dev/null || true
        echo "  removed deepseek-coder model from ollama"
    fi
else
    echo "  data directories kept at $DATA_DIR (pass --purge to delete)"
fi

echo "Done."
