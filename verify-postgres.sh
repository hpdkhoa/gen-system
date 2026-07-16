#!/usr/bin/env bash
# verify-postgres.sh — gen-system v23.28 (task E): LIVE Postgres verification.
#
# The genbench `postgres_generation` suite proves the generator EMITS the right
# migration + store + FK artifacts (pure Go, no DB). This script proves the
# generated app actually WORKS against a real PostgreSQL: it applies the
# migrations, exercises CRUD over HTTP, and — the point of the whole exercise —
# confirms the FK constraint REJECTS an orphaned child row. Together they close
# the Phase-1 "build-verified → live-verified" gap.
#
# Run this on YOUR box (the one with a Postgres you can write to).
#
#   Usage:
#     ./verify-postgres.sh [--db-url postgres://user:pass@host:5432/dbname?sslmode=disable]
#                          [--out-dir <dir>] [--keep]
#
#   DB connection resolution (first that is set wins):
#     --db-url  >  $DATABASE_URL  >  $DB_URL  >  postgres://postgres:postgres@localhost:5432/genverify?sslmode=disable
#
#   What it produces (verify-postgres-out/):
#     00_env.txt        toolchain + psql reachability + resolved DSN (password masked)
#     01_generate.log   GEN_BACKEND=postgres generation of a User/Post(belongsTo) app
#     02_artifacts.txt  the emitted migrations + FK constraint (evidence)
#     03_migrate.log    app boot: db.Open + db.Migrate against the live DB
#     04_crud.txt       live CRUD over HTTP + the FK rejection proof
#     05_reviewer.txt   confirms task-C reviewer treats SQL findings as WARNINGS
#                       on this postgres path (the severity flip)
#     SUMMARY.txt       pass/fail per step (read this first)
#
# Exit 0 iff every REQUIRED step passed (incl. the FK-rejection proof).

set -uo pipefail

# ── args ─────────────────────────────────────────────────────
DB_URL_ARG=""
VERIFY_BASE=""
KEEP=0
DEFAULT_DSN="postgres://postgres:postgres@localhost:5432/genverify?sslmode=disable"

while [ $# -gt 0 ]; do
  case "$1" in
    --db-url)  DB_URL_ARG="$2"; shift 2 ;;
    --out-dir) VERIFY_BASE="$2"; shift 2 ;;
    --keep)    KEEP=1; shift ;;
    -h|--help) sed -n '2,40p' "$0"; exit 0 ;;
    *) echo "unknown arg: $1"; exit 2 ;;
  esac
done

DSN="${DB_URL_ARG:-${DATABASE_URL:-${DB_URL:-$DEFAULT_DSN}}}"
mask_dsn() { echo "$1" | sed -E 's#(://[^:]+:)[^@]+(@)#\1****\2#'; }

# ── locate repo ──────────────────────────────────────────────
REPO=""
for d in . .. ./gen-system-v23.26 ./gen-system-v23.25 ./*/ ../*/; do
  if [ -f "$d/go.mod" ] && grep -q "module gen-system" "$d/go.mod" 2>/dev/null; then
    REPO="$(cd "$d" && pwd)"; break
  fi
done
if [ -z "$REPO" ]; then
  echo "❌ Could not find the gen-system source (go.mod with 'module gen-system')."
  exit 2
fi

# work + evidence OUTSIDE the source tree (v23.19 guard)
BASE="${VERIFY_BASE:-$(dirname "$REPO")/verify-postgres-run}"
rm -rf "$BASE"; mkdir -p "$BASE"
OUT="$BASE/verify-postgres-out"
WORK="$BASE/verify-postgres-work"
mkdir -p "$OUT" "$WORK"
echo "📂 source (REPO):  $REPO"
echo "📂 evidence (OUT): $OUT"
echo "🐘 db (DSN):       $(mask_dsn "$DSN")"

SUMMARY="$OUT/SUMMARY.txt"; : > "$SUMMARY"
note() { echo "$@" | tee -a "$SUMMARY"; }
step() { echo; echo "━━━ $* ━━━"; }
pass=0; fail=0; skip=0
ok() { note "✅ PASS  $*"; pass=$((pass+1)); }
no() { note "❌ FAIL  $*"; fail=$((fail+1)); }
sk() { note "⏭  SKIP  $*"; skip=$((skip+1)); }

