#!/usr/bin/env bash
# verify-gen-enrich.sh — on-box verification for gen-enrich (gen-system v26.16)
#
# Run from the snapshot root:
#     cd gen-system-v26.16
#     bash verify-gen-enrich.sh [TARGET_PROJECT]
#
# TARGET_PROJECT defaults to "." (this repo). Point it at a small real project
# for a more meaningful run, e.g.:  bash verify-gen-enrich.sh ../myapp
#
# Everything is read-only EXCEPT: it writes <TARGET>/.gen/beliefs.json (the whole
# point) and a few files under ./_verify_enrich/. No network beyond local Ollama.
#
# Paste the ENTIRE output back. Sections are delimited so nothing is ambiguous.

set -uo pipefail   # deliberately NOT -e: we want every section to run and report

TARGET="${1:-.}"
OUT="_verify_enrich"
mkdir -p "$OUT"

hr() { echo; echo "══════════ $* ══════════"; }
note() { echo "  → $*"; }

hr "0. ENVIRONMENT"
echo "date:        $(date -Is 2>/dev/null || date)"
echo "pwd:         $(pwd)"
echo "target:      $TARGET"
echo "go version:  $(go version 2>&1 || echo 'GO NOT FOUND')"
echo "uname:       $(uname -srm 2>/dev/null || echo n/a)"
echo "python3:     $(python3 --version 2>&1 || echo 'not found (only matters for .py parsing)')"
echo "go.mod head:"; head -5 go.mod 2>&1
echo "gen-enrich source present: $(test -f generator/cmd/gen-enrich/main.go && echo yes || echo NO)"
echo "studio badge: $(grep -o 'logo-badge mono">v[0-9.]*' web/studio-v23.html 2>/dev/null | head -1)"
echo "webui in sync: $(cmp -s web/studio-v23.html generator/internal/webui/studio-v23.html && echo yes || echo 'NO — run: cp web/studio-v23.html generator/internal/webui/studio-v23.html')"

hr "1. BUILD ALL BINARIES"
go build -o "$OUT/gen"                ./generator/cmd/gen                2>&1 | tail -30; echo "gen               exit=${PIPESTATUS[0]}"
go build -o "$OUT/gen-server"         ./generator/cmd/gen-server         2>&1 | tail -30; echo "gen-server        exit=${PIPESTATUS[0]}"
go build -o "$OUT/understand-server"  ./generator/cmd/understand-server  2>&1 | tail -30; echo "understand-server exit=${PIPESTATUS[0]}"
go build -o "$OUT/genbench"           ./generator/cmd/genbench           2>&1 | tail -30; echo "genbench          exit=${PIPESTATUS[0]}"
echo "--- the new one ---"
go build -o "$OUT/gen-enrich"         ./generator/cmd/gen-enrich         2>&1 | tail -40; echo "gen-enrich        exit=${PIPESTATUS[0]}"

hr "2. VET (belief + enrich + the binary)"
go vet ./generator/internal/belief/... ./generator/internal/enrich/... ./generator/cmd/gen-enrich/... 2>&1 | tail -40
echo "vet exit=$?"

hr "3. UNIT TESTS (the belief/enrich contract)"
echo "--- belief (relocation, staleness, kinds, validation, producer, hierarchy) ---"
go test ./generator/internal/belief/ -count=1 2>&1 | tail -40; echo "belief exit=${PIPESTATUS[0]}"
echo "--- enrich (prompt/mapping, no model needed) ---"
go test ./generator/internal/enrich/ -count=1 2>&1 | tail -40; echo "enrich exit=${PIPESTATUS[0]}"
echo "--- planner_v2 (correction steering) ---"
go test ./generator/internal/planner_v2/ -count=1 2>&1 | tail -20; echo "planner_v2 exit=${PIPESTATUS[0]}"
echo "--- presentation ---"
go test ./generator/internal/presentation/ -count=1 2>&1 | tail -20; echo "presentation exit=${PIPESTATUS[0]}"

hr "4. BOUNDARY (archtest: understand-server links no generation pkgs)"
go test ./generator/internal/archtest/ -count=1 -v 2>&1 | tail -25; echo "archtest exit=${PIPESTATUS[0]}"
echo "--- transitive check: does understand-server pull in ai or enrich? (empty = good) ---"
go list -deps ./generator/cmd/understand-server 2>/dev/null | grep -E "internal/(ai|enrich)$" || echo "CLEAN: no ai/enrich in understand-server's dep tree"
echo "--- gen-enrich SHOULD pull them in: ---"
go list -deps ./generator/cmd/gen-enrich 2>/dev/null | grep -E "internal/(ai|enrich|belief)$" || echo "unexpected: gen-enrich missing expected deps"

