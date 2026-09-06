# PUBLISH: from the v26.35 zip to a public repo, with a fresh campaign

One document, in order, written for a rerun. Everything before Stage 5 is reversible. Nothing
is public until Stage 6, and Stage 5 is the gate that decides whether you publish at all.

Where things are: `bench/README.md` (what each driver measures), `bench/swebench/README.md`
(the SWE-bench boundary), `docs/` (four engineering docs, including `style-gate.md`),
`README.md` (the public front page), `out/README.md` (move and execute detail),
`out/RESULTS-REVIEW.md` (what the 2026-09-04 upload got wrong and why).

What changed since the last runbook, in one list:

- Strategy B in the 2026-09-04 campaign measured nothing. A keep alive encoding bug made every
  request fail. The door, the pipeline and the aggregator are fixed, and the 2026-09-06 rerun
  measured all three strategies.
- SWE-bench ran for the first time on 2026-09-06 (60 tasks). The step is inside `run-all.sh`
  and `run-partial.sh`.
- `run-partial.sh` (v26.35) runs only the drivers you name and reuses every other table from an
  earlier directory without re-dating it.
- `bench/attest.py` writes the certificate for a campaign. `run-all.sh` runs it last.
- `results/` no longer picks up the genbench binary or the corpus clones.
- Team style profiles exist (v26.33). Nothing measures them yet by default; Stage 4 has an
  optional run.

---

## Stage 0. Put the files where they belong (15 min)

```bash
cd ~/code/gen-system
unzip -o /path/to/out/gen-system-main-v26.35.zip -d /tmp/gs
cp -r /tmp/gs/gen-system-main/. .
git status                       # review; nothing should surprise you
```

v26.34 changed the verify stage: it now attaches the standard library and reports internal
resolution only, so the 171 finding halt you saw does not happen on this zip. The zip does not carry `results/`, `bench/env.json` or `bench/swebench/tasks.jsonl`. Your results
stay yours, the environment is frozen again in Stage 2, and the task list is re-fetched and
checked against the committed `tasks.sha256`.

```bash
P=~/code/my-portfolio
cp -r /path/to/out/changes/my-portfolio/. $P/
```

That copies writeups 02 and 03 (markdown and regenerated html), the gen-system project page,
`tools/fill_portfolio.py`, and this time `benchmarks/results/measured.json`: your uploaded file
plus the 2026-09-05 rows for four tables and the SWE-bench per repository table. If you ran
anything after the upload, do not copy `measured.json`; run the commands in
`RESULTS-REVIEW-2.md` section 5 instead.

```bash
cp /path/to/out/changes/gen-system/results-2026-09-04/* ~/code/gen-system/results/2026-09-04/
```

That adds the attestation, the manifest and the corrected `measured.rows.json` to the campaign
you already ran.

```bash
mkdir -p ~/code/gen-system-refbuild && cd ~/code/gen-system-refbuild
unzip -o /path/to/out/refbuild.zip     # BESIDE the repo, never inside it
```

---

## Stage 1. Make the repo safe to publish (30 min)

Publishing is not undoable. Five things in the tree must not ship.

**The cache directories.** `.gitignore` excludes `.gen/`. If any were committed earlier:

```bash
cd ~/code/gen-system
git rm -r --cached .gen generator/internal/*/.gen 2>/dev/null || true
```

**The genbench binary and the corpus clones inside `results/`.** `git add results/` on
2026-09-04 picked up the 14 MB binary and turned the CobolCraft clone into a submodule. Both are
ignored now; the index still holds them:

```bash
git rm --cached -r results/2026-09-04/corpus results/2026-09-04/genbench 2>/dev/null || true
git rm --cached .gitmodules 2>/dev/null || true; rm -f .gitmodules
```

**`PUBLISH.md` in the repo root.** It is this internal runbook. It does not belong in the tree:

```bash
git rm --cached PUBLISH.md 2>/dev/null || true; rm -f PUBLISH.md
```

**`guide/GEN_SYSTEM_CAPABILITY_AND_VC_ANALYSIS.md`.** Internal strategy, written deliberately
critically. In a public repo it becomes a list of the product's weaknesses in your own words.
Keep it out. The honest limits you want public are already in `README.md` under Limitations.

```bash
git rm --cached guide/GEN_SYSTEM_CAPABILITY_AND_VC_ANALYSIS.md 2>/dev/null || true
echo 'guide/GEN_SYSTEM_CAPABILITY_AND_VC_ANALYSIS.md' >> .gitignore
```

Read the other `guide/*.md` yourself before publishing. They were internal manuals.

**One placeholder.**

```bash
grep -n 'TODO repo url' README.md      # fill in the real clone URL
```

`results/` itself is deliberately not ignored. The raw JSON is the evidence a published number
rests on, and the commit that carries it dates the run.

```bash
git status --short | head -40    # nothing under results/ should be a binary or a clone
```

---

## Stage 2. Verify, freeze, commit (30 min)

```bash
cd ~/code/gen-system
go build ./... && go test ./...
```

Expected: **26 packages ok, 0 failures.** That is the verified state on Windows at v26.35
(25 packages plus the new style package). A failure on Linux is new drift; understand it before
spending hours measuring.

