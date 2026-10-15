#!/usr/bin/env python3
"""Extract every CALL and PERFORM site from COBOL source with GnuCOBOL's cobc.

cobc -C translates each file to C. The C marks every statement with a comment
such as

    /* Line: 79        : CALL               : src/world/blocks.cob */

and names the target right after it:
  - a CALL to another file's program: cob_resolve_cobol ("World-GetBlock", ...)
  - a CALL to a program of the same file: a direct call, World__GetBlock (...)
  - a CALL to a system routine: cob_sys_... (the name is read from the source,
    because cobc renames some, CBL_GC_NANOSLEEP to cob_sys_oc_nanosleep)
  - a CALL through a pointer or a data item: no name, recorded as <dynamic>
  - a PERFORM: /* PERFORM StartBreakingBlock */
Programs and paragraphs are marked the same way (PROGRAM-ID, Entry, Paragraph,
Section), so each site also gets the program and paragraph that make the call.
That is the compiler's view of the call sites; compare_cobol.py diffs it
against gen-system's.

Output: one JSON object per line,
    {"file", "line", "verb", "target", "program", "paragraph", "copybook"}
where copybook is the copybook path when the statement came from a COPY, else "".
A file cobc rejects is listed in --failures with the first lines of its error.

Usage:
    cobc_edges.py --corpus DIR --out cobc.jsonl [--failures fail.jsonl]
                  [--flags "-free"] [--include PREFIX=DIR[,DIR...]]... [--skip GLOB]...
--include adds -I dirs for files whose path starts with PREFIX ("" = every file);
a DIR may be a glob, so "src/_copybooks/*" adds each subfolder.
"""
import argparse
import fnmatch
import glob
import json
import os
import re
import shlex
import subprocess
import sys
import tempfile

LINE_RE = re.compile(r'^\s*/\* Line: (\d+)\s*: (.*?)\s*: (\S.*?) \*/\s*$')
PROGRAM_RE = re.compile(r"^/\* PROGRAM-ID '([^']+)' \*/", re.M)
PERFORM_RE = re.compile(r'^\s*/\* PERFORM (\S+)')
RESOLVE_RE = re.compile(r'cob_resolve_cobol \("([^"]+)"')
SYS_RE = re.compile(r'\bcob_sys_[a-z0-9_]+ \(')
CALL_LITERAL_RE = re.compile(r'\bCALL\s+"([^"]+)"', re.I)
DYNAMIC_MARKS = ('cob_unifunc', 'cob_call_field', 'cob_resolve_cobol (cob_')


def sources(corpus, skips):
    out = []
    for dirpath, dirnames, files in os.walk(corpus):
        dirnames[:] = sorted(d for d in dirnames if not d.startswith('.'))
        for f in sorted(files):
            if not f.lower().endswith(('.cob', '.cbl')):
                continue
            rel = os.path.relpath(os.path.join(dirpath, f), corpus).replace(os.sep, '/')
            if any(fnmatch.fnmatch(rel, s) for s in skips):
                continue
            out.append(rel)
    return out


def include_dirs(corpus, rel, rules):
    dirs = []
    for prefix, patterns in rules:
        if not rel.startswith(prefix):
            continue
        for pat in patterns:
            for d in sorted(glob.glob(os.path.join(corpus, pat))):
                if os.path.isdir(d):
                    dirs.append(os.path.relpath(d, corpus).replace(os.sep, '/'))
    return dirs


class Sources:
    """Source lines by path, read once."""

    def __init__(self, corpus):
        self.corpus, self.cache = corpus, {}

    def line(self, path, lineno):
        if path not in self.cache:
            try:
                with open(os.path.join(self.corpus, path), encoding='utf-8', errors='replace') as f:
                    self.cache[path] = f.read().splitlines()
            except OSError:
                self.cache[path] = []
        lines = self.cache[path]
        return lines[lineno - 1] if 0 < lineno <= len(lines) else ''


