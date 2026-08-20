#!/usr/bin/env bash
#
# build.sh — build gen-system. Two products from one tree.
#
# Modes:
#   ./build.sh                 default = --internal (everything)
#   ./build.sh --understand    ENTERPRISE: build only understand-server + genbench,
#                              then run the architecture boundary test that proves
#                              the enterprise binary links NO generation/model code.
#   ./build.sh --internal      FULL: gen + gen-server + understand-server (+ vet)
#
# Flags (any mode):
#   --run                      after building, start the server
#   --run --root DIR --port N --copybooks DIR --browse-root DIR
#   --with-bench               also build genbench (implied by --understand)
#   --test                     also run the test suite
#   --clean                    remove built binaries first
#
# --run starts:
#   • understand-server   in --understand mode (enterprise Studio, offline)
#   • gen-server          otherwise (all-in-one dev server)
#
# Run this from the repository root (the dir containing go.mod).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

for a in "$@"; do
  if [[ "$a" == "-h" || "$a" == "--help" ]]; then
    grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0
  fi
done

if [[ ! -f go.mod ]]; then
  echo "✗ go.mod not found in $SCRIPT_DIR"
  echo "  Run build.sh from the gen-system repository root."
  exit 1
fi

# ── parse args ────────────────────────────────────────────────
MODE="internal"   # internal | understand
DO_RUN=0; DO_BENCH=0; DO_TEST=0; DO_CLEAN=0
ROOT="."; PORT="28080"; COPYBOOKS=""; BROWSE_ROOT=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --understand)  MODE="understand"; DO_BENCH=1; shift ;;
    --internal)    MODE="internal"; shift ;;
    --run)         DO_RUN=1; shift ;;
    --with-bench)  DO_BENCH=1; shift ;;
    --test)        DO_TEST=1; shift ;;
    --clean)       DO_CLEAN=1; shift ;;
    --root)        ROOT="$2"; shift 2 ;;
    --port)        PORT="$2"; shift 2 ;;
    --copybooks)   COPYBOOKS="$2"; shift 2 ;;
    --browse-root) BROWSE_ROOT="$2"; shift 2 ;;
    -h|--help)     grep '^#' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown arg: $1"; exit 1 ;;
  esac
done

if ! command -v go >/dev/null 2>&1; then
  echo "✗ Go is not installed or not on PATH."
  echo "  Install Go 1.25+ (https://go.dev/dl/) then re-run."
  exit 1
fi
echo "→ Go: $(go version)"
echo "→ Mode: $MODE"

if [[ $DO_CLEAN -eq 1 ]]; then
  echo "→ Cleaning old binaries…"
  rm -f gen gen-server understand-server gen-enrich genbench bench
fi

echo "→ Resolving modules…"
go mod download 2>/dev/null || echo "  (go mod download skipped — likely already cached/offline)"

# ── build ─────────────────────────────────────────────────────
BUILT=()
if [[ "$MODE" == "understand" ]]; then
  echo "→ Building understand-server (ENTERPRISE, offline, embedded UI)…"
  go build -o understand-server ./generator/cmd/understand-server
  BUILT+=(understand-server)
else
  echo "→ Building gen-server (all-in-one dev Studio)…"
  go build -o gen-server ./generator/cmd/gen-server
  echo "→ Building understand-server (enterprise binary)…"
  go build -o understand-server ./generator/cmd/understand-server
  echo "→ Building gen (CLI)…"
  go build -o gen ./generator/cmd/gen
  echo "→ Building gen-enrich (belief enrichment producer, generation-side)…"
  go build -o gen-enrich ./generator/cmd/gen-enrich
  BUILT+=(gen gen-server understand-server gen-enrich)
fi

if [[ $DO_BENCH -eq 1 ]]; then
  echo "→ Building genbench…"
  go build -o genbench ./generator/cmd/genbench
  BUILT+=(genbench)
fi

# ── vet ───────────────────────────────────────────────────────
echo "→ go vet ./generator/… (should be silent)"
if go vet ./generator/... ; then echo "  vet clean."; else echo "  ⚠ vet reported issues above."; fi

# ── enterprise guarantee: prove no generation/model code is linked ───
if [[ "$MODE" == "understand" ]]; then
  echo "→ Architecture guardrail: understand-server must link NO generation/model code…"
  if go test ./generator/internal/archtest/ -run TestUnderstandServerHasNoGenDeps ; then
    echo "  ✓ enterprise binary is generation-free (air-gapped by construction)."
  else
    echo "  ✗ FAIL: generation/model code leaked into the enterprise binary (see above)."
    exit 1
  fi
fi

# ── tests (optional) ──────────────────────────────────────────
if [[ $DO_TEST -eq 1 ]]; then
  echo "→ Running tests…"
  go test ./generator/... || echo "  ⚠ some tests failed (see above)."
fi

echo ""
echo "✓ Build complete. Binaries in $SCRIPT_DIR:"
printf '    %s\n' "${BUILT[@]}"

# ── run (optional) ────────────────────────────────────────────
if [[ $DO_RUN -eq 1 ]]; then
  ARGS=(--root "$ROOT" --port "$PORT")
  [[ -n "$COPYBOOKS"   ]] && ARGS+=(--copybooks "$COPYBOOKS")
  [[ -n "$BROWSE_ROOT" ]] && ARGS+=(--browse-root "$BROWSE_ROOT")
  if [[ "$MODE" == "understand" ]]; then
    echo ""; echo "→ Starting enterprise Studio:  ./understand-server ${ARGS[*]}"
    echo "  Open http://localhost:$PORT/"; echo ""
    exec ./understand-server "${ARGS[@]}"
  else
    echo ""; echo "→ Starting dev Studio:  ./gen-server ${ARGS[*]}"
    echo "  Open http://localhost:$PORT/studio-v23.html"; echo ""
    exec ./gen-server "${ARGS[@]}"
  fi
else
  echo ""
  echo "Next:"
  if [[ "$MODE" == "understand" ]]; then
    echo "  ./understand-server --root /path/to/legacy --copybooks /path/to/cpy"
    echo "  then open http://localhost:28080/"
  else
    echo "  ./build.sh --understand        # build the shippable enterprise binary"
    echo "  ./gen-server --root . --port 28080   # dev all-in-one"
  fi
fi
