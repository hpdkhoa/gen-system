// generator/internal/engine/ship_scripts.go — v23.10 (Phase 4-B): writes the
// build + bundle scripts INTO the generated project. These produce a deployable
// ARTIFACT only — there is NO deploy/transport step (that is a separate future
// phase; see CHECKPOINT). Target: Ubuntu 24.04 LTS, linux/amd64. Version comes
// from `--version <v>` or a VERSION file (air-gapped boxes often lack git).
//
// build-all.sh : backend static binary (CGO_ENABLED=0 GOOS=linux GOARCH=amd64);
//                web (npm ci && npm run build) and mobile (npm ci) IF those dirs
//                exist. Postgres migrations are embedded/auto-applied by the
//                backend, so nothing extra is needed for them.
// bundle.sh    : assembles <app>-<version>-linux-amd64.tar.gz = backend binary +
//                web build + mobile bundle + migrations + .env.example +
//                systemd unit + run.sh.
package engine

import (
	"path/filepath"
)

func writeShipScripts(appName, path string) {
	if appName == "" {
		appName = "app"
	}
	writeFileMk(filepath.Join(path, "VERSION"), "0.1.0\n")
	writeFileMk(filepath.Join(path, "build-all.sh"), buildAllSh)
	writeFileMk(filepath.Join(path, "bundle.sh"), bundleShFor(appName))
	writeFileMk(filepath.Join(path, ".env.example"), envExample)
	writeFileMk(filepath.Join(path, "deploy/"+appName+".service"), systemdUnitFor(appName))
	writeFileMk(filepath.Join(path, "deploy/run.sh"), runSh)
}

const buildAllSh = `#!/usr/bin/env bash
# build-all.sh — build every layer of this generated product.
# Target: Ubuntu 24.04 LTS, linux/amd64. Produces artifacts under ./dist/.
# This script BUILDS only. It does not deploy anything.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

DIST="dist"
mkdir -p "$DIST"

echo "→ Backend (Go, static linux/amd64)…"
( cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "../$DIST/server" . )
echo "  built $DIST/server"

if [ -d web ] && [ -f web/package.json ]; then
  echo "→ Web (ReactJS)…"
  ( cd web && npm ci && npm run build )
  # CRA->build, Vite->dist; copy whichever exists.
  if [ -d web/build ]; then cp -r web/build "$DIST/web"; elif [ -d web/dist ]; then cp -r web/dist "$DIST/web"; fi
  echo "  built $DIST/web"
else
  echo "→ Web: skipped (no web/ — experimental layer, not generated this run)"
fi

if [ -d mobile ] && [ -f mobile/package.json ]; then
  echo "→ Mobile (React Native) deps + JS bundle…"
  ( cd mobile && npm ci )
  if [ -f mobile/index.js ]; then
    ( cd mobile && npx react-native bundle --platform android --dev false \
        --entry-file index.js --bundle-output "../$DIST/mobile.android.bundle" 2>/dev/null ) || \
        echo "  (RN bundle skipped — configure entry/platform on your box)"
  fi
  echo "  mobile deps installed"
else
  echo "→ Mobile: skipped (no mobile/ — experimental layer, not generated this run)"
fi

echo "✓ build-all complete. Artifacts in ./$DIST/"
`

func bundleShFor(app string) string {
	return `#!/usr/bin/env bash
# bundle.sh — assemble a single deployable tarball from ./dist/.
# Usage: ./bundle.sh [--version X.Y.Z]
# Version resolution: --version flag, else the VERSION file, else 0.0.0.
# Produces ` + app + `-<version>-linux-amd64.tar.gz. Build only; no deploy.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

APP="` + app + `"
VERSION=""
while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --version=*) VERSION="${1#*=}"; shift ;;
    *) echo "unknown arg: $1"; exit 1 ;;
  esac
done
if [ -z "$VERSION" ]; then
  if [ -f VERSION ]; then VERSION="$(tr -d '[:space:]' < VERSION)"; else VERSION="0.0.0"; fi
fi

if [ ! -f dist/server ]; then
  echo "✗ dist/server not found — run ./build-all.sh first." >&2
  exit 1
fi

STAGE="$(mktemp -d)"
ROOT="$STAGE/$APP"
mkdir -p "$ROOT"
cp dist/server "$ROOT/server"
[ -d dist/web ] && cp -r dist/web "$ROOT/web" || true
[ -f dist/mobile.android.bundle ] && cp dist/mobile.android.bundle "$ROOT/" || true
# Postgres migrations are embedded in the binary; copy the SQL too for reference.
[ -d backend/internal/db/migrations ] && { mkdir -p "$ROOT/migrations"; cp backend/internal/db/migrations/*.sql "$ROOT/migrations/" 2>/dev/null || true; }
[ -f .env.example ] && cp .env.example "$ROOT/" || true
[ -d deploy ] && cp -r deploy "$ROOT/deploy" || true

TARBALL="$APP-$VERSION-linux-amd64.tar.gz"
tar -C "$STAGE" -czf "$TARBALL" "$APP"
rm -rf "$STAGE"
echo "✓ bundled $TARBALL"
echo "  contents: server binary + web (if built) + mobile bundle (if built) + migrations + .env.example + deploy/"
echo "  NOTE: this is the artifact only. Deploying it to a host is a separate step."
`
}

const envExample = `# Copy to .env and edit. The backend defaults to the in-memory store.
# Set STORE_BACKEND=postgres + DATABASE_URL to use Postgres.
STORE_BACKEND=memory
# DATABASE_URL=postgres://user:pass@localhost:5432/app?sslmode=disable
# PORT=8080
`

func systemdUnitFor(app string) string {
	return `[Unit]
Description=` + app + ` (gen-system generated service)
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/` + app + `
ExecStart=/opt/` + app + `/server
Restart=on-failure
EnvironmentFile=-/opt/` + app + `/.env

[Install]
WantedBy=multi-user.target
`
}

const runSh = `#!/usr/bin/env bash
# run.sh — start the server from an unpacked bundle (no systemd needed).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
[ -f .env ] && set -a && . ./.env && set +a || true
exec ./server
`
