#!/usr/bin/env bash
# install.sh: gen-system installation script for a fresh Linux server.
# v23.11 (Phase 0): added --with-toolchains to provision the full-stack
#   verification toolchain (Node 20 LTS, JDK 17 + Maven, Docker engine) so the
#   box can build/verify the web, mobile, Java(17) and Docker layers as well as
#   the Go backend. Purely additive: a new p0_* function block + one call site
#   after Go install; existing 8 steps and the enterprise/--understand path are
#   untouched (toolchains are auto-skipped under --understand).
# v23.1: --model= / --extra-models= flags + OLLAMA_MODEL env; model pull is now
# a loop (primary + extras). The engine reads OLLAMA_MODEL at runtime.
#
# Handles:
#   1. System packages   (curl, git, build-essential, ca-certificates, jq)
#   2. Go 1.25.0         (downloaded from go.dev; apt versions are too old)
#   3. Ollama            (local LLM runtime, official installer)
#   4. Ollama models    (qwen3:14b + qwen2.5-coder:14b by default; ~9 GB each)
#   5. PostgreSQL 15     (apt + initial database/role; optional, skip with --no-postgres)
#   6. Build gen + gen-server (from the source tree this script lives next to)
#   7. Install to /opt/gen-system (or --prefix=...)
#   8. Create gen-system user + working directories
#
# Tested on:  Ubuntu 22.04, Ubuntu 24.04, Debian 12.
# Other distros: see DISTRO NOTES at the bottom of this file.
#
# Usage:
#   sudo ./install.sh                      # default install (full/internal)
#   sudo ./install.sh --understand         # enterprise: only the read only tools (understand-server, -mcp, -review)
#                                          #   (UI embedded). Forces no Ollama /
#                                          #   no model / no Postgres: a clean
#                                          #   air-gapped appliance.
#   sudo ./install.sh --no-postgres        # skip Postgres (using external DB)
#   sudo ./install.sh --no-ollama          # skip Ollama (offline reinstall)
#   sudo ./install.sh --prefix=/srv/gen    # custom install prefix
#   sudo ./install.sh --skip-model         # don't pull any model (do it later)
#   sudo ./install.sh --plan-model=NAME    # planner model (default qwen3:14b)
#   sudo ./install.sh --code-model=NAME    # code model (default qwen2.5-coder:14b)
#   sudo ./install.sh --model=NAME         # legacy: one model for both roles
#   sudo ./install.sh --extra-models="a b" # also pull these (space-separated)
#   sudo ./install.sh --with-toolchains    # ALSO install Node 20 + JDK 17 +
#                                          #   Maven + Docker for verifying the
#                                          #   web/mobile/Java/Docker layers
#                                          #   (Phase 0 full-stack build env).
#                                          #   Ignored under --understand.
#
# After install:
#   ./install-service.sh                   # to register systemd auto-start

set -euo pipefail

# ─── Config ───────────────────────────────────────────────────────────────

GO_VERSION="1.25.0"
GO_TARBALL="go${GO_VERSION}.linux-amd64.tar.gz"
GO_URL="https://go.dev/dl/${GO_TARBALL}"

OLLAMA_MODEL="${OLLAMA_MODEL:-}"   # legacy single-model override (back-compat).
                                   # If set, it is used for both roles unless a
                                   # role-specific model below is also set.
# v23.20: per-task (role-based) model selection. The planner (idea analysis,
# architecture, entity design) wants a reasoning model; code generation wants a
# coder model. Defaults are the recommended dense-14B pair that fits a 16 GB GPU
# (e.g. RTX 4060 Ti). Override with --plan-model= / --code-model=.
PLAN_MODEL="${PLAN_MODEL:-qwen3:14b}"
CODE_MODEL="${CODE_MODEL:-qwen2.5-coder:14b}"
# v23.1: extra models to pull alongside the primary set, space-separated.
EXTRA_MODELS="${EXTRA_MODELS:-}"

INSTALL_PREFIX="/opt/gen-system"
SERVICE_USER="gensystem"
DATA_DIR="/var/lib/gen-system"
LOG_DIR="/var/log/gen-system"

