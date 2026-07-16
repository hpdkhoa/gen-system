#!/usr/bin/env bash
# install-service.sh — register gen-server as a systemd service that starts
# automatically on boot.
#
# Run AFTER install.sh has completed successfully.
#
# Usage:
#   sudo ./install-service.sh
#   sudo ./install-service.sh --port=8090 --root=/srv/myprojects
#   sudo ./install-service.sh --github-enabled            # mount /api/github/*
#   sudo ./install-service.sh --copybook-path=/srv/copybooks
#   sudo ./install-service.sh --uninstall
#
# What it does:
#   1. Writes /etc/systemd/system/gen-server.service
#   2. Writes /etc/gen-system/environment      (port, root, copybook path, etc.)
#   3. systemctl daemon-reload, enable, start
#   4. Prints status and the URL the studio is now reachable at

set -euo pipefail

# ─── Defaults (must match install.sh) ─────────────────────────────────────

INSTALL_PREFIX="/opt/gen-system"
SERVICE_USER="gensystem"
DATA_DIR="/var/lib/gen-system"
LOG_DIR="/var/log/gen-system"

DEFAULT_PORT="28080"
DEFAULT_ROOT="${DATA_DIR}/projects"
DEFAULT_COPYBOOK_PATH="${DATA_DIR}/copybooks"

SERVICE_NAME="gen-server"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
ENV_DIR="/etc/gen-system"
ENV_FILE="${ENV_DIR}/environment"

# ─── Parse flags ──────────────────────────────────────────────────────────

PORT="$DEFAULT_PORT"
ROOT="$DEFAULT_ROOT"
COPYBOOK_PATH="$DEFAULT_COPYBOOK_PATH"
GITHUB_ENABLED=""
UNINSTALL=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --port=*)           PORT="${1#*=}"; shift ;;
        --root=*)           ROOT="${1#*=}"; shift ;;
        --copybook-path=*)  COPYBOOK_PATH="${1#*=}"; shift ;;
        --github-enabled)   GITHUB_ENABLED="--github-enabled"; shift ;;
        --uninstall)        UNINSTALL=1; shift ;;
        -h|--help)
            grep -E '^# ' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)  echo "Unknown flag: $1" >&2; exit 2 ;;
    esac
done

if [[ $EUID -ne 0 ]]; then
    echo "This script must run as root. Use: sudo $0 $*" >&2
    exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
    echo "systemd not detected. This script only supports systemd-based distros." >&2
    echo "For OpenRC/SysV, write an /etc/init.d/gen-server script manually." >&2
    exit 1
fi

# ─── Uninstall path ───────────────────────────────────────────────────────

if [[ $UNINSTALL -eq 1 ]]; then
    echo "Uninstalling gen-server systemd service…"
    systemctl stop    "$SERVICE_NAME" 2>/dev/null || true
    systemctl disable "$SERVICE_NAME" 2>/dev/null || true
    rm -f "$SERVICE_FILE"
    rm -f "$ENV_FILE"
    systemctl daemon-reload
    echo "Done. Service files removed."
    echo "(Data directories under $DATA_DIR were left intact.)"
    exit 0
fi

# ─── Sanity checks ────────────────────────────────────────────────────────

if [[ ! -x "${INSTALL_PREFIX}/bin/gen-server" ]]; then
    echo "${INSTALL_PREFIX}/bin/gen-server not found." >&2
    echo "Run install.sh first." >&2
    exit 1
fi
if ! id "$SERVICE_USER" >/dev/null 2>&1; then
    echo "Service user $SERVICE_USER missing. Run install.sh first." >&2
    exit 1
fi

mkdir -p "$ROOT" "$COPYBOOK_PATH"
chown -R "$SERVICE_USER":"$SERVICE_USER" "$ROOT" "$COPYBOOK_PATH"

# ─── Environment file ─────────────────────────────────────────────────────

mkdir -p "$ENV_DIR"
cat > "$ENV_FILE" <<EOF
# /etc/gen-system/environment
# Sourced by gen-server.service. Edit and run 'systemctl restart gen-server'
# to apply changes. Quoting follows systemd EnvironmentFile semantics:
# no shell expansion, KEY=value, no spaces around the equals sign.

GEN_PORT=${PORT}
GEN_ROOT=${ROOT}