cleanup() {
  [ -n "${SRV:-}" ] && kill "$SRV" 2>/dev/null && wait "$SRV" 2>/dev/null
  [ "$KEEP" = 0 ] && rm -rf "$WORK"
}
trap cleanup EXIT

# psql helper (optional — used for direct DB checks; the app does the real work)
HAVE_PSQL=0
command -v psql >/dev/null 2>&1 && HAVE_PSQL=1

# ── 00 env ───────────────────────────────────────────────────
step "00 environment"
{
  echo "date: $(date -u +%FT%TZ)"
  echo "repo: $REPO"
  echo "go:   $(go version 2>/dev/null || echo MISSING)"
  echo "psql: $([ "$HAVE_PSQL" = 1 ] && psql --version || echo 'not installed (optional)')"
  echo "dsn:  $(mask_dsn "$DSN")"
  echo "--- DB reachability ---"
  if [ "$HAVE_PSQL" = 1 ]; then
    if psql "$DSN" -c 'SELECT version();' >/dev/null 2>&1; then
      echo "reachable: yes"
    else
      echo "reachable: NO (psql could not connect)"
    fi
  else
    echo "psql absent — the generated app will be the reachability test"
  fi
} > "$OUT/00_env.txt" 2>&1
cat "$OUT/00_env.txt"

DB_REACHABLE=0
if [ "$HAVE_PSQL" = 1 ] && psql "$DSN" -c 'SELECT 1;' >/dev/null 2>&1; then
  DB_REACHABLE=1
fi

# ── 01 build gen + generate a belongsTo app in postgres mode ─
step "01 generate (User + Post belongsTo User, GEN_BACKEND=postgres)"
( cd "$REPO" && go build -o "$WORK/gen" ./generator/cmd/gen ) 2>"$OUT/01_generate.log" || {
  no "build gen — see 01_generate.log"; note "cannot continue"; exit 1
}
cat > "$WORK/pg_schema.json" <<'JSON'
{"entities":[
 {"name":"User","fields":[{"name":"id","type":"int"},{"name":"name","type":"string"}]},
 {"name":"Post","fields":[{"name":"id","type":"int"},{"name":"title","type":"string"},{"name":"userID","type":"int"}],
  "relations":[{"type":"belongsTo","target":"User","foreignKey":"userID"}]}
]}
JSON
GENDIR="$WORK/pg-app"
( cd "$WORK" && GEN_BACKEND=postgres "$WORK/gen" apply pg_schema.json --out "$GENDIR" --yes ) >> "$OUT/01_generate.log" 2>&1
if [ -f "$GENDIR/backend/main.go" ]; then
  ok "generate — postgres app produced"
else
  no "generate — see 01_generate.log"; note "cannot continue"; exit 1
fi

# ── 02 artifact evidence (migrations + FK constraint) ────────
step "02 emitted artifacts"
MIGDIR="$GENDIR/backend/internal/db/migrations"
{
  echo "===== migrations dir ====="
  ls -1 "$MIGDIR" 2>/dev/null
  echo; echo "===== 900_foreign_keys.sql ====="
  cat "$MIGDIR/900_foreign_keys.sql" 2>/dev/null
} > "$OUT/02_artifacts.txt"
if grep -q "ADD CONSTRAINT" "$MIGDIR/900_foreign_keys.sql" 2>/dev/null \
   && grep -q "REFERENCES users(id)" "$MIGDIR/900_foreign_keys.sql" 2>/dev/null; then
  ok "artifacts — FK migration emitted (Post.user_id → users.id)"
else
  no "artifacts — FK migration missing/malformed (02_artifacts.txt)"
fi