DEFAULT_PORT=28080

# Parse flags
SKIP_POSTGRES=0
SKIP_OLLAMA=0
SKIP_MODEL=0
WITH_TOOLCHAINS=0   # v23.11 (Phase 0): provision Node/JDK17/Maven/Docker
UNDERSTAND_ONLY=0   # v23.9 (Phase 5): enterprise mode: ship only understand-server
while [[ $# -gt 0 ]]; do
    case "$1" in
        --understand)   UNDERSTAND_ONLY=1; shift ;;
        --no-postgres)  SKIP_POSTGRES=1; shift ;;
        --no-ollama)    SKIP_OLLAMA=1;   shift ;;
        --skip-model)   SKIP_MODEL=1;    shift ;;
        --with-toolchains) WITH_TOOLCHAINS=1; shift ;;
        --model=*)      OLLAMA_MODEL="${1#*=}"; shift ;;
        --plan-model=*) PLAN_MODEL="${1#*=}"; shift ;;
        --code-model=*) CODE_MODEL="${1#*=}"; shift ;;
        --extra-models=*) EXTRA_MODELS="${1#*=}"; shift ;;
        --prefix=*)     INSTALL_PREFIX="${1#*=}"; shift ;;
        -h|--help)
            grep -E '^# ' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)  echo "Unknown flag: $1" >&2; exit 2 ;;
    esac
done

# Enterprise mode: the shippable appliance is understanding-only. It must not
# install a model runtime or Postgres-for-generation; those belong to the
# internal build. Force them off so `--understand` alone yields a clean,
# air-gapped install with no LLM present on the box.
if [[ $UNDERSTAND_ONLY -eq 1 ]]; then
    SKIP_OLLAMA=1
    SKIP_MODEL=1
    SKIP_POSTGRES=1
    # The enterprise appliance never builds web/mobile/Java/Docker layers;
    # it only reads code. Provisioning those toolchains would defeat the
    # air-gapped, minimal-surface goal, so force them off too.
    WITH_TOOLCHAINS=0
fi

# ─── Resolve effective models (v23.20) ─────────────────────────────────────
# Legacy --model=/OLLAMA_MODEL, if given, wins for both roles (back-compat:
# someone who only knows the old flag gets one model everywhere). Otherwise the
# role-specific PLAN_MODEL / CODE_MODEL (defaulting to the 16 GB-friendly pair)
# are used. The runtime resolves OLLAMA_MODEL_PLAN/_CODE → OLLAMA_MODEL →
# built-in default, so writing the role vars into the environment (step c,
# below) makes `gen idea`/`gen plan`/`gen run` use the right model per phase.
if [[ -n "${OLLAMA_MODEL}" ]]; then
    PLAN_MODEL="${OLLAMA_MODEL}"
    CODE_MODEL="${OLLAMA_MODEL}"
fi

# Build a de-duplicated pull list: plan, code, then any extras.
MODELS_TO_PULL=""
add_pull() {
    local m="$1"
    [[ -z "$m" ]] && return 0
    case " $MODELS_TO_PULL " in
        *" $m "*) ;;                       # already queued
        *) MODELS_TO_PULL="${MODELS_TO_PULL} $m" ;;
    esac
}
add_pull "$PLAN_MODEL"
add_pull "$CODE_MODEL"
for m in $EXTRA_MODELS; do add_pull "$m"; done
MODELS_TO_PULL="${MODELS_TO_PULL# }"

# ─── Pre-flight checks ────────────────────────────────────────────────────

if [[ $EUID -ne 0 ]]; then
    echo "This script must run as root. Use: sudo $0 $*" >&2
    exit 1
fi

# Detect arch. We only ship the amd64 Go tarball; the code below handles the arm64 fallback.
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64) GO_TARBALL="go${GO_VERSION}.linux-amd64.tar.gz" ;;
    aarch64|arm64) GO_TARBALL="go${GO_VERSION}.linux-arm64.tar.gz" ;;
    *) echo "Unsupported CPU architecture: $ARCH" >&2; exit 1 ;;
esac
GO_URL="https://go.dev/dl/${GO_TARBALL}"

