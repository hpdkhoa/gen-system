# SWE-bench Verified: localization recall, and only that

## The boundary, first

DeepDiscovery (arXiv 2606.22906) reports 78.6 percent on SWE-bench Verified. That is a
**solve rate**: given a GitHub issue on a real Python repository, the agent produces a patch
that makes the hidden tests pass.

gen-system cannot produce that number, and this folder does not try. gen-system has no issue
to patch pipeline. Its generation half builds new Go applications from an idea. Its repair loop
rewrites one function body at a time in code it generated itself. Turning that into an
issue to patch agent is a new product.

What gen-system can measure on the same benchmark is the part DeepDiscovery reports in its
first two evaluations: **localization**. Given the issue text and the repository at the task
commit, does the Understand graph reach the files the gold patch touched? That is their file
recovery and Full Recall Rate line. Their retrieval is a learned model and this one is a fixed rule,
so even that is not a head to head. Read the result as gen-system's own localization baseline on a
public task set.

Every output of this folder prints that boundary. Do not put the localization number and the
78.6 percent in the same sentence.

## What the script does

1. `fetch_tasks.py` downloads the 500 tasks from the Hugging Face datasets server with the
   standard library only. It sorts them, writes `tasks.jsonl`, and writes `tasks.sha256`. Later
   runs check the tasks against the hash and refuse on drift. Commit `tasks.sha256`. Git ignores
   the jsonl, and you can fetch it again.
2. `run_swebench_localize.sh` clones each of the 12 repositories once, checks out each task's
   base commit, and for each task:
   - `prep_task.py` extracts **seeds** from the issue text and **gold files** from the gold
     patch. Seed extraction is a fixed rule with no model. It takes identifiers inside
     backticks, with underscores, in camel case, followed by a parenthesis, or segments of
     dotted paths. It drops plain English words. The rule is crude, and you can explain every
     seed it picks.
   - `genbench localize` loads the repository through the Understand engine and matches seeds
     to symbols by exact name. It expands over resolved call edges in both directions for N
     hops. At each hop it reports how many gold files fall inside the reached set.
3. Once per repository it also runs the parse and resolution suites. This gives a table of
   whether the parsers can read these codebases at all.
4. `summarise_swebench.py` aggregates into `results/<date>/swebench_localization.json` and,
   with `--measured`, into the portfolio's `measured.json`.

The run uses no GPU and no model. The Python frontend needs `python3` 3.9 or newer.

## How to run

```bash
./bench/freeze-env.sh                                       # once, then commit
./bench/swebench/run_swebench_localize.sh --limit 20        # smoke run, minutes
./bench/swebench/run_swebench_localize.sh                   # all 500, hours
./bench/swebench/run_swebench_localize.sh --repo django/django --hops 3
MEASURED=~/code/my-portfolio/benchmarks/results/measured.json ./bench/swebench/run_swebench_localize.sh
```

The parse cache lives under `results/<date>/.gen/ast_cache`, and the 500 checkouts share it.
The parser reads a file that did not change between commits only once. Git ignores the clones
under `results/<date>/swebench/repos/`. Everything else under `results/` is evidence; commit it.

## How to read the result

| Column | Meaning |
|---|---|
| `file_recall_mean` | Mean over tasks of gold files reached divided by gold files total, at that hop |
| `hit_rate_pct` | Percent of tasks where the search reached at least one gold file |
| `mean_neighbourhood_files` | How many files the search reached. This is the precision side. A neighbourhood the size of the repository reaches everything and tells you nothing |
| `seeds_matched_pct` | Percent of tasks where at least one seed matched a symbol. If this is low, the bottleneck is the seed rule and not the graph |

Read recall and neighbourhood size together. A high recall with a small neighbourhood is a
useful result. A high recall with a huge neighbourhood is not.

If `seeds_matched_pct` is low for a repository, the next step is a better seed rule, and it
should stay a rule you can write down. A learned retriever here would make the number better
and the claim weaker.

## What a real solve rate would take

A solve rate needs four things gen-system does not have:

- a step from issue text to candidate functions;
- a model that writes a multi file patch, which means extending the body only contract to new
  functions and imports;
- an execution environment per repository to run the FAIL_TO_PASS tests;
- the official SWE-bench Docker harness for scoring.

That is weeks of work and a new agent. If I build it, I will measure it through this same folder
with the same frozen task set.