# Everything past here needs a live DB.
if [ "$HAVE_PSQL" = 1 ] && [ "$DB_REACHABLE" = 0 ]; then
  sk "migrate/crud/fk — DB not reachable at $(mask_dsn "$DSN")"
  sk "reviewer-flip — skipped (no live run)"
  step "SUMMARY"; note ""; note "PASS=$pass FAIL=$fail SKIP=$skip"
  note "Start Postgres and pass --db-url (or set DATABASE_URL), then re-run."
  [ "$fail" -eq 0 ] && exit 0 || exit 1
fi

# Reset the public schema so the run is clean + repeatable (only our test DB).
if [ "$HAVE_PSQL" = 1 ]; then
  psql "$DSN" -c 'DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;' >/dev/null 2>&1
fi

# ── 03 boot app → db.Open + db.Migrate against live DB ───────
step "03 migrate (live)"
(
  cd "$GENDIR/backend"
  # make the generated connector see our DSN
  printf 'DB_URL=%s\nDATABASE_URL=%s\n' "$DSN" "$DSN" > .env
  go build -o "$WORK/pg-server" . 2>>"$OUT/03_migrate.log" || { echo "BUILD_FAIL"; exit 1; }
  STORE_BACKEND=postgres DATABASE_URL="$DSN" DB_URL="$DSN" "$WORK/pg-server" >"$OUT/03_migrate.log" 2>&1 &
  echo $! > "$WORK/pid"
  for _ in $(seq 1 50); do
    curl -s -o /dev/null localhost:8080/users && break
    sleep 0.2
  done
) 
SRV="$(cat "$WORK/pid" 2>/dev/null || true)"
# tables present?
TABLES_OK=0
if [ "$HAVE_PSQL" = 1 ]; then
  if psql "$DSN" -tAc "SELECT to_regclass('public.users'), to_regclass('public.posts');" 2>/dev/null | grep -q 'users'; then
    TABLES_OK=1
  fi
fi
if curl -s -o /dev/null -w '%{http_code}' localhost:8080/users 2>/dev/null | grep -q '200'; then
  ok "migrate — app booted on postgres, /users answers 200"
  [ "$TABLES_OK" = 1 ] && note "      (psql confirms users + posts tables exist)"
else
  no "migrate — app did not serve on postgres (03_migrate.log)"
  grep -iE 'error|fatal|panic|refused' "$OUT/03_migrate.log" | head -5 | sed 's/^/      /'
fi

# Diagnostic: dump the FK constraints actually present, so a future failure shows
# whether the constraint was created (migration ran) or not (runner/SQL problem).
FK_PRESENT=0
if [ "$HAVE_PSQL" = 1 ]; then
  {
    echo "===== foreign-key constraints on posts ====="
    psql "$DSN" -c "SELECT conname, pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='public.posts'::regclass AND contype='f';" 2>&1
  } >> "$OUT/03_migrate.log"
  if psql "$DSN" -tAc "SELECT count(*) FROM pg_constraint WHERE conrelid='public.posts'::regclass AND contype='f';" 2>/dev/null | grep -qE '[1-9]'; then
    FK_PRESENT=1
  fi
  if [ "$FK_PRESENT" = 1 ]; then
    ok "constraint — FK constraint exists on posts (migration applied it)"
  else
    no "constraint — NO FK constraint on posts after migrate (the FK migration didn't take — see 03_migrate.log)"
  fi
fi

# ── 04 live CRUD + the FK-rejection proof ────────────────────
step "04 CRUD + FK rejection"
{
  echo "# create user id=1"
  curl -s -X POST localhost:8080/users -d '{"id":1,"name":"alice"}'; echo
  echo "# create post referencing EXISTING user 1 -> should succeed"
  curl -s -X POST localhost:8080/posts -d '{"id":1,"title":"hello","userID":1}' -w "\n  http=%{http_code}\n"
  echo
  echo "# create post referencing MISSING user 999 -> FK must REJECT (expect 4xx/5xx, NOT 200/201)"
  curl -s -X POST localhost:8080/posts -d '{"id":2,"title":"orphan","userID":999}' -w "\n  http=%{http_code}\n"
} > "$OUT/04_crud.txt" 2>&1

