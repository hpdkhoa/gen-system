#!/usr/bin/env bash
# gen-verify.sh — gen-system v23.26 end-to-end verification + benchmark harness.
#
# v23.25 (task D) changes vs the v23.24 script:
#   • Step 05 (continue) now FAILS LOUDLY when the continue TASK reports a
#     failure ("❌ N failed" with N>0, or "N failed" in the agent summary),
#     instead of marking PASS whenever the staged app merely builds. The bare
#     success line "✅ 17 done  ❌ 0 failed" is NOT treated as a failure.
#   • Step 02 (benchmark) now inspects the "TOTAL: P pass, F fail" line and
#     fails when F exceeds the known-pre-existing count (the 3 safety
#     TestValidateBuild_* harness fails), instead of passing just because a
#     JSON file was written.
#   • Optional: when --baseline-check is given and --root is a COBOL corpus,
#     diffs the fresh benchmark against the frozen genbench baseline (task F).
#
# Run this on YOUR box (the one with Ollama + a GPU).
#
#   Usage:
#     ./gen-verify.sh [--root <existing-codebase>] [--copybooks <dir>]
#                     [--no-ollama] [--plan-model M] [--code-model M]
#                     [--baseline-check] [--out-dir <dir>]
#
# Exit code is 0 iff every REQUIRED step passed.

set -uo pipefail

# ─────────────────────────────────────────────────────────────
# args
# ─────────────────────────────────────────────────────────────
ROOT=""
COPYBOOKS=""
USE_OLLAMA=1
BASELINE_CHECK=0
PLAN_MODEL="${OLLAMA_MODEL_PLAN:-qwen3:14b}"
CODE_MODEL="${OLLAMA_MODEL_CODE:-qwen2.5-coder:14b}"
OLLAMA_HOST_DEFAULT="${OLLAMA_HOST:-http://localhost:11434}"
VERIFY_BASE=""

# Known pre-existing benchmark failures that are NOT ours. As of v23.26 the
# postgres_generation fail is FIXED (FK column resolution + store-emit fidelity),
# so genbench `all` is expected fully green: default 0. Override if your tree
# still carries a known fail.
KNOWN_BENCH_FAILS="${GEN_KNOWN_BENCH_FAILS:-0}"

while [ $# -gt 0 ]; do
  case "$1" in
    --root)           ROOT="$2"; shift 2 ;;
    --copybooks)      COPYBOOKS="$2"; shift 2 ;;
    --no-ollama)      USE_OLLAMA=0; shift ;;
    --plan-model)     PLAN_MODEL="$2"; shift 2 ;;
    --code-model)     CODE_MODEL="$2"; shift 2 ;;
    --baseline-check) BASELINE_CHECK=1; shift ;;
    --out-dir)        VERIFY_BASE="$2"; shift 2 ;;
    -h|--help)        sed -n '2,40p' "$0"; exit 0 ;;
    *) echo "unknown arg: $1"; exit 2 ;;
  esac
done

# Resolve repo root = dir containing go.mod with "module gen-system".
REPO=""
for d in . .. ./gen-system-v23.25 ./gen-system-v23.24 ./*/ ../*/; do
  if [ -f "$d/go.mod" ] && grep -q "module gen-system" "$d/go.mod" 2>/dev/null; then
    REPO="$(cd "$d" && pwd)"; break
  fi
done
if [ -z "$REPO" ]; then
  echo "❌ Could not find the gen-system source (go.mod with 'module gen-system')."
  echo "   Run this from the repo root or its parent."
  exit 2
fi

# Place scratch + evidence OUTSIDE the source tree (v23.19 source-tree guard).
BASE="${VERIFY_BASE:-$(dirname "$REPO")/gen-verify-run}"
rm -rf "$BASE"; mkdir -p "$BASE"
OUT="$BASE/gen-verify-out"
WORK="$BASE/gen-verify-work"
mkdir -p "$OUT" "$WORK"
echo "📂 source (REPO):   $REPO"
echo "📂 scratch (WORK):  $WORK   (outside the source tree)"
echo "📂 evidence (OUT):  $OUT"

SUMMARY="$OUT/SUMMARY.txt"
: > "$SUMMARY"
note() { echo "$@" | tee -a "$SUMMARY"; }
step() { echo; echo "━━━ $* ━━━"; }

pass=0; fail=0; skip=0
ok()   { note "✅ PASS  $*"; pass=$((pass+1)); }
no()   { note "❌ FAIL  $*"; fail=$((fail+1)); }
sk()   { note "⏭  SKIP  $*"; skip=$((skip+1)); }

