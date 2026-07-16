#!/usr/bin/env bash
# verify-v23.1.sh — one-shot capability check for gen-system v23.1.
# What it does, in order, stopping at the first hard failure:
#   1. sanity: Go toolchain, project layout, single-dep rule
#   2. build:  gen, gen-server, genbench  +  go vet
#   3. studio: confirm studio-v23.html carries the v23.1 additions
#   4. serve:  boot gen-server on the COBOL demo, probe v23 endpoints + UI
#   5. bench:  run genbench offline (no AI) for a first baseline number
#   6. idea:   (optional, needs Ollama) plain-language idea → generated project
#
# Run from the project root (the dir holding go.mod):
#   chmod +x verify-v23.1.sh
#   ./verify-v23.1.sh                 # steps 1–5 (offline, no model needed)
#   ./verify-v23.1.sh --with-idea     # also step 6 (needs Ollama running)
#
# Nothing here writes outside the repo except a temp gen-server log in /tmp
# and, for step 6, a fresh project under ./_verify_idea_app/.

set -uo pipefail

# ----- config knobs (override via env) ---------------------------------------
PORT="${GEN_PORT:-28080}"
DEMO="${GEN_DEMO:-./demo-cobol}"
COPYBOOKS="${GEN_COPYBOOKS:-./demo-cobol/copybooks}"
OLLAMA_HOST="${OLLAMA_HOST:-http://localhost:11434}"
IDEA_TEXT="${GEN_IDEA:-A task tracker: users create projects, each project has tasks with a title, status, and due date.}"
IDEA_DIR="./_verify_idea_app"

WITH_IDEA=0
[ "${1:-}" = "--with-idea" ] && WITH_IDEA=1

# ----- pretty output ---------------------------------------------------------
b=$(tput bold 2>/dev/null || true); n=$(tput sgr0 2>/dev/null || true)
g=$(tput setaf 2 2>/dev/null || true); r=$(tput setaf 1 2>/dev/null || true)
y=$(tput setaf 3 2>/dev/null || true); c=$(tput setaf 6 2>/dev/null || true)
pass(){ echo "${g}  ✓${n} $*"; }
warn(){ echo "${y}  ‼${n} $*"; }
fail(){ echo "${r}  ✗${n} $*"; }
hdr(){  echo; echo "${b}${c}== $* ==${n}"; }
die(){  fail "$*"; cleanup; exit 1; }

SRV_PID=""
cleanup(){ [ -n "$SRV_PID" ] && kill "$SRV_PID" 2>/dev/null; }
trap cleanup EXIT

# =============================================================================
hdr "1. Sanity"
[ -f go.mod ] || die "no go.mod here — run from the project root."
command -v go >/dev/null 2>&1 || die "Go toolchain not on PATH."
GOVER=$(go version | awk '{print $3}')
pass "Go present: $GOVER  (project targets go 1.25.0)"
DEPS=$(go list -m all 2>/dev/null | grep -v '^gen-system' | grep -c . || true)
echo "    external module lines: $DEPS  (expected: just github.com/lib/pq)"
go list -m all 2>/dev/null | grep -q 'github.com/lib/pq' \
  && pass "single external dep github.com/lib/pq present" \
  || warn "lib/pq not found in module graph — check go.mod"
[ -f web/studio-v23.html ] || die "web/studio-v23.html missing."
pass "project layout looks right"

# =============================================================================
hdr "2. Build — gen, gen-server, genbench + vet"
for cmd in gen gen-server genbench; do
  if go build -o "/tmp/gen_verify_$cmd" "./generator/cmd/$cmd" 2>/tmp/gen_build_err; then
    pass "go build ./generator/cmd/$cmd"
  else
    cat /tmp/gen_build_err
    die "build failed: ./generator/cmd/$cmd"
  fi
done
if go vet ./generator/... 2>/tmp/gen_vet_err; then
  pass "go vet ./generator/... clean"
else
  cat /tmp/gen_vet_err
  warn "go vet reported issues (see above) — not fatal to the demo"
fi
GEN="/tmp/gen_verify_gen"
GENSRV="/tmp/gen_verify_gen-server"
GENBENCH="/tmp/gen_verify_genbench"

# =============================================================================
hdr "3. Studio v23.1 — confirm the new UI code is present"
H=web/studio-v23.html
check(){ grep -q -- "$1" "$H" && pass "$2" || fail "MISSING: $2"; }
check "v23.1"                 "version marker bumped to v23.1"
check "function attachZoom"   "call-graph zoom/pan: attachZoom() defined"
check "zoom-controls"         "call-graph zoom: +/−/reset control panel"
check "'viewport'"            "call-graph zoom: transformable viewport layer"
check "settings-pop"          "Display Settings popup present"
check "fz-scale"              "text-size scaling variable wired"
check 'data-theme="light"'    "readable light (paper) theme defined"
check "text-dim:#8893a6"      "dark --text-dim lifted for legibility"

# =============================================================================
hdr "4. Serve — boot on the COBOL demo, probe endpoints"
if [ ! -d "$DEMO" ]; then
  warn "demo dir $DEMO not found — skipping live server probe"