# Detect distro family
if [[ -f /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    DISTRO_ID="${ID:-unknown}"
    DISTRO_LIKE="${ID_LIKE:-}"
else
    DISTRO_ID="unknown"
    DISTRO_LIKE=""
fi

case "$DISTRO_ID" in
    ubuntu|debian) PKG_MGR="apt-get" ;;
    *)
        if [[ "$DISTRO_LIKE" == *debian* ]]; then
            PKG_MGR="apt-get"
        elif command -v dnf >/dev/null 2>&1; then
            PKG_MGR="dnf"
        elif command -v yum >/dev/null 2>&1; then
            PKG_MGR="yum"
        else
            echo "Unsupported distro: $DISTRO_ID. Manual install required." >&2
            exit 1
        fi
        ;;
esac

# Where this script lives. The code uses it to locate the project source tree.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_SRC="${SCRIPT_DIR}"
if [[ ! -f "${PROJECT_SRC}/go.mod" ]]; then
    # If install.sh sits in an install/ subdirectory, look one level up.
    if [[ -f "${SCRIPT_DIR}/../go.mod" ]]; then
        PROJECT_SRC="$(cd "${SCRIPT_DIR}/.." && pwd)"
    else
        echo "Cannot find go.mod next to install.sh." >&2
        echo "Place install.sh in the gen-system project root, or one level up." >&2
        exit 1
    fi
fi

echo "═════════════════════════════════════════════════════════════════"
echo "  gen-system installer"
echo "─────────────────────────────────────────────────────────────────"
echo "  source tree     : $PROJECT_SRC"
echo "  install prefix  : $INSTALL_PREFIX"
echo "  service user    : $SERVICE_USER"
echo "  data dir        : $DATA_DIR"
echo "  log dir         : $LOG_DIR"
echo "  distro          : $DISTRO_ID ($PKG_MGR)"
echo "  arch            : $ARCH"
echo "  postgres        : $([[ $SKIP_POSTGRES -eq 1 ]] && echo skip || echo install)"
echo "  ollama          : $([[ $SKIP_OLLAMA -eq 1 ]] && echo skip || echo install)"
echo "  model pull      : $([[ $SKIP_MODEL -eq 1 ]] && echo skip || echo "plan=$PLAN_MODEL code=$CODE_MODEL")"
echo "  extra models    : ${EXTRA_MODELS:-none}"
echo "  toolchains      : $([[ $WITH_TOOLCHAINS -eq 1 ]] && echo "Node20+JDK17+Maven+Docker" || echo skip)"
echo "═════════════════════════════════════════════════════════════════"
echo

# ─── 1. System packages ───────────────────────────────────────────────────

echo "[1/8] Installing base system packages…"
case "$PKG_MGR" in
    apt-get)
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -y
        apt-get install -y \
            curl wget ca-certificates git \
            build-essential pkg-config \
            jq tar
        ;;
    dnf|yum)
        $PKG_MGR install -y \
            curl wget ca-certificates git \
            gcc gcc-c++ make pkg-config \
            jq tar
        ;;
esac
echo "      ok"

# ─── 2. Go ────────────────────────────────────────────────────────────────

echo "[2/8] Installing Go ${GO_VERSION}…"
INSTALLED_GO_VERSION=""
if command -v go >/dev/null 2>&1; then
    INSTALLED_GO_VERSION="$(go version | awk '{print $3}' | sed 's/^go//')"
fi

if [[ "$INSTALLED_GO_VERSION" == "$GO_VERSION" ]]; then
    echo "      Go ${GO_VERSION} already present, skipping"
else
    rm -rf /usr/local/go
    TMP_GO="/tmp/${GO_TARBALL}"
    echo "      downloading ${GO_URL}"
    curl -fsSL "$GO_URL" -o "$TMP_GO"
    tar -C /usr/local -xzf "$TMP_GO"
    rm -f "$TMP_GO"
    # Add to system PATH (idempotent)
    if ! grep -q "/usr/local/go/bin" /etc/profile.d/go.sh 2>/dev/null; then
        cat > /etc/profile.d/go.sh <<'EOF'