# ─────────────────────────────────────────────────────────────
# 00 — environment
# ─────────────────────────────────────────────────────────────
step "00 environment"
{
  echo "date: $(date -u +%FT%TZ)"
  echo "repo: $REPO"
  echo "go:   $(go version 2>/dev/null || echo MISSING)"
  echo "os:   $(uname -a)"
  echo "--- GPU ---"; (nvidia-smi -L 2>/dev/null || echo "no nvidia-smi")
  echo "--- Ollama ---"
  if [ "$USE_OLLAMA" = 1 ]; then
    curl -fsS "$OLLAMA_HOST_DEFAULT/api/tags" 2>/dev/null | head -c 4000 || echo "ollama not reachable at $OLLAMA_HOST_DEFAULT"
    echo; echo "plan-model: $PLAN_MODEL"; echo "code-model: $CODE_MODEL"
  else
    echo "ollama disabled (--no-ollama)"
  fi
} > "$OUT/00_env.txt" 2>&1
cat "$OUT/00_env.txt"

MODEL_OK=0
if [ "$USE_OLLAMA" = 1 ] && curl -fsS "$OLLAMA_HOST_DEFAULT/api/tags" >/dev/null 2>&1; then
  MODEL_OK=1
fi

# ─────────────────────────────────────────────────────────────
# 01 — build all binaries
# ─────────────────────────────────────────────────────────────
step "01 build"
(
  cd "$REPO"
  echo "building gen, gen-server, genbench, understand-server…"
  go build -o "$WORK/gen"               ./generator/cmd/gen               && echo "  built gen" || exit 1
  go build -o "$WORK/gen-server"        ./generator/cmd/gen-server        && echo "  built gen-server" || exit 1
  go build -o "$WORK/genbench"          ./generator/cmd/genbench          && echo "  built genbench" || exit 1
  go build -o "$WORK/understand-server" ./generator/cmd/understand-server && echo "  built understand-server" || exit 1
  echo "--- vet ---"
  go vet ./generator/... && echo "vet clean"
) > "$OUT/01_build.log" 2>&1
if [ -x "$WORK/gen" ] && [ -x "$WORK/genbench" ] && [ -x "$WORK/understand-server" ]; then
  ok "build — all four binaries"
else
  no "build — see 01_build.log"; note ""; note "Build failed; cannot continue."; exit 1
fi
GEN="$WORK/gen"; GENBENCH="$WORK/genbench"

# ─────────────────────────────────────────────────────────────
# 02 — benchmark (the fundable number)
# ─────────────────────────────────────────────────────────────
step "02 benchmark"
BENCH_ROOT="$ROOT"
if [ -z "$BENCH_ROOT" ]; then
  echo "no --root given; generating a sample app to benchmark against…"
  cat > "$WORK/bench_schema.json" <<'JSON'
{"entities":[
 {"name":"Account","fields":[{"name":"id","type":"int"},{"name":"owner","type":"string"},{"name":"balance","type":"float"}]},
 {"name":"Txn","fields":[{"name":"id","type":"int"},{"name":"amount","type":"float"},{"name":"kind","type":"string"}]}
]}
JSON
  ( cd "$WORK" && "$GEN" apply bench_schema.json --out ./bench-sample >/dev/null 2>&1 )
  BENCH_ROOT="$WORK/bench-sample"
fi
BENCH_ARGS=( all --root "$BENCH_ROOT" --out "$OUT/02_benchmark.json" )
[ -n "$COPYBOOKS" ] && BENCH_ARGS+=( --copybooks "$COPYBOOKS" )
if [ "$MODEL_OK" = 1 ]; then
  BENCH_ARGS+=( --model "$CODE_MODEL" --host "$OLLAMA_HOST_DEFAULT" )
else
  BENCH_ARGS+=( --skip-ai )
fi
"$GENBENCH" "${BENCH_ARGS[@]}" > "$OUT/02_benchmark.txt" 2>&1

# D: don't pass merely because a JSON was written — read the TOTAL line and
# fail when fail-count exceeds the known pre-existing safety harness fails.
BENCH_TOTAL_LINE="$(grep -oE 'TOTAL: [0-9]+ pass, [0-9]+ fail' "$OUT/02_benchmark.txt" | tail -1)"
BENCH_FAILS="$(echo "$BENCH_TOTAL_LINE" | grep -oE '[0-9]+ fail' | grep -oE '[0-9]+')"
BENCH_FAILS="${BENCH_FAILS:-0}"
if [ ! -f "$OUT/02_benchmark.json" ]; then
  no "benchmark — no JSON produced (see 02_benchmark.txt)"