else
  "$GENSRV" --port "$PORT" --root "$DEMO" --copybooks "$COPYBOOKS" \
      >/tmp/gen_verify_server.log 2>&1 &
  SRV_PID=$!
  echo "    gen-server pid $SRV_PID on :$PORT (root=$DEMO)"
  # wait up to ~8s for it to answer
  up=0
  for i in $(seq 1 40); do
    if curl -fsS "http://localhost:$PORT/studio-v23.html" >/dev/null 2>&1; then up=1; break; fi
    sleep 0.2
  done
  if [ "$up" != 1 ]; then
    tail -20 /tmp/gen_verify_server.log
    die "server did not come up on :$PORT"
  fi
  pass "server up — http://localhost:$PORT/studio-v23.html"

  probe(){ # path  label
    code=$(curl -s -o /tmp/gen_probe_body -w '%{http_code}' "http://localhost:$PORT$1")
    if [ "$code" = "200" ]; then
      pass "$2  (200, $(wc -c </tmp/gen_probe_body) bytes)"
    else
      fail "$2  (HTTP $code)"
    fi
  }
  probe "/studio-v23.html"                              "UI: studio-v23.html served"
  probe "/api/present/v23/architecture"                "v23: GET /architecture"
  probe "/api/present/v23/effects"                     "v23: GET /effects"
  probe "/api/present/v23/search?q=&limit=5"           "v23: GET /search"
  # pull one real symbol id from search, then exercise callgraph (zoom data source)
  ROOT=$(curl -s "http://localhost:$PORT/api/present/v23/search?q=&limit=1" \
         | grep -o '"symbol_id":"[^"]*"' | head -1 | sed 's/.*:"//;s/"//')
  if [ -n "$ROOT" ]; then
    echo "    sample root symbol: $ROOT"
    probe "/api/present/v23/callgraph?root=$(printf %s "$ROOT" | sed 's/ /%20/g')&depth=2&direction=callees" \
          "v23: GET /callgraph (feeds the zoomable graph)"
  else
    warn "no symbols returned by search — callgraph probe skipped"
  fi
  echo
  echo "    ${b}Open in a browser and eyeball v23.1:${n}"
  echo "      http://localhost:$PORT/studio-v23.html"
  echo "      • press ${b}g g${n} → ⌘K → pick a symbol → graph renders"
  echo "      • mouse-wheel over the graph to zoom; drag to pan; ⊡ to reset"
  echo "      • click ${b}⚙${n} (top-right) → slide text size, toggle Dark/Light"
fi

# =============================================================================
hdr "5. genbench — offline baseline (no AI)"
# Static suites (resolution/cfg/effects/diff/parse/loadctx/incremental) need no model.
if "$GENBENCH" all --skip-ai --root "$DEMO" >/tmp/gen_verify_bench.log 2>&1 \
   || "$GENBENCH" all --root "$DEMO" >/tmp/gen_verify_bench.log 2>&1; then
  pass "genbench ran — see /tmp/gen_verify_bench.log"
  grep -Ei 'resolution|symbols|pass|score|p50|p95|ok' /tmp/gen_verify_bench.log | head -12 | sed 's/^/      /'
else
  warn "genbench exited non-zero (model suites may need Ollama) — log: /tmp/gen_verify_bench.log"
  tail -8 /tmp/gen_verify_bench.log | sed 's/^/      /'
fi

# =============================================================================
hdr "6. Idea → project (plain language)  [optional]"
if [ "$WITH_IDEA" != 1 ]; then
  echo "    skipped. Re-run with ${b}--with-idea${n} once Ollama is up to test generation."
else
  if ! curl -fsS "$OLLAMA_HOST/api/tags" >/dev/null 2>&1; then
    warn "Ollama not reachable at $OLLAMA_HOST — start it: ${b}ollama serve${n}"
    warn "and pull the model: ${b}ollama pull deepseek-coder-v2:16b-lite-instruct-q5_K_M${n}"
  else
    pass "Ollama reachable at $OLLAMA_HOST"
    rm -rf "$IDEA_DIR"; mkdir -p "$IDEA_DIR"
    echo "    idea: \"$IDEA_TEXT\""
    echo "    generating into $IDEA_DIR ..."
    # plan first (review artifact), then run into the target dir
    ( cd "$IDEA_DIR" && "$GEN" plan "$IDEA_TEXT" ) 2>&1 | sed 's/^/      /'
    if [ -f "$IDEA_DIR/plan.generated.json" ]; then
      pass "plan.generated.json written"
      ( cd "$IDEA_DIR" && "$GEN" run --plan plan.generated.json --path . ) 2>&1 | sed 's/^/      /'
      echo "    generated tree:"
      find "$IDEA_DIR" -maxdepth 2 -type d | sed 's/^/        /' | head -20
    else
      warn "no plan produced — inspect model output above"
    fi
  fi
fi

hdr "Done"
echo "  Build + UI checks are the gate. Steps 5–6 give you the numbers and"
echo "  the demo a design partner will want to see. Server log (if started):"
echo "    /tmp/gen_verify_server.log"
echo