export PATH=$PATH:/usr/local/go/bin
export GOPATH=${GOPATH:-$HOME/go}
EOF
        chmod 644 /etc/profile.d/go.sh
    fi
    export PATH=$PATH:/usr/local/go/bin
    echo "      installed: $(/usr/local/go/bin/go version)"
fi

# Make sure subsequent commands in this script see go.
export PATH=$PATH:/usr/local/go/bin

# ─── 2.5 Full-stack verification toolchains (Phase 0) ─────────────────────
#
# Optional (--with-toolchains). Provisions everything needed to build and
# verify the layers beyond the Go backend:
#   • Node.js 20 LTS + npm  → React web build, React Native (Expo) bundle,
#                             TypeScript `tsc --noEmit` contract gate
#   • JDK 17 + Maven        → Java 17 understanding/generation track
#   • Docker engine         → `docker build` / `docker compose up` layer
#
# Each installer is idempotent and distro-aware (apt primary, dnf/yum
# fallback), mirroring the rest of this script. Failures are warned, not
# fatal: a missing toolchain only blocks its own layer, never the Go build.

p0_have() { command -v "$1" >/dev/null 2>&1; }

p0_install_node() {
    if p0_have node; then
        local v; v="$(node --version 2>/dev/null)"
        echo "      node already present: ${v}"
        return 0
    fi
    echo "      installing Node.js 20 LTS…"
    case "$PKG_MGR" in
        apt-get)
            # NodeSource setup script adds the repo + key, then apt installs.
            curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
                && apt-get install -y nodejs \
                || echo "      ⚠ node install failed (web/mobile/TS layers unavailable)"
            ;;
        dnf|yum)
            curl -fsSL https://rpm.nodesource.com/setup_20.x | bash - \
                && $PKG_MGR install -y nodejs \
                || echo "      ⚠ node install failed (web/mobile/TS layers unavailable)"
            ;;
    esac
    p0_have node && echo "      node ok: $(node --version)"
}

p0_install_java() {
    if p0_have javac; then
        local v; v="$(javac -version 2>&1)"
        echo "      JDK already present: ${v}"
    else
        echo "      installing JDK 17 (Temurin/OpenJDK)…"
        case "$PKG_MGR" in
            apt-get)
                # openjdk-17-jdk is in the default Ubuntu 22.04/24.04 + Debian 12 repos.
                apt-get install -y openjdk-17-jdk \
                    || echo "      ⚠ JDK 17 install failed (Java layer unavailable)"
                ;;
            dnf|yum)
                $PKG_MGR install -y java-17-openjdk-devel \
                    || echo "      ⚠ JDK 17 install failed (Java layer unavailable)"
                ;;
        esac
    fi
    if p0_have mvn; then
        echo "      maven already present: $(mvn -v 2>/dev/null | head -1)"
    else
        echo "      installing Maven…"
        case "$PKG_MGR" in
            apt-get) apt-get install -y maven || echo "      ⚠ maven install failed" ;;
            dnf|yum) $PKG_MGR install -y maven || echo "      ⚠ maven install failed" ;;
        esac
    fi
}

p0_install_docker() {
    if p0_have docker; then
        echo "      docker already present: $(docker --version 2>/dev/null)"
    else
        echo "      installing Docker engine…"
        # get.docker.com handles apt/dnf/yum distros itself, including the
        # compose plugin on current releases.
        curl -fsSL https://get.docker.com | sh \
            || echo "      ⚠ docker install failed (Docker layer unavailable)"
        if p0_have systemctl; then
            systemctl enable docker 2>/dev/null || true
            systemctl start docker 2>/dev/null || true
        fi
    fi
    # Let the service user run docker without sudo (best-effort).
    if p0_have docker && getent group docker >/dev/null 2>&1; then
        usermod -aG docker "$SERVICE_USER" 2>/dev/null || true
    fi
}

if [[ $WITH_TOOLCHAINS -eq 1 ]]; then
    echo "[2.5] Provisioning full-stack toolchains (Node 20, JDK 17, Maven, Docker)…"
    p0_install_node
    p0_install_java
    p0_install_docker
    echo "      toolchain provisioning done (warnings above are per-layer, non-fatal)"