```bash
./bench/freeze-env.sh            # writes bench/env.json: commit, GPU, driver, models
git add -A
git commit -m "v26.35: keep alive fix, attestation, SWE-bench in run-all, style profiles, import scoped resolution, stdlib auto, partial runs with reuse"
./bench/freeze-env.sh            # again, now that the tree is clean, so working_tree_dirty is false
git add bench/env.json && git commit -m "bench: freeze environment at a clean tree"
```

The double freeze is on purpose. The 2026-09-04 environment file says `working_tree_dirty: true`,
and the attestation had to print that. This time the file must say false. `run-all.sh` refuses a
dirty tree, and the commit order is the proof the task set was fixed before the results existed.
That proof is the whole argument of writeup 03.

---

## Stage 3. Run the campaign (most of a day)

```bash
ollama pull qwen3:14b
ollama pull qwen2.5-coder:14b
ollama pull deepseek-coder-v2:16b-lite-instruct-q4_K_M
ollama pull deepseek-coder-v2:16b-lite-instruct-q5_K_M
ollama pull deepseek-coder-v2:16b-lite-instruct-q8_0
```

One command runs everything, in dependency order, and ends with the summaries and the attestation:

```bash
MEASURED=~/code/my-portfolio/benchmarks/results/measured.json ./bench/run-all.sh
```

`run-all.sh` now includes the SWE-bench step. `SWEBENCH_LIMIT` picks how many of the 500
Verified tasks it takes: 60 by default (minutes), `0` for all 500 (hours, and 12 repository
clones on disk), `skip` to leave it out.

Or piecewise, fastest feedback first. Every driver writes raw JSON into `results/<date>/raw/`.

| Command | Fills | Rough time |
|---|---|---|
| `./bench/run-understand.sh` | 4 pinned public repos + CobolCraft baseline verify | minutes, no GPU |
| `./bench/run-stream.sh` | TTFT, stream on vs off | minutes |
| `./bench/run-offload.sh` | tok/s and VRAM vs `OLLAMA_NUM_GPU` | ~30 min |
| `./bench/run-quant-sweep.sh` | Q4/Q5/Q8 plus the production pair, n=3 | hours |
| `./bench/run-strategies.sh` | strategies A, B, C on the frozen task set (rerun on 2026-09-06 with all rows measured) | hours |
| `./bench/swebench/run_swebench_localize.sh --limit 60` | localization recall on SWE-bench Verified. Not a solve rate; see its README | minutes for 60, hours for all 500 |
| `python3 bench/summarise.py --results results/<date> --measured $MEASURED` | every gen-system table | seconds |
| `python3 bench/swebench/summarise_swebench.py --results results/<date> --measured $MEASURED` | the three SWE-bench tables | seconds |
| `python3 bench/attest.py --results results/<date>` | `MANIFEST.sha256` and `ATTESTATION.md`, the certificate | seconds |

Start with `run-understand.sh`. No GPU, and it exercises the parser path end to end, so a broken
setup shows up in minutes instead of after the sweep.

**If you only rerun what is missing.** `run-partial.sh` (v26.35) runs the drivers you name into
today's directory and fills every other table from earlier directories without re-dating them:

```bash
MEASURED=~/code/my-portfolio/benchmarks/results/measured.json   ./bench/run-partial.sh --run strategies,swebench:60   --reuse results/2026-09-05 --reuse results/2026-09-06 --reuse results/2026-09-04
```

Reuse order matters: the first directory with raw data for a table wins, and that table keeps that
directory's date and commit in its provenance plus a `reused_from` field. The attestation for the
new directory lists every reused table with its source. With no `--reuse`, every other `results/*`
directory is tried, newest first. The three directories you have today: `2026-09-05` holds
understand, stream, offload and the quant sweep at a clean commit; `2026-09-06` holds strategies
and SWE-bench at the same commit; `2026-09-04` is the first campaign with a dirty tree and should
be last in the order. If you would rather have one directory with everything at one commit, run
`run-all.sh` and accept the day.

Then commit the evidence:

```bash
git add results/ && git commit -m "bench: <date> campaign raw results, summaries, attestation"
git log --format='%h %ad %s' --date=short -- bench/tasks.json results/   # tasks.json must come first
```

---

## Stage 4. Optional: measure the style gate (1 hour, GPU)

Nothing in the portfolio quotes a style number, and nothing has to. If you want one run on
record so a reader can see the feature produce metrics:

```bash
cd ~/code/gen-system
gen style print > bench/gen-style.json        # the default profile; edit if you like, then commit it
git add bench/gen-style.json && git commit -m "bench: default style profile for the gate run"
GEN_LOGIC=ollama OLLAMA_MODEL_PLAN=qwen3:14b OLLAMA_MODEL_CODE=qwen2.5-coder:14b \
  results/$(date +%F)/genbench genpipe --ideas bench/tasks.json --model qwen2.5-coder:14b \
  --style bench/gen-style.json --style-mode gate \
  --out results/$(date +%F)/raw/style-gate-genpipe-run1.json
```