def parse_c(text, rel, src):
    """Return one record per CALL or PERFORM statement in the generated C."""
    local = {name.replace('-', '__'): name for name in PROGRAM_RE.findall(text)}
    local_re = None
    if local:
        names = sorted(local, key=len, reverse=True)
        # World__GetBlock (...) or, for a call cobc caches, ... = World__GetBlock_0__;
        local_re = re.compile(r'(?<![A-Za-z0-9_])(' + '|'.join(map(re.escape, names)) +
                              r')(?:_\d+__)?(?![A-Za-z0-9_])')

    records, pending = [], None
    program, paragraph = '', ''

    def flush():
        nonlocal pending
        if pending and pending['target']:
            records.append(pending)
        pending = None

    for line in text.splitlines():
        m = PROGRAM_RE.match(line)
        if m:
            flush()
            program, paragraph = m.group(1), ''
            continue
        m = LINE_RE.match(line)
        if m:
            flush()
            lineno, what, path = int(m.group(1)), m.group(2).split(), m.group(3).strip()
            if not what:
                continue
            verb = what[0]
            if verb == 'Entry':
                paragraph = ''
            elif verb in ('Paragraph', 'Section') and len(what) > 1:
                paragraph = what[1]
            elif verb in ('CALL', 'PERFORM'):
                pending = {'file': rel, 'line': lineno, 'verb': verb, 'target': '',
                           'program': program, 'paragraph': paragraph,
                           'copybook': '' if path == rel else path}
            continue
        if not pending or pending['target']:
            continue
        if pending['verb'] == 'PERFORM':
            m = PERFORM_RE.match(line)
            if m:
                pending['target'] = m.group(1)
            continue
        m = RESOLVE_RE.search(line)
        if m:
            pending['target'] = m.group(1)
        elif SYS_RE.search(line):
            m = CALL_LITERAL_RE.search(src.line(pending['copybook'] or rel, pending['line']))
            pending['target'] = m.group(1) if m else '<system>'
        elif local_re and local_re.search(line):
            pending['target'] = local[local_re.search(line).group(1)]
        elif any(k in line for k in DYNAMIC_MARKS):
            pending['target'] = '<dynamic>'
    flush()
    return records


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--corpus', required=True)
    ap.add_argument('--out', required=True)
    ap.add_argument('--failures')
    ap.add_argument('--cobc', default='cobc')
    ap.add_argument('--flags', default='-free')
    ap.add_argument('--include', action='append', default=[])
    ap.add_argument('--skip', action='append', default=[])
    args = ap.parse_args()

    rules = []
    for spec in args.include:
        prefix, _, dirs = spec.partition('=')
        rules.append((prefix, [d for d in dirs.split(',') if d]))
    src = Sources(args.corpus)
    files = sources(args.corpus, args.skip)
    records, failures = [], []
    with tempfile.TemporaryDirectory() as tmp:
        c_path = os.path.join(tmp, 'out.c')
        for i, rel in enumerate(files, 1):
            cmd = [args.cobc, '-C'] + shlex.split(args.flags)
            for d in include_dirs(args.corpus, rel, rules):
                cmd += ['-I', d]
            cmd += ['-o', c_path, rel]
            p = subprocess.run(cmd, cwd=args.corpus, capture_output=True, text=True)
            if p.returncode != 0 or not os.path.exists(c_path):
                failures.append({'file': rel, 'error': (p.stderr or p.stdout).strip().splitlines()[:5]})
                continue
            with open(c_path, encoding='utf-8', errors='replace') as f:
                records.extend(parse_c(f.read(), rel, src))
            os.remove(c_path)
            if i % 50 == 0:
                print(f'cobc: {i}/{len(files)} files', file=sys.stderr)
    with open(args.out, 'w', encoding='utf-8', newline='\n') as f:
        for r in records:
            f.write(json.dumps(r, sort_keys=True) + '\n')
    if args.failures:
        with open(args.failures, 'w', encoding='utf-8', newline='\n') as f:
            for r in failures:
                f.write(json.dumps(r) + '\n')
    print(f'cobc: {len(files)} files, {len(files) - len(failures)} translated, '
          f'{len(failures)} rejected, {len(records)} CALL and PERFORM sites', file=sys.stderr)


if __name__ == '__main__':
    main()