else
    echo "[2.5] Skipping full-stack toolchains (pass --with-toolchains to install)"
fi

# ─── 3. Ollama ────────────────────────────────────────────────────────────

if [[ $SKIP_OLLAMA -eq 0 ]]; then
    echo "[3/8] Installing Ollama…"
    if command -v ollama >/dev/null 2>&1; then
        echo "      ollama already installed: $(ollama --version 2>/dev/null | head -1)"
    else
        curl -fsSL https://ollama.com/install.sh | sh
        echo "      installed"
    fi

    # Ollama install script sets up a systemd unit on most distros. Start it.
    if command -v systemctl >/dev/null 2>&1; then
        systemctl enable ollama 2>/dev/null || true
        systemctl start ollama 2>/dev/null || true
        # Give the server a moment to bind to :11434
        sleep 2
    fi
else
    echo "[3/8] Skipping Ollama (--no-ollama)"
fi

# ─── 4. Model pull ────────────────────────────────────────────────────────

# v23.20: pull the resolved plan + code models (plus any extras), de-duplicated.
# The runtime selects per phase via OLLAMA_MODEL_PLAN / OLLAMA_MODEL_CODE, which
# step (c) writes into the environment below.
pull_model() {
    local m="$1"
    if ollama list 2>/dev/null | awk '{print $1}' | grep -q "^${m}"; then
        echo "      ${m}: already present, skipping"
    else
        echo "      ${m}: pulling…"
        ollama pull "$m" && echo "      ${m}: pulled" || echo "      ⚠ ${m}: pull failed"
    fi
}

if [[ $SKIP_OLLAMA -eq 0 && $SKIP_MODEL -eq 0 ]]; then
    echo "[4/8] Pulling Ollama models — plan: ${PLAN_MODEL}, code: ${CODE_MODEL}…"
    for m in $MODELS_TO_PULL; do
        pull_model "$m"
    done
else
    echo "[4/8] Skipping model pull (later: ollama pull ${PLAN_MODEL} ${CODE_MODEL})"
fi

# ─── 4b. Wire the role-based model selection into the environment (v23.20) ──
# So that `gen idea` / `gen plan` / `gen run` (run interactively) AND the
# gen-server service both pick the right model per phase, write the role vars to:
#   • a shared env file the service sources (install-service.sh reads it), and
#   • /etc/profile.d so interactive login shells export them automatically.
# GEN_LOGIC=ollama is also set so model-written business logic is on by default;
# unset it (or set GEN_LOGIC=) to fall back to deterministic templates.
if [[ $SKIP_OLLAMA -eq 0 ]]; then
    mkdir -p "${INSTALL_PREFIX}/etc"
    cat > "${INSTALL_PREFIX}/etc/models.env" <<EOF
# Written by install.sh (v23.20). Role-based Ollama model selection.
# Resolution at runtime: OLLAMA_MODEL_PLAN/_CODE → OLLAMA_MODEL → built-in.
OLLAMA_MODEL_PLAN=${PLAN_MODEL}
OLLAMA_MODEL_CODE=${CODE_MODEL}
GEN_LOGIC=ollama
EOF
    chmod 644 "${INSTALL_PREFIX}/etc/models.env"
    echo "      wrote ${INSTALL_PREFIX}/etc/models.env (plan=${PLAN_MODEL}, code=${CODE_MODEL})"

    # Interactive shells: export the same values via profile.d.
    cat > /etc/profile.d/gen-system-models.sh <<EOF
# gen-system role-based model selection (install.sh v23.20).
export OLLAMA_MODEL_PLAN="${PLAN_MODEL}"
export OLLAMA_MODEL_CODE="${CODE_MODEL}"
export GEN_LOGIC=ollama
EOF
    chmod 644 /etc/profile.d/gen-system-models.sh
    echo "      wrote /etc/profile.d/gen-system-models.sh (login shells export plan/code models)"
fi

# ─── 5. PostgreSQL ────────────────────────────────────────────────────────

