#!/usr/bin/env python3
"""
prep_task.py: turn one SWE-bench Verified task into the two inputs genbench localize needs.

  seeds.txt   identifiers mentioned in the issue text, one per line
  gold.txt    files the gold patch touched, one per line, relative to the repo root
  meta.json   repo, base_commit, instance_id, counts

Seed extraction is deterministic and has no model in it. It keeps an identifier from the
issue text when it looks like code rather than English: it is inside backticks, or it
contains an underscore, or it has camel case, or it is followed by an opening parenthesis,
or it is a segment of a dotted path. Plain lowercase words are dropped. This is deliberately
crude. It trades retrieval quality for a fixed rule that anyone can explain.

Usage:  python3 bench/swebench/prep_task.py <instance_id> <outdir>
"""
import json, re, sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
TASKS = HERE / "tasks.jsonl"
STOP = set("""
after also another because before being between could does doing each every first found
from have here into just like made make many more most much must only other over same
should since some such than that their them then there these they this those through
under using very were what when where which while will with without would your about above
below error errors example expected actual issue issues problem behavior behaviour version
""".split())
IDENT = re.compile(r"[A-Za-z_][A-Za-z0-9_]*")


def gold_files_from_patch(patch):
    files = []
    for m in re.finditer(r"^diff --git a/(\S+) b/(\S+)$", patch or "", re.M):
        path = m.group(2)
        if path not in files:
            files.append(path)
    return files


def looks_like_code(tok, following):
    if len(tok) < 4 or tok.lower() in STOP:
        return False
    if "_" in tok:
        return True
    if re.search(r"[a-z][A-Z]", tok):
        return True
    if following.startswith("("):
        return True
    return False


def seeds_from_text(text, cap=80):
    seen, out = set(), []

    def add(tok):
        if tok and tok not in seen and len(tok) >= 4 and tok.lower() not in STOP:
            seen.add(tok); out.append(tok)

    # 1. anything inside backticks is code by declaration
    for span in re.findall(r"`([^`]+)`", text or ""):
        for part in re.split(r"[^A-Za-z0-9_]+", span):
            add(part)
    # 2. dotted paths: keep each segment
    for path in re.findall(r"\b[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)+\b", text or ""):
        for seg in path.split("."):
            add(seg)
    # 3. bare identifiers that look like code
    for m in IDENT.finditer(text or ""):
        tok = m.group(0)
        following = (text or "")[m.end():m.end() + 1]
        if looks_like_code(tok, following):
            add(tok)
    return out[:cap]


def main():
    if len(sys.argv) != 3:
        print(__doc__); return 2
    instance, outdir = sys.argv[1], Path(sys.argv[2])
    task = None
    with open(TASKS, encoding="utf-8") as f:
        for line in f:
            r = json.loads(line)
            if r["instance_id"] == instance:
                task = r; break
    if task is None:
        print(f"instance {instance} not in {TASKS}", file=sys.stderr); return 1

    outdir.mkdir(parents=True, exist_ok=True)
    seeds = seeds_from_text(task["problem_statement"])
    gold = gold_files_from_patch(task["patch"])
    (outdir / "seeds.txt").write_text("\n".join(seeds) + "\n", encoding="utf-8")
    (outdir / "gold.txt").write_text("\n".join(gold) + "\n", encoding="utf-8")
    (outdir / "meta.json").write_text(json.dumps({
        "instance_id": instance, "repo": task["repo"], "base_commit": task["base_commit"],
        "version": task.get("version"), "seeds": len(seeds), "gold_files": len(gold),
    }, indent=2), encoding="utf-8")
    print(f"{instance}: {len(seeds)} seeds, {len(gold)} gold files")
    return 0


if __name__ == "__main__":
    sys.exit(main())