elif [ "$BENCH_FAILS" -gt "$KNOWN_BENCH_FAILS" ]; then
  no "benchmark — $BENCH_TOTAL_LINE (exceeds $KNOWN_BENCH_FAILS known pre-existing fail(s))"
  grep -iE "✗|fail" "$OUT/02_benchmark.txt" | head -10 | sed 's/^/      /'
else
  ok "benchmark — $BENCH_TOTAL_LINE (≤ $KNOWN_BENCH_FAILS known pre-existing)"
  grep -iE "resolution|determinis|recall|pass|score" "$OUT/02_benchmark.txt" | head -8 | sed 's/^/      /'
fi

# 02b (optional, task F): regression-diff a COBOL run against the frozen baseline.
if [ "$BASELINE_CHECK" = 1 ] && [ -n "$ROOT" ]; then
  step "02b baseline regression (frozen COBOL baseline)"
  "$GENBENCH" regression --baseline --new "$OUT/02_benchmark.json" --root "$REPO" \
      > "$OUT/02b_baseline.txt" 2>&1
  if grep -q "OK — no metric regressed" "$OUT/02b_baseline.txt"; then
    ok "baseline — fresh run matches frozen baseline"
  else
    no "baseline — drift vs frozen baseline (see 02b_baseline.txt)"
    grep -E "✗|DRIFT" "$OUT/02b_baseline.txt" | head -8 | sed 's/^/      /'
  fi
fi

# ─────────────────────────────────────────────────────────────
# 03 — generate an app WITH real business logic (the scenario)
# ─────────────────────────────────────────────────────────────
step "03 generate (orders/products scenario)"
IDEA='An orders app. An Order has a total and a status. Customers can check out an order (only if it has a total; mark it paid) and apply a discount percentage to an unpaid order. A Product has a name and stock; staff can adjust stock up or down but it can never go negative.'
GENDIR="$WORK/orders-app"
if [ "$MODEL_OK" = 1 ]; then
  (
    cd "$WORK"
    GEN_LOGIC=ollama OLLAMA_MODEL_PLAN="$PLAN_MODEL" OLLAMA_MODEL_CODE="$CODE_MODEL" \
      "$GEN" idea --v2 --out "$GENDIR" --yes "$IDEA"
  ) > "$OUT/03_generate.log" 2>&1
else
  cat > "$WORK/orders_schema.json" <<'JSON'
{"entities":[
 {"name":"Order","fields":[{"name":"id","type":"int"},{"name":"total","type":"float"},{"name":"status","type":"string"}]},
 {"name":"Product","fields":[{"name":"id","type":"int"},{"name":"name","type":"string"},{"name":"stock","type":"int"}]}
]}
JSON
  ( cd "$WORK" && "$GEN" apply orders_schema.json --out "$GENDIR" --yes ) > "$OUT/03_generate.log" 2>&1
  echo "(offline: generated from schema via gen apply; service logic = built-in templates)" >> "$OUT/03_generate.log"
fi
if [ -f "$GENDIR/backend/main.go" ]; then
  ok "generate — app produced"
  ( cd "$GENDIR" && find backend/internal -name '*.go' | sort ) > "$OUT/03_app_tree.txt"
  echo "--- service methods (REAL = no 'not implemented') ---" >> "$OUT/03_app_tree.txt"
  grep -rL "not implemented" "$GENDIR"/backend/internal/service/*_service.go 2>/dev/null \
     | sed 's|.*/|  real: |' >> "$OUT/03_app_tree.txt"
  grep -rl "not implemented" "$GENDIR"/backend/internal/service/*_service.go 2>/dev/null \
     | sed 's|.*/|  STUB: |' >> "$OUT/03_app_tree.txt"
else
  no "generate — see 03_generate.log"
fi