if [[ $SKIP_POSTGRES -eq 0 ]]; then
    echo "[5/8] Installing PostgreSQL 15…"
    case "$PKG_MGR" in
        apt-get)
            apt-get install -y postgresql postgresql-contrib
            ;;
        dnf|yum)
            $PKG_MGR install -y postgresql-server postgresql-contrib
            postgresql-setup --initdb 2>/dev/null || true
            ;;
    esac

    if command -v systemctl >/dev/null 2>&1; then
        systemctl enable postgresql 2>/dev/null || true
        systemctl start postgresql 2>/dev/null || true
        sleep 2
    fi

    # Create the gen-system role + database if they don't exist.
    # All operations are idempotent.
    PG_USER="gensystem"
    PG_PASS="$(head -c 24 /dev/urandom | base64 | tr -d '=+/' | head -c 24)"
    PG_DB="gensystem"

    sudo -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='${PG_USER}'" | grep -q 1 \
        || sudo -u postgres psql -c "CREATE ROLE ${PG_USER} WITH LOGIN PASSWORD '${PG_PASS}';"
    sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='${PG_DB}'" | grep -q 1 \
        || sudo -u postgres psql -c "CREATE DATABASE ${PG_DB} OWNER ${PG_USER};"

    # Save credentials in a root-readable file (the service env will source it).
    mkdir -p "${INSTALL_PREFIX}/etc"
    cat > "${INSTALL_PREFIX}/etc/database.env" <<EOF
GEN_DB_URL=postgres://${PG_USER}:${PG_PASS}@localhost:5432/${PG_DB}?sslmode=disable
EOF
    chmod 640 "${INSTALL_PREFIX}/etc/database.env"
    echo "      database '${PG_DB}' ready (creds in ${INSTALL_PREFIX}/etc/database.env)"
else
    echo "[5/8] Skipping PostgreSQL (--no-postgres)"
fi

# ─── 6. Build gen-system ──────────────────────────────────────────────────

echo "[6/8] Building gen-system binaries…"
cd "$PROJECT_SRC"

# Pre-fetch deps (offline rebuilds will not need network).
GOFLAGS="${GOFLAGS:-}" go mod download

mkdir -p "${INSTALL_PREFIX}/bin"
if [[ $UNDERSTAND_ONLY -eq 1 ]]; then
    # Enterprise: a single self-contained binary with the Studio UI embedded.
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
        -o "${INSTALL_PREFIX}/bin/understand-server" ./generator/cmd/understand-server
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
        -o "${INSTALL_PREFIX}/bin/understand-mcp" ./generator/cmd/understand-mcp
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
        -o "${INSTALL_PREFIX}/bin/understand-review" ./generator/cmd/understand-review
    chmod 755 "${INSTALL_PREFIX}/bin/understand-server" "${INSTALL_PREFIX}/bin/understand-mcp" "${INSTALL_PREFIX}/bin/understand-review"
    echo "      built ${INSTALL_PREFIX}/bin/understand-server (enterprise, UI embedded)"
    echo "      built ${INSTALL_PREFIX}/bin/understand-mcp (the same engine for MCP agents)"
    echo "      built ${INSTALL_PREFIX}/bin/understand-review (reviews a change)"
else
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
        -o "${INSTALL_PREFIX}/bin/gen" ./generator/cmd/gen
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
        -o "${INSTALL_PREFIX}/bin/gen-server" ./generator/cmd/gen-server
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
        -o "${INSTALL_PREFIX}/bin/understand-server" ./generator/cmd/understand-server
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
        -o "${INSTALL_PREFIX}/bin/understand-mcp" ./generator/cmd/understand-mcp
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
        -o "${INSTALL_PREFIX}/bin/understand-review" ./generator/cmd/understand-review
    chmod 755 "${INSTALL_PREFIX}/bin/gen" "${INSTALL_PREFIX}/bin/gen-server" \
              "${INSTALL_PREFIX}/bin/understand-server" "${INSTALL_PREFIX}/bin/understand-mcp" \
              "${INSTALL_PREFIX}/bin/understand-review"
    echo "      built ${INSTALL_PREFIX}/bin/gen"
    echo "      built ${INSTALL_PREFIX}/bin/gen-server"
    echo "      built ${INSTALL_PREFIX}/bin/understand-server"
    echo "      built ${INSTALL_PREFIX}/bin/understand-mcp"
    echo "      built ${INSTALL_PREFIX}/bin/understand-review"