hr "5. THE THING I COULD NOT VERIFY — body-slicing offsets"
note "This is the one piece I flagged: gen-enrich slices each routine's source"
note "using sym.DefinedIn + Span.Start.Offset/End.Offset. If a frontend's offset"
note "convention differs, bodies come out empty, truncated, or shifted."
# NOTE: must live INSIDE the module as a real package — `go run somefile.go`
# compiles as "command-line-arguments" which is denied access to internal/.
OCDIR="generator/cmd/_offsetcheck"
mkdir -p "$OCDIR"
cat > "$OCDIR/main.go" <<'EOF'
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	gencontext "gen-system/generator/internal/context"
	"gen-system/generator/internal/symbols"
)

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	ctx := gencontext.LoadContext(root)

	files := map[string][]byte{}
	read := func(p string) []byte {
		if b, ok := files[p]; ok {
			return b
		}
		b, err := os.ReadFile(p)
		if err != nil {
			b = nil
		}
		files[p] = b
		return b
	}

	type row struct {
		name, lang, verdict, first string
		n                          int
	}
	var rows []row
	byLang := map[string][3]int{}

	total := 0
	for _, s := range ctx.Index.AllSymbols() {
		if s.Provenance.IsExternal() {
			continue
		}
		switch s.Kind {
		case symbols.SymFunc, symbols.SymMethod, symbols.SymProcedure:
		default:
			continue
		}
		total++
		src := read(s.DefinedIn)
		st, en := s.Span.Start.Offset, s.Span.End.Offset
		lang := string(s.Language)
		c := byLang[lang]

		verdict, first := "", ""
		bodyText := ""
		usedFallback := false
		if src != nil {
			if en > st && st >= 0 && en <= len(src) {
				bodyText = string(src[st:en])
			} else if s.Span.Start.Line > 0 {
				// v26.20: frontends without byte offsets (Java/COBOL/TS/Python) slice by line.
				usedFallback = true
				lines := strings.Split(string(src), "\n")
				sl := s.Span.Start.Line
				el := s.Span.End.Line
				if el <= 0 || el < sl {
					el = sl + 60
				}
				if el > len(lines) {
					el = len(lines)
				}
				if sl <= len(lines) {
					bodyText = strings.Join(lines[sl-1:el], "\n")
				}
			}
		}
		switch {
		case src == nil:
			verdict = "FILE-UNREADABLE"
		case bodyText == "":
			verdict = fmt.Sprintf("NO-BODY(st=%d,en=%d,line=%d)", st, en, s.Span.Start.Line)
		default:
			body := strings.TrimSpace(bodyText)
			if body == "" {
				verdict = "EMPTY"
			} else {
				head := body
				if len(head) > 60 {
					head = head[:60]
				}
				first = strings.ReplaceAll(head, "\n", "\\n")
				if s.Name != "" && strings.Contains(body[:min(len(body), 200)], s.Name) {
					verdict = "OK"
					if usedFallback {
						verdict = "OK(by-line)"
					}
				} else {
					verdict = "SUSPICIOUS(name-not-in-head)"
				}
			}
		}
		switch {
		case strings.HasPrefix(verdict, "OK"):
			c[0]++
		case verdict == "EMPTY":
			c[1]++
		default:
			c[2]++
		}
		byLang[lang] = c
		rows = append(rows, row{s.Name, lang, verdict, first, en - st})
	}

	fmt.Printf("routines examined: %d\n\n", total)
	fmt.Println("per-language: ok / empty / suspicious-or-bad")
	langs := []string{}
	for l := range byLang {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	for _, l := range langs {
		c := byLang[l]
		fmt.Printf("  %-12s ok=%-5d empty=%-5d bad=%-5d\n", l, c[0], c[1], c[2])
	}

	fmt.Println("\nfirst 12 samples (verdict | lang | bytes | name | head):")
	for i, r := range rows {
		if i >= 12 {
			break
		}
		fmt.Printf("  %-30s %-8s %-6d %-24s %s\n", r.verdict, r.lang, r.n, r.name, r.first)
	}

	fmt.Println("\nfirst 10 NON-OK samples:")
	shown := 0
	for _, r := range rows {
		if strings.HasPrefix(r.verdict, "OK") || shown >= 10 {
			continue
		}
		fmt.Printf("  %-30s %-8s %-6d %-24s %s\n", r.verdict, r.lang, r.n, r.name, r.first)
		shown++
	}
	if shown == 0 {
		fmt.Println("  (none - every slice looked sane)")
	}
}
EOF
go build -o "$OUT/offsetcheck" ./generator/cmd/_offsetcheck 2>&1 | tail -20
echo "offsetcheck build exit=${PIPESTATUS[0]}"
"$OUT/offsetcheck" "$TARGET" 2>&1 | grep -v "level=INFO" | tail -50
echo "offsetcheck exit=${PIPESTATUS[0]}"
rm -rf "$OCDIR"

hr "6. OLLAMA REACHABLE?"
echo "OLLAMA_HOST=${OLLAMA_HOST:-<unset, default http://localhost:11434>}"
curl -sS -m 5 "${OLLAMA_HOST:-http://localhost:11434}/api/tags" 2>&1 | head -c 600; echo
echo "(if that failed, sections 7-9 will correctly report the graceful-absence path)"