# Copybook path for COBOL COPY directive resolution.
# Multiple directories: separate with ':' (Linux).
GEN_COBOL_COPYBOOK_PATH=${COPYBOOK_PATH}

# Ollama endpoint (default uses local socket on :11434).
OLLAMA_HOST=http://127.0.0.1:11434

# Pass through any extra CLI flags here. Quoted as a single string.
GEN_SERVER_EXTRA_FLAGS=${GITHUB_ENABLED}
EOF

# Pull database URL into the env file if install.sh produced one.
if [[ -f "${INSTALL_PREFIX}/etc/database.env" ]]; then
    cat "${INSTALL_PREFIX}/etc/database.env" >> "$ENV_FILE"
fi

# v23.20: pull the role-based model selection (OLLAMA_MODEL_PLAN/_CODE,
# GEN_LOGIC) written by install.sh so the service uses the same models the
# interactive CLI does. (Harmless for the enterprise/understand-only build,
# which produces no models.env.)
if [[ -f "${INSTALL_PREFIX}/etc/models.env" ]]; then
    echo "" >> "$ENV_FILE"
    echo "# Role-based model selection (from install.sh)." >> "$ENV_FILE"
    grep -v '^#' "${INSTALL_PREFIX}/etc/models.env" >> "$ENV_FILE"
fi
chmod 640 "$ENV_FILE"
chown root:"$SERVICE_USER" "$ENV_FILE"
echo "wrote $ENV_FILE"

# ─── Service unit ─────────────────────────────────────────────────────────

cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=gen-system studio (gen-server)
Documentation=https://github.com/your-org/gen-system
After=network-online.target postgresql.service ollama.service
Wants=network-online.target
# Ollama is optional at runtime; don't fail if its unit name differs.
# postgresql is required only if you use the DB-backed features.

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
WorkingDirectory=${INSTALL_PREFIX}

# Source environment from the file written by this script.
EnvironmentFile=${ENV_FILE}

# Use shell form so \${GEN_SERVER_EXTRA_FLAGS} expands cleanly when empty.
ExecStart=/bin/sh -c '${INSTALL_PREFIX}/bin/gen-server \\
    --port=\${GEN_PORT} \\
    --root=\${GEN_ROOT} \\
    \${GEN_SERVER_EXTRA_FLAGS}'

# Logs go to systemd journal; view with: journalctl -u gen-server -f
StandardOutput=journal
StandardError=journal
SyslogIdentifier=gen-server

# Restart policy: keep the studio up.
Restart=on-failure
RestartSec=5s
StartLimitBurst=5
StartLimitIntervalSec=60

# Security hardening — gen-server is offline and read-only by default.
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=${DATA_DIR} ${LOG_DIR}
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
RestrictNamespaces=true
RestrictSUIDSGID=true
LockPersonality=true

# Resource limits — adjust to your hardware.
LimitNOFILE=65536
MemoryMax=4G

[Install]
WantedBy=multi-user.target
EOF

echo "wrote $SERVICE_FILE"

# ─── Activate ─────────────────────────────────────────────────────────────

systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
systemctl restart "$SERVICE_NAME"

sleep 2

echo
echo "═════════════════════════════════════════════════════════════════"
systemctl --no-pager status "$SERVICE_NAME" | head -15
echo "─────────────────────────────────────────────────────────────────"

# Determine the host's primary IP for the URL hint.
HOST_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
[[ -z "$HOST_IP" ]] && HOST_IP="localhost"

echo
echo "  ✅ gen-server is now installed as a systemd service"
echo
echo "  URL    : http://${HOST_IP}:${PORT}/intelligence.html"
echo "  Logs   : journalctl -u ${SERVICE_NAME} -f"
echo "  Config : ${ENV_FILE}"
echo "  Unit   : ${SERVICE_FILE}"
echo
echo "  Useful commands:"
echo "    systemctl status   ${SERVICE_NAME}"
echo "    systemctl restart  ${SERVICE_NAME}"
echo "    systemctl stop     ${SERVICE_NAME}"
echo "    systemctl disable  ${SERVICE_NAME}        # don't start on boot"
echo "    sudo $0 --uninstall                       # remove service entirely"
echo "═════════════════════════════════════════════════════════════════"