fi

# Copy the web/ assets next to the binary so the dev gen-server can serve them.
# Enterprise understand-server has the UI embedded, so it needs no web/ dir.
if [[ $UNDERSTAND_ONLY -eq 0 && -d "${PROJECT_SRC}/web" ]]; then
    mkdir -p "${INSTALL_PREFIX}/web"
    cp -r "${PROJECT_SRC}/web/." "${INSTALL_PREFIX}/web/"
    echo "      copied web/ assets (dev server)"
fi

# ─── 7. Service user and dirs ─────────────────────────────────────────────

echo "[7/8] Creating service user and directories…"
if ! id "$SERVICE_USER" >/dev/null 2>&1; then
    useradd --system --shell /usr/sbin/nologin --home "$DATA_DIR" \
            --create-home --user-group "$SERVICE_USER"
    echo "      created user $SERVICE_USER"
fi

install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0755 "$DATA_DIR"
install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0755 "$LOG_DIR"
install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0755 "${DATA_DIR}/.gen"
install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0755 "${DATA_DIR}/projects"
install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0755 "${DATA_DIR}/copybooks"

if [[ -f "${INSTALL_PREFIX}/etc/database.env" ]]; then
    chown root:"$SERVICE_USER" "${INSTALL_PREFIX}/etc/database.env"
fi

echo "      data: $DATA_DIR"
echo "      logs: $LOG_DIR"

# ─── 8. Sanity check ──────────────────────────────────────────────────────

echo "[8/8] Sanity check…"
SANITY_BIN="gen-server"
[[ $UNDERSTAND_ONLY -eq 1 ]] && SANITY_BIN="understand-server"
if file "${INSTALL_PREFIX}/bin/${SANITY_BIN}" | grep -q ELF; then
    echo "      ${SANITY_BIN} binary ok"
else
    echo "      ${SANITY_BIN} binary missing or broken" >&2; exit 1
fi

if [[ $SKIP_OLLAMA -eq 0 ]]; then
    if curl -fsS http://localhost:11434/api/tags >/dev/null 2>&1; then
        echo "      ollama API reachable at :11434"
    else
        echo "      ⚠ ollama API not responding on :11434 (will recover on service start)"
    fi
fi

if [[ $WITH_TOOLCHAINS -eq 1 ]]; then
    echo "      toolchain check:"
    for t in node npm javac mvn docker; do
        if command -v "$t" >/dev/null 2>&1; then
            printf "        %-7s ✅ %s\n" "$t" "$($t --version 2>&1 | head -1)"
        else
            printf "        %-7s ⚠ not installed (its layer will be unavailable)\n" "$t"
        fi
    done
fi

echo
echo "═════════════════════════════════════════════════════════════════"
echo "  ✅ Installation complete"
echo "─────────────────────────────────────────────────────────────────"
echo "  Manually start the server:"
echo "    sudo -u $SERVICE_USER ${INSTALL_PREFIX}/bin/gen-server \\"
echo "        --root=$DATA_DIR/projects --port=$DEFAULT_PORT"
echo
echo "  Register as systemd service (auto-start on boot):"
echo "    sudo ${PROJECT_SRC}/install-service.sh"
echo
if [[ $SKIP_POSTGRES -eq 0 ]]; then
    echo "  Database URL is stored in:"
    echo "    ${INSTALL_PREFIX}/etc/database.env"
fi
echo "═════════════════════════════════════════════════════════════════"

# DISTRO NOTES:
# - This script targets Ubuntu/Debian. On RHEL/Fedora/AlmaLinux it falls back
#   to dnf and the postgresql initdb step. Service unit names match upstream.
# - For Alpine, you would need to swap apk in and write OpenRC scripts instead
#   of the systemd unit; not covered here.
# - For Arch, replace apt-get with pacman and Postgres init is manual.