# ─────────────────────────────────────────────────────────────
# 04 — run it and prove the business rules over HTTP
# ─────────────────────────────────────────────────────────────
step "04 run + curl"
if [ -f "$GENDIR/backend/main.go" ]; then
  {
    echo "===== operations the model actually generated ====="
    for sh in "$GENDIR"/backend/internal/handler/*_service_handler.go; do
      [ -f "$sh" ] || continue
      ent=$(basename "$sh" | sed 's/_service_handler.go//')
      cases=$(grep -oE 'case "[^"]+"' "$sh" | sed 's/case "//;s/"//' | paste -sd, -)
      echo "  ${ent}: ${cases:-(none)}"
    done
    echo; echo "(routes below are the discovered ops, not guesses)"; echo
  } > "$OUT/04_run_curl.txt"

  (
    cd "$GENDIR/backend"
    go build -o "$WORK/orders-server" . 2>>"$OUT/04_run_curl.txt" || { echo "build of generated app failed"; exit 1; }
    "$WORK/orders-server" >/dev/null 2>&1 &
    SRV=$!
    for _ in $(seq 1 50); do
      curl -s -o /dev/null localhost:8080/orders && break
      sleep 0.2
    done
    find_op() { grep -oE 'case "[^"]+"' "$1" 2>/dev/null | sed 's/case "//;s/"//' | grep -i "$2" | head -1; }
    op_key() {
      awk -v m="func (s \\*.*Service) $2(" 'index($0,"Service) '"$2"'(")>0{f=1} f&&/in\[/{print; exit}' "$1" 2>/dev/null \
        | grep -oE 'in\["[^"]+"\]' | head -1 | sed 's/in\["//;s/"\]//'
    }
    OSH="$GENDIR/backend/internal/handler/Order_service_handler.go"
    PSH="$GENDIR/backend/internal/handler/Product_service_handler.go"
    OSVC="$GENDIR/backend/internal/service/order_service.go"
    PSVC="$GENDIR/backend/internal/service/product_service.go"
    CO=$(find_op "$OSH" checkout)
    DI=$(find_op "$OSH" discount)
    AS_DOWN=$(grep -oE 'case "[^"]+"' "$PSH" 2>/dev/null | sed 's/case "//;s/"//' | grep -iE 'down|decrease|remove|reduce' | head -1)
    AS_ANY=$(find_op "$PSH" stock); [ -z "$AS_ANY" ] && AS_ANY=$(find_op "$PSH" adjust)
    AS="${AS_DOWN:-$AS_ANY}"
    DI_KEY=$(op_key "$OSVC" ApplyDiscount); [ -z "$DI_KEY" ] && DI_KEY=$(op_key "$OSVC" Discount); [ -z "$DI_KEY" ] && DI_KEY="percent"
    AS_KEY=$(op_key "$PSVC" AdjustStock); [ -z "$AS_KEY" ] && AS_KEY=$(op_key "$PSVC" AdjustStockDown); [ -z "$AS_KEY" ] && AS_KEY="amount"
    if echo "$AS" | grep -qiE 'down|decrease|remove|reduce'; then AS_AMT=100; else AS_AMT=-100; fi
    {
      echo "# create order total=100 status=open"
      curl -s -X POST localhost:8080/orders -d '{"total":100,"status":"open"}'; echo
      if [ -n "$DI" ]; then
        echo "# apply discount via '/orders-ops/1/$DI' (payload key: $DI_KEY)"
        curl -s -X POST "localhost:8080/orders-ops/1/$DI" -d "{\"$DI_KEY\":10}"; echo
      else
        echo "# NO discount op was generated on Order"
      fi
      if [ -n "$CO" ]; then
        echo "# checkout via '/orders-ops/1/$CO' -> expect status paid"
        curl -s -X POST "localhost:8080/orders-ops/1/$CO"; echo
        echo "# checkout AGAIN -> ideally 400 'already paid' (tests the guard)"
        curl -s -o /dev/null -w "  http=%{http_code}\n" -X POST "localhost:8080/orders-ops/1/$CO"
      else
        echo "# NO checkout op was generated on Order"
      fi
      echo "# create product stock=10"
      curl -s -X POST localhost:8080/products -d '{"name":"widget","stock":10}'; echo
      if [ -n "$AS" ]; then
        echo "# over-draw stock via '/products-ops/1/$AS' ($AS_KEY=$AS_AMT) -> ideally 400 cannot go negative"
        curl -s -X POST "localhost:8080/products-ops/1/$AS" -d "{\"$AS_KEY\":$AS_AMT}" -w "\n  http=%{http_code}\n"
      else
        echo "# NO adjust-stock op was generated on Product (operation-misplacement?)"
      fi
    } >> "$OUT/04_run_curl.txt" 2>&1
    kill $SRV 2>/dev/null; wait $SRV 2>/dev/null
  )
  if grep -q "http=" "$OUT/04_run_curl.txt"; then
    ok "run+curl — server answered (see 04_run_curl.txt: discovered ops + business-rule codes)"
  else
    no "run+curl — see 04_run_curl.txt"
  fi
else
  sk "run+curl — no app to run"
fi

# ─────────────────────────────────────────────────────────────
# 05 — CONTINUE: add an entity to the generated app (v23.24)
# ─────────────────────────────────────────────────────────────
step "05 continue (add Customer to the generated app)"
if [ -f "$GENDIR/backend/main.go" ]; then
  CONT_IDEA='Add a Customer with a name and an email. Add a note field to Order.'
  if [ "$MODEL_OK" = 1 ]; then
    ( cd "$WORK" && GEN_LOGIC=ollama OLLAMA_MODEL_PLAN="$PLAN_MODEL" OLLAMA_MODEL_CODE="$CODE_MODEL" \
        "$GEN" continue "$GENDIR" "$CONT_IDEA" ) > "$OUT/05_continue.log" 2>&1
  else
    cat > "$WORK/delta.json" <<'JSON'
{"changes":[
 {"kind":"NEW","entity":"Customer","fields":{"name":"string","email":"string"}},
 {"kind":"ADD_FIELD","entity":"Order","field":"note","field_type":"string"}
]}
JSON
    ( cd "$WORK" && GEN_CONTINUE_DELTA="$WORK/delta.json" \
        "$GEN" continue "$GENDIR" "$CONT_IDEA" ) > "$OUT/05_continue.log" 2>&1
  fi

  # D: the continue TASK can report failed ops while the scaffold still
  # compiles. Scan the task log for a REAL failure BEFORE trusting the build.
  # The success summary is "✅ N done  ❌ 0 failed" — so a bare ❌ is NOT a
  # failure; only "❌ <nonzero>" or "<nonzero> failed" is.
  CONT_FAIL=0
  if grep -qE '❌ [1-9]' "$OUT/05_continue.log"; then CONT_FAIL=1; fi
  if grep -qE '[1-9][0-9]* failed' "$OUT/05_continue.log"; then CONT_FAIL=1; fi
  if grep -qiE 'continuation failed|undefined:|panic:' "$OUT/05_continue.log"; then CONT_FAIL=1; fi

  STAGED="$GENDIR.evolve-review"
  if [ -d "$STAGED/backend/internal/model" ]; then
    ( cd "$STAGED" && find backend/internal/model -name '*.go' | sort ) > "$OUT/05_continue_tree.txt"
    echo "--- staged Order (should keep original fields + new note) ---" >> "$OUT/05_continue_tree.txt"
    cat "$STAGED/backend/internal/model/order.go" >> "$OUT/05_continue_tree.txt" 2>/dev/null
    ( cd "$STAGED/backend" && go build ./... 2>>"$OUT/05_continue.log" ) \
      && echo "STAGED_BUILD=ok" >> "$OUT/05_continue.log" \
      || echo "STAGED_BUILD=FAIL" >> "$OUT/05_continue.log"

    if [ "$CONT_FAIL" = 1 ]; then
      no "continue — task reported a failure (see 05_continue.log)"
      grep -nE '❌ [1-9]|[1-9][0-9]* failed|undefined:|panic:|continuation failed' "$OUT/05_continue.log" | head -5 | sed 's/^/      /'
    elif grep -q "STAGED_BUILD=ok" "$OUT/05_continue.log"; then
      ok "continue — staged delta builds green AND task reported 0 failures"
    else
      no "continue — staged app did not build (05_continue.log)"
    fi
  else
    no "continue — no staged review dir (05_continue.log)"
  fi
else
  sk "continue — no base app"
fi

# ─────────────────────────────────────────────────────────────
# 06 — determinism (the offline promise) — model-dependent
# ─────────────────────────────────────────────────────────────
step "06 determinism"
if [ "$MODEL_OK" = 1 ]; then
  "$GENBENCH" determinism --root "$BENCH_ROOT" --model "$CODE_MODEL" \
      --host "$OLLAMA_HOST_DEFAULT" > "$OUT/06_determinism.txt" 2>&1
  if grep -iqE "1 distinct hash|all_outputs_identical|TOTAL: [0-9]+ pass, 0 fail" "$OUT/06_determinism.txt"; then
    ok "determinism — see 06_determinism.txt"
  else
    no "determinism — see 06_determinism.txt"
  fi
else
  sk "determinism — needs Ollama"
  echo "skipped: no model" > "$OUT/06_determinism.txt"
fi

# ─────────────────────────────────────────────────────────────
# summary
# ─────────────────────────────────────────────────────────────
step "SUMMARY"
note ""
note "PASS=$pass  FAIL=$fail  SKIP=$skip"
note ""
note "Hand back the whole evidence directory (zip it):"
note "    cd $BASE && zip -r gen-verify-out.zip gen-verify-out"
note ""
if [ "$MODEL_OK" != 1 ]; then
  note "NOTE: ran WITHOUT a live model. Model-dependent steps used deterministic"
  note "fallbacks. For the full picture, start Ollama and re-run without --no-ollama."
fi

[ "$fail" -eq 0 ] && exit 0 || exit 1
