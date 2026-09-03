# SWE-bench Verified: localization recall, and only that

## The boundary, first

DeepDiscovery (arXiv 2606.22906) reports 78.6 percent on SWE-bench Verified. That is a
**solve rate**: given a GitHub issue on a real Python repository, produce a patch that makes
the hidden tests pass.

gen-system cannot produce that number, and this folder does not try. gen-system has no issue
to patch pipeline. Its generation half builds new Go applications from an idea. Its repair loop
rewrites one function body at a time in code it generated itself. Turning that into an
issue to patch agent is a new product, not a script.

What gen-system can measure on the same benchmark is the part DeepDiscovery reports in its
first two evaluations: **localization**. Given the issue text and the repository at the task
commit, does the Understand graph reach the files the gold patch touched? That is their file
recovery and Full Recall Rate line. Even that is not a head to head, because their retrieval is
learned and this one is a fixed rule. Treat the result as gen-system's own localization
baseline on a public task set, not as a comparison.

Every output of this folder prints that boundary. Do not put the localization number and the
78.6 percent in the same sentence.

## What the script does

1. `fetch_tasks.py` downloads the 500 tasks from the Hugging Face datasets server, stdlib only,
   sorts them, writes `tasks.jsonl`, and writes `tasks.sha256`. Later runs verify against the
   hash and refuse on drift. Commit `tasks.sha256`. The jsonl is git ignored and re fetchable.
2. `run_swebench_localize.sh` clones each of the 12 repositories once, checks out each task's
   base commit, and for each task:
   - `prep_task.py` extracts **seeds** from the issue text and **gold files** from the gold
     patch. Seed extraction is a fixed rule with no model: identifiers inside backticks, with
     underscores, in camel case, followed by a parenthesis, or segments of dotted paths. Plain
     English words are dropped. It is deliberately crude and fully explainable.
   - `genbench localize` loads the repository through the Understand engine and matches seeds
     to symbols by exact name. It expands over resolved call edges in both directions for N
     hops. At each hop it reports how many gold files fall inside the reached set.
3. Once per repository it also runs the parse and resolution suites, so there is a table of
   whether the parsers can read these codebases at all.
4. `summarise_swebench.py` aggregates into `results/<date>/swebench_localization.json` and,
   with `--measured`, into the portfolio's `measured.json`.

No GPU and no model are involved. The Python frontend needs `python3` 3.9 or newer.

## How to run

```bash
./bench/freeze-env.sh                                       # once, then commit
./bench/swebench/run_swebench_localize.sh --limit 20        # smoke run, minutes
./bench/swebench/run_swebench_localize.sh                   # all 500, hours
./bench/swebench/run_swebench_localize.sh --repo django/django --hops 3
MEASURED=~/code/my-portfolio/benchmarks/results/measured.json ./bench/swebench/run_swebench_localize.sh
```

The parse cache lives under `results/<date>/.gen/ast_cache` and is shared across the 500
checkouts, so files unchanged between commits are parsed once. Clones under
`results/<date>/swebench/repos/` are git ignored. Everything else under `results/` is
evidence and should be committed.

## How to read the result

| Column | Meaning |
|---|---|
| `file_recall_mean` | Mean over tasks of gold files reached divided by gold files total, at that hop |
| `hit_rate_pct` | Percent of tasks where at least one gold file was reached |
| `mean_neighbourhood_files` | How many files the search reached. This is the precision side. A neighbourhood the size of the repository reaches everything and tells you nothing |
| `seeds_matched_pct` | Percent of tasks where at least one seed matched a symbol. If this is low, the seed rule is the bottleneck, not the graph |

Read recall and neighbourhood size together. A high recall with a small neighbourhood is a
useful result. A high recall with a huge neighbourhood is not.

If `seeds_matched_pct` is low for a repository, the honest next step is a better seed rule,
and it should stay a rule you can write down. Adding a learned retriever here would make the
number better and the claim weaker.

## What a real solve rate would take

Stated so nobody mistakes the gap for an oversight. A solve rate needs four things gen-system
does not have. A step from issue text to candidate functions. A model producing a multi file
patch, which means extending the body only contract to new functions and imports. An execution
environment per repository to run the FAIL_TO_PASS tests. And the official SWE-bench Docker
harness for scoring. That is weeks of work and a new agent. If it gets built, it gets measured
through this same folder with the same frozen task set.