The raw JSON carries `ops_style_violations`, `ops_style_repair_attempts`, `ops_style_repaired`,
`style_findings`, `style_errors`, `style_clean_ideas`. `summarise.py` does not build a table
for them yet; if the numbers are worth a paragraph, write the paragraph from the raw file and
cite it. Do not add a table without an aggregator behind it.

---

## Stage 5. GATE: is this publishable? (1 hour)

Do not skip to Stage 6. Every line is from PLAN.md's Definition of Done, plus three added since.

```bash
cd ~/code/my-portfolio
python3 tools/fill_portfolio.py --gen-system ~/code/gen-system --hieuluat ~/code/hieuluat
python3 tools/fill_portfolio.py --check          # must list nothing unfilled that you care about
python3 tools/regen_writeup_html.py              # the html twins carry the tables only after this
```

| # | Gate | How to check |
|---|---|---|
| 1 | Writeup 02 tables have rows, n and range, or say future work | Open section 6; tables inject between the `measured:gen` markers |
| 2 | **At least one metric differs across quants** | Passed on 2026-09-04: stub rate near 99, 82, 74 and 44 percent across Q4, Q5, Q8 and production, n=3. Check again after any rerun. |
| 3 | At least one honest loss reported | True twice: writeup 01 section 6 (rerank), writeup 02 section 4 (broken metric); now a third time in section 6 (strategy B) |
| 4 | No template text | `grep -rn '_readme\|WebAssembly-free\|Replace every' ~/code/my-portfolio` returns nothing |
| 5 | CV, story, README, portfolio, reference agree | All done except the CV. See Stage 7 |
| 6 | Reference 100% annotated | In refbuild: `extract.py` then `check.py` must exit 0 |
| 7 | **No row rests on a run that measured nothing** | `grep -n 'runs not measured' writeups/02-*.md`; every value in that column must be 0 |
| 8 | **The attestation exists and matches** | `results/<date>/ATTESTATION.md` and `MANIFEST.sha256` are committed; `sha256sum -c results/<date>/MANIFEST.sha256` from inside `results/<date>/` passes |
| 9 | **SWE-bench is labelled** | The SWE-bench tables in writeup 02 carry "not a solve rate" in their caption, and the text never puts the recall next to the DeepDiscovery solve rate |

Gates 7 to 9 are the ones added since the last runbook. A row with a null quality cell and a
non zero not measured count has not been measured. It must not be described as measured.

---

## Stage 6. Publish (1 hour)

Repo first, then portfolio, then the post that links to both.

```bash
cd ~/code/gen-system
git log --oneline | head            # last sanity read
git remote add origin <your repo url>
git push -u origin main
```

Flip it public in GitHub settings. On the public page, confirm `LICENSE` renders as Apache-2.0,
no `.gen/` directory appears, no `PUBLISH.md` appears, and `results/<date>/` holds JSON and the
two attestation files only.

Do not delete the writeup `.html` files. `index.html` links to them. They are rebuilt from the
`.md` sources by `tools/regen_writeup_html.py`, which ran in Stage 5.

```bash
cd ~/code/my-portfolio
git add -A && git commit -m "portfolio: measured results, corrected claims" && git push
```

**The launch post** is `out/changes/drafts/drafts-launch-story.md`. The voice is final. Three
things need your hand:

- Swap the placeholder repo link for the real one.
- Check the swap cost figure in "The 16 GB problem" against the strategies table from the rerun.
  If it does not match, cut the number and link the table instead.
- The determinism paragraph is written as a confession. Read it once more and decide you are
  happy standing behind it. It is the strongest paragraph in the piece, and it is about a bug you
  shipped.

---

## Stage 7. The four things only you can decide

| Decision | Where | Recommendation |
|---|---|---|
| **The CV** | Not in this directory | Three corrections: flowcharts are rendered for human review, not fed to the model; the stub path exists and the repair caps are 2 (operation) and 3 (task); "sequential scheduling with keep alive" is Ollama keep alive plus two roles plus measurement, not a scheduler. Gate 5 fails until this is done. |
| **The reranker** | writeup 01 section 6, `[TODO]` | Drop it. 384 ms for zero measured gain, and say you removed it. That reads better than leaving it on. |
| **The VC analysis doc** | `guide/` | Keep it out of the public tree (Stage 1). |
| **Repo URL** | `README.md` quickstart | Only you know it. |

---

## If you have an hour, not a day

```bash
cd ~/code/gen-system
go build ./... && go test ./...
./bench/freeze-env.sh && git add -A && git commit -m "bench: freeze environment and task set"
./bench/run-strategies.sh                                  # the one table that is wrong today
./bench/swebench/run_swebench_localize.sh --limit 60       # the one table that is missing today
python3 bench/summarise.py --results results/$(date +%F) --measured $MEASURED
python3 bench/swebench/summarise_swebench.py --results results/$(date +%F) --measured $MEASURED
python3 bench/attest.py --results results/$(date +%F)
```

The strategies run is the long part. Skip it if the hour is real, and run SWE-bench alone: it
needs no GPU, it is the table that is absent, and its attestation says plainly that nothing else
ran that day.