hr "7. GEN-ENRICH: GRACEFUL ABSENCE + HELP"
"$OUT/gen-enrich" --help 2>&1 | head -20
echo "--- with a deliberately bogus OLLAMA_HOST (must exit cleanly, not panic) ---"
OLLAMA_HOST="http://127.0.0.1:9" "$OUT/gen-enrich" --root "$TARGET" 2>&1 | head -15
echo "exit=${PIPESTATUS[0]}  (expected: non-zero with a clear 'model not reachable' message)"

hr "8. GEN-ENRICH: SMALL LIVE RUN (limit 3)"
BEFORE="$TARGET/.gen/beliefs.json"
echo "beliefs.json before: $(test -f "$BEFORE" && wc -c < "$BEFORE" || echo 'absent') bytes"
time "$OUT/gen-enrich" --root "$TARGET" --limit 3 2>&1 | tail -25
echo "exit=${PIPESTATUS[0]}"
echo "beliefs.json after:  $(test -f "$BEFORE" && wc -c < "$BEFORE" || echo 'absent') bytes"

hr "9. WHAT THE MODEL ACTUALLY WROTE (the quality question)"
if [ -f "$BEFORE" ]; then
  python3 - "$BEFORE" <<'PY' 2>&1 | head -70
import json,sys
d=json.load(open(sys.argv[1]))
eb=d.get("enrich_beliefs") or {}
sm=d.get("summaries") or {}
co=d.get("corrections") or {}
print(f"corrections={len(co)}  enrich_beliefs={len(eb)}  summaries={len(sm)}")
print("top-level keys:", sorted(d.keys()))
if "beliefs" in d:
    print("!! legacy 'beliefs' key present (transient parser beliefs should NOT persist):", len(d["beliefs"]))
from collections import Counter
print("\nkinds:", Counter(b.get("kind") for b in eb.values()))
print("validated:", Counter(b.get("validated") for b in eb.values()))
print("has source_hash:", sum(1 for b in eb.values() if b.get("source_hash")), "/", len(eb))
print("\n--- up to 8 enrich beliefs verbatim ---")
for i,(k,b) in enumerate(sorted(eb.items())):
    if i>=8: break
    print(f"\n[{b.get('kind')}] {b.get('subject')}")
    print(f"   claim     : {b.get('object')}")
    print(f"   validated : {b.get('validated')}  note={b.get('validation_note','')}")
    print(f"   src_hash  : {(b.get('source_hash') or '')[:16]}  stale={b.get('stale',False)}")
if sm:
    print("\n--- summaries ---")
    for k,s in sorted(sm.items()):
        print(f"\n[{s.get('scope')}] {s.get('subject')}  stale={s.get('stale',False)}")
        print(f"   {s.get('object')}")
        print(f"   members={len(s.get('member_ids') or [])} fp={(s.get('child_fingerprint') or '')[:12]}")
PY
else
  echo "no beliefs.json at $BEFORE — nothing was written (see section 8 exit code)"
fi

hr "10. SUMMARIZE MODE + CASCADE (only if section 8 worked)"
"$OUT/gen-enrich" --root "$TARGET" --limit 3 --summarize 2>&1 | tail -15
echo "exit=${PIPESTATUS[0]}"
echo "--- summaries after --summarize ---"
if [ -f "$BEFORE" ]; then
  python3 - "$BEFORE" <<'PY' 2>&1 | head -25
import json,sys
d=json.load(open(sys.argv[1]))
sm=d.get("summaries") or {}
print("summaries:",len(sm))
for k,s in sorted(sm.items()):
    print(f"  [{s.get('scope')}] {s.get('subject')}: {(s.get('object') or '')[:160]}")
PY
fi

hr "11. IDEMPOTENCE / RE-RUN SAFETY"
echo "re-running the same command must replace, not duplicate:"
"$OUT/gen-enrich" --root "$TARGET" --limit 3 >/dev/null 2>&1
if [ -f "$BEFORE" ]; then
  python3 -c "
import json
d=json.load(open('$BEFORE'))
eb=d.get('enrich_beliefs') or {}
print('enrich_beliefs after re-run:',len(eb),'(should match section 9 count, not double)')
print('duplicate ids:', len(list(eb.keys()))-len(set(eb.keys())))
"
fi

hr "12. SUMMARY FOR CLAUDE"
echo "Paste everything above. Key things I'm looking for:"
echo "  §1  gen-enrich exit=0 (it is the only binary I could not compile-check)"
echo "  §3  belief + enrich tests PASS"
echo "  §4  understand-server dep tree CLEAN of ai/enrich"
echo "  §5  offsetcheck: mostly OK, few EMPTY/SUSPICIOUS — this is the flagged risk"
echo "  §9  validated= mix of consistent/needs-human (contradicts is fine and expected sometimes)"
echo "  §9  has_source_hash == count (staleness fingerprints are being stored)"
echo "  §9  NO legacy 'beliefs' key (parser beliefs must not persist)"
echo "  §11 no duplicate ids after re-run"
echo
echo "Artifacts left in $OUT/ (binaries + offsetcheck.go) — safe to delete."
