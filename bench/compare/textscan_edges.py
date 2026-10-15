#!/usr/bin/env python3
"""A crude reference: find CALL "name" and PERFORM name with a regular expression.

No parser, no copybooks, no program or paragraph. It exists so the comparison
has a floor that shares no code with gen-system or cobc. It is wrong on purpose
in known ways: it reads PERFORM X(I) TIMES as a call, and it sees nothing that
a COPY brings in. Output has the same fields as cobc_edges.py.

Usage: textscan_edges.py --corpus DIR --out text.jsonl [--skip GLOB]...
"""
import argparse
import fnmatch
import json
import os
import re

CALL_RE = re.compile(r'\bCALL\s+"([^"]+)"', re.I)
PERFORM_RE = re.compile(r'^\s*PERFORM\s+([A-Za-z0-9-]+)\.?(?:\s+(\S+))?', re.I)
NOT_A_PARAGRAPH = {'UNTIL', 'VARYING', 'WITH', 'TEST', 'FUNCTION', 'THRU', 'THROUGH', 'TIMES'}


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--corpus', required=True)
    ap.add_argument('--out', required=True)
    ap.add_argument('--skip', action='append', default=[])
    args = ap.parse_args()
    records = []
    for dirpath, dirnames, files in os.walk(args.corpus):
        dirnames[:] = sorted(d for d in dirnames if not d.startswith('.'))
        for f in sorted(files):
            if not f.lower().endswith(('.cob', '.cbl')):
                continue
            rel = os.path.relpath(os.path.join(dirpath, f), args.corpus).replace(os.sep, '/')
            if any(fnmatch.fnmatch(rel, s) for s in args.skip):
                continue
            with open(os.path.join(dirpath, f), encoding='utf-8', errors='replace') as src:
                for no, line in enumerate(src, 1):
                    s = line.rstrip('\n')
                    fixed_comment = len(s) > 6 and s[6] == '*' and s[:6].strip().isdigit()
                    if s.strip().startswith('*>') or fixed_comment:
                        continue
                    s = s.split('*>')[0]
                    base = {'file': rel, 'line': no, 'program': '', 'paragraph': '', 'copybook': ''}
                    for m in CALL_RE.finditer(s):
                        records.append(dict(base, verb='CALL', target=m.group(1)))
                    m = PERFORM_RE.match(s)
                    if m:
                        name, after = m.group(1).upper(), (m.group(2) or '').upper().rstrip('.')
                        if name not in NOT_A_PARAGRAPH and not name.isdigit() and after != 'TIMES':
                            records.append(dict(base, verb='PERFORM', target=m.group(1)))
    with open(args.out, 'w', encoding='utf-8', newline='\n') as out:
        for r in records:
            out.write(json.dumps(r, sort_keys=True) + '\n')
    print(f'textscan: {len(records)} CALL and PERFORM sites')


if __name__ == '__main__':
    main()
