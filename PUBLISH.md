# PUBLISH — the whole path from here to a public repo

One document, in order. Everything before Stage 4 is reversible. Nothing is
public until Stage 5, and Stage 4 is the gate that decides whether you publish
at all.

Companion docs, all in the repo after Stage 0: `bench/README.md` (what each
driver measures), `docs/` (three engineering docs), `README.md` (the public
front page), `out/README.md` (move/execute detail).

---

## Stage 0 — Put the files where they belong · 15 min

```bash
cd ~/code/gen-system
unzip -o /path/to/out/gen-system-main-v26.26.zip -d /tmp/gs
cp -r /tmp/gs/gen-system-main/. .
git status                       # review; nothing should surprise you
```

```bash
P=~/code/my-portfolio
C=/path/to/out/changes/my-portfolio
cp $C/tools/fill_portfolio.py                          $P/tools/
cp $C/benchmarks/README.md                             $P/benchmarks/
cp $C/writeups/01-hieuluat-retrieval-optimization.md   $P/writeups/
cp $C/writeups/02-gen-system-inference-optimization.md $P/writeups/
cp $C/writeups/03-reproducible-benchmarking.md         $P/writeups/
cp $C/projects/gen-system/README.md                    $P/projects/gen-system/
```

```bash
mkdir -p ~/code/gen-system-refbuild && cd ~/code/gen-system-refbuild
unzip /path/to/out/refbuild.zip      # BESIDE the repo, never inside it
```

Never unzip anything over the portfolio. The portfolio zips carry an empty
`measured.json` and would erase your filled rows.

---

## Stage 1 — Make the repo safe to publish · 30 min

This stage exists because publishing is not undoable. Three things in the tree
you would not want to ship:

**1817 cache files.** Five `.gen/` directories hold AST caches and one belief
store left over from test runs. The new `.gitignore` (in the v26.26 zip)
excludes them. If any were already committed, drop them from the index:

```bash
cd ~/code/gen-system
git rm -r --cached .gen generator/internal/*/.gen 2>/dev/null || true
git status --short | head
```

**`guide/GEN_SYSTEM_CAPABILITY_AND_VC_ANALYSIS.md`.** A fundraising-readiness
document: market position, competitive assessment, and whether to take this to
YC or Antler, written deliberately critically. That is internal strategy. In a
public repo it becomes a list of your product's weaknesses in your own words,
quotable out of context, dated June 2026. **Recommendation: keep it out of the
public tree.** The honest-limits material you *do* want public is already in
`README.md` under Limitations.

```bash
git rm --cached guide/GEN_SYSTEM_CAPABILITY_AND_VC_ANALYSIS.md 2>/dev/null || true
echo 'guide/GEN_SYSTEM_CAPABILITY_AND_VC_ANALYSIS.md' >> .gitignore
```

Read the other four `guide/*.md` yourself before publishing. They were written
as internal manuals and are probably fine, but I have only listed them.

**`results/` is deliberately NOT ignored.** The raw JSON is the evidence a
published number rests on, and the commit carrying it dates the run.

One placeholder is still open:

```bash
grep -n 'TODO repo url' README.md      # fill in the real clone URL
```

---

## Stage 2 — Verify, freeze, commit · 30 min

```bash
cd ~/code/gen-system
go build ./... && go test ./...
```

Expected: **25 packages ok, 0 failures.** That is the verified state on Windows;
Linux should match. A failure here is new drift, worth understanding before you
spend hours measuring.

```bash
./bench/freeze-env.sh            # writes bench/env.json: commit, GPU, driver, models
git add .gitignore bench/ docs/ generator/ LICENSE NOTICE README.md
git commit -m "bench: WP1/WP2 instrumentation, frozen task set, docs, Apache-2.0"
```

**Commit before you measure.** `run-all.sh` refuses a dirty tree, and the commit
timestamp is the proof the task set was fixed before the results existed. That
proof is the entire argument of writeup 03.

---

## Stage 3 — Run the campaign · most of a day

```bash
ollama pull qwen3:14b
ollama pull qwen2.5-coder:14b
ollama pull deepseek-coder-v2:16b-lite-instruct-q4_K_M
ollama pull deepseek-coder-v2:16b-lite-instruct-q5_K_M
ollama pull deepseek-coder-v2:16b-lite-instruct-q8_0

MEASURED=~/code/my-portfolio/benchmarks/results/measured.json ./bench/run-all.sh
```

Or piecewise, fastest feedback first:

| Command | Fills | Rough time |
|---|---|---|
| `./bench/run-understand.sh` | 4 pinned public repos + CobolCraft baseline verify | minutes, no GPU |
| `./bench/run-stream.sh` | TTFT, stream on vs off | minutes |
| `./bench/run-offload.sh` | tok/s and VRAM vs `OLLAMA_NUM_GPU` | ~30 min |
| `./bench/run-quant-sweep.sh` | Q4/Q5/Q8 plus the production pair, n=3 | hours |
| `./bench/run-strategies.sh` | strategies A, B, C on the frozen task set | hours |
| `python3 bench/summarise.py --results results/<date> --measured $MEASURED` | every table | seconds |

Start with `run-understand.sh`. No GPU, and it exercises the parser path end to
end, so a broken setup shows up in minutes instead of after the sweep.

```bash
git add results/ && git commit -m "bench: <date> campaign raw results"
```

---

## Stage 4 — GATE: is this publishable? · 1 hour

Do not skip to Stage 5. Every line is from PLAN.md's Definition of Done.

```bash
cd ~/code/my-portfolio
python3 tools/fill_portfolio.py --gen-system ~/code/gen-system --hieuluat ~/code/hieuluat
python3 tools/fill_portfolio.py --check          # must list nothing unfilled
```

| # | Gate | How to check |
|---|---|---|
| 1 | Writeup 02 tables have rows, n and range, or say future work | Open §6; tables inject between the `measured:gen` markers |
| 2 | **At least one metric differs across quants** | Compare `stub_rate_pct` and `op_repair_attempts` across Q4/Q5/Q8 |
| 3 | At least one honest loss reported | Already true twice: writeup 01 §6 (rerank), writeup 02 §4 (broken metric) |
| 4 | No template text | `grep -rn '_readme\|WebAssembly-free\|Replace every' ~/code/my-portfolio` returns nothing |
| 5 | CV, story, README, portfolio, reference agree | All done except the CV — Stage 6 |
| 6 | Reference 100% annotated | In refbuild: `extract.py` then `check.py` must exit 0 |

**Gate 2 is the one that matters.** If stub rate and repair counts are identical
across every quant, the new metric still is not measuring anything and you are
back where P1 started. Stop and tell me if that happens — it is a real finding,
not a formality, and it changes what writeup 02 should say.

---

## Stage 5 — Publish · 1 hour

Repo first, then portfolio, then the post that links to both.

```bash
cd ~/code/gen-system
git log --oneline | head            # last sanity read
git remote add origin <your repo url>
git push -u origin main
```

Flip it public in GitHub settings. On the public page, confirm `LICENSE` renders
as Apache-2.0 and no `.gen/` directory appears.

```bash
cd ~/code/my-portfolio
rm writeups/*.html                  # .md files changed; twins are stale, GitHub renders .md
git add -A && git commit -m "portfolio: measured results, corrected claims" && git push
```

**The launch post** is `out/changes/drafts/drafts-launch-story.md`. The voice is
final. Three things need your hand:

- swap the placeholder repo link for the real one
- check the swap-cost figure in "The 16 GB problem" against what the run actually
  produced, or cut the number and link the table instead
- the determinism paragraph is written as a confession. Read it once more and
  decide you are happy standing behind it. It is the strongest paragraph in the
  piece, and it is about a bug you shipped.

---

## Stage 6 — The four things only you can decide

| Decision | Where | Recommendation |
|---|---|---|
| **The CV** | Not in this directory | Three corrections: flowcharts are rendered for human review, not fed to the model; the stub path exists and the repair caps are 2 (operation) and 3 (task); "sequential scheduling with keep-alive" is Ollama keep-alive plus two roles plus measurement, not a scheduler. Gate 5 fails until this is done. |
| **The reranker** | writeup 01 §6, `[TODO]` | Drop it. 384 ms for zero measured gain, and say you removed it. That reads better than leaving it on. |
| **The VC analysis doc** | `guide/` | Keep it out of the public tree (Stage 1). |
| **Repo URL** | `README.md` quickstart | Only you know it. |

---

## If you have an hour, not a day

```bash
cd ~/code/gen-system
go build ./... && go test ./...
./bench/freeze-env.sh && git add -A && git commit -m "bench: freeze environment and task set"
./bench/run-understand.sh
python3 bench/summarise.py --results results/$(date +%F)
```

That produces the Understand table: four public repos at pinned commits,
including 30k lines of third-party COBOL. It is the most defensible artifact in
the set, because anyone can re-run it and get your number. The GPU sweeps can
follow later.