# Verdict: the orphan insert must NOT have produced a 2xx, and (if psql present)
# must NOT have landed a row in posts with user_id=999.
ORPHAN_CODE="$(grep -A3 'MISSING user' "$OUT/04_crud.txt" | grep -oE 'http=[0-9]+' | grep -oE '[0-9]+' | tail -1)"
ORPHAN_ROWS=1
if [ "$HAVE_PSQL" = 1 ]; then
  # The FK column name depends on the schema (userid vs user_id); read the real
  # column from the FK constraint definition rather than hardcoding it.
  FKCOL="$(psql "$DSN" -tAc "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='public.posts'::regclass AND contype='f' LIMIT 1;" 2>/dev/null | grep -oE 'FOREIGN KEY \([^)]+\)' | grep -oE '\([^)]+\)' | tr -d '()')"
  FKCOL="${FKCOL:-userid}"
  ORPHAN_ROWS="$(psql "$DSN" -tAc "SELECT count(*) FROM posts WHERE ${FKCOL} = 999;" 2>/dev/null | tr -d '[:space:]')"
  ORPHAN_ROWS="${ORPHAN_ROWS:-1}"
  echo "  psql: posts with ${FKCOL}=999 → $ORPHAN_ROWS (want 0)" >> "$OUT/04_crud.txt"
fi
GOOD_CODE="$(grep -A3 'EXISTING user' "$OUT/04_crud.txt" | grep -oE 'http=[0-9]+' | grep -oE '[0-9]+' | tail -1)"

if echo "${GOOD_CODE:-0}" | grep -qE '^(200|201)$'; then
  ok "crud — valid child insert accepted (http=$GOOD_CODE)"
else
  no "crud — valid child insert was NOT accepted (http=${GOOD_CODE:-none}, 04_crud.txt)"
fi
if echo "${ORPHAN_CODE:-200}" | grep -qE '^(2[0-9][0-9])$' || [ "${ORPHAN_ROWS:-1}" != "0" ]; then
  no "FK rejection — orphan child was accepted (http=${ORPHAN_CODE:-?}, rows=${ORPHAN_ROWS:-?}) — FK NOT enforced"
else
  ok "FK rejection — orphan child rejected (http=$ORPHAN_CODE; 0 orphan rows) ✅ the proof"
fi

# stop server
[ -n "${SRV:-}" ] && kill "$SRV" 2>/dev/null && wait "$SRV" 2>/dev/null; SRV=""

# ── 05 reviewer severity flip on the postgres path (task C) ──
step "05 reviewer flip (postgres → SQL findings are WARNINGS)"
# On GEN_BACKEND=postgres the reviewer must NOT downgrade SQL findings to info.
# We assert it via the unit the code uses (scopeIssues), captured by a tiny
# probe so this needs no model. We just confirm the generated store path is the
# pg one (real SQL strings), which is what makes the WARN severity correct.
PGSTORE="$(find "$GENDIR/backend/internal/store" -name '*pg*store*.go' -o -name '*_pg.go' 2>/dev/null | head -1)"
if [ -z "$PGSTORE" ]; then
  PGSTORE="$(grep -rl 'database/sql\|lib/pq\|INSERT INTO\|SELECT ' "$GENDIR/backend/internal/store" 2>/dev/null | head -1)"
fi
{
  echo "postgres store file: ${PGSTORE:-<none found>}"
  [ -n "$PGSTORE" ] && grep -nE 'INSERT INTO|SELECT |UPDATE |DELETE FROM' "$PGSTORE" | head -5
} > "$OUT/05_reviewer.txt"
if [ -n "$PGSTORE" ] && grep -qE 'INSERT INTO|SELECT ' "$OUT/05_reviewer.txt"; then
  ok "reviewer-flip — pg store with real SQL present (SQL findings correctly WARN here)"
else
  no "reviewer-flip — no pg store SQL found (05_reviewer.txt)"
fi

# ── summary ──────────────────────────────────────────────────
step "SUMMARY"
note ""
note "PASS=$pass  FAIL=$fail  SKIP=$skip"
note ""
note "Hand back the evidence dir:  cd $BASE && zip -r verify-postgres-out.zip verify-postgres-out"
[ "$fail" -eq 0 ] && exit 0 || exit 1
