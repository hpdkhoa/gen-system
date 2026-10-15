#!/usr/bin/env python3
"""Diff gen-system's COBOL call edges against another tool's call sites.

Inputs
  --gen   the output of `genbench edges` (gen-system's view)
  --ref   the reference sites: cobc_edges.py or textscan_edges.py output
  --corpus the source tree both read

What it checks, on the files the reference covered:
  1. Call sites. A CALL or PERFORM site is the key (file, line, verb, target).
     Sites written in the file itself are matched one to one. Sites a COPY
     brought in are matched by (file, target), because gen-system dates them to
     the COPY line and cobc to the copybook line.
  2. Callers. On each matched site: do both name the same program and the same
     paragraph or section as the caller? (Skipped when the reference has none.)
  3. Resolution. A PERFORM must resolve to that paragraph in the same file. A
     CALL to a program the corpus defines must resolve to that program; a CALL
     to anything else (runtime routine, C helper) must stay unresolved.

gen-system also records intrinsic FUNCTION calls and EXEC blocks; they are not
CALL or PERFORM statements, so they are counted and left out.

Output: a Markdown report (--md) and the full lists as JSON (--json).
"""
import argparse
import collections
import fnmatch
import json
import os
import re

PROGRAM_ID_RE = re.compile(r'^\s*PROGRAM-ID\.\s*"?([A-Za-z0-9_-]+)', re.I)


def load_jsonl(path):
    with open(path, encoding='utf-8') as f:
        return [json.loads(line) for line in f if line.strip()]


class Source:
    def __init__(self, corpus):
        self.corpus, self.cache = corpus, {}

    def lines(self, rel):
        if rel not in self.cache:
            try:
                with open(os.path.join(self.corpus, rel), encoding='utf-8', errors='replace') as f:
                    self.cache[rel] = [l.rstrip('\n') for l in f]
            except OSError:
                self.cache[rel] = []
        return self.cache[rel]

    def line(self, rel, no):
        ls = self.lines(rel)
        return ls[no - 1] if 0 < no <= len(ls) else ''


def gen_verb(src, e):
    """Classify a gen-system edge by the source line it sits on.

    Returns (verb, target): CALL, PERFORM, FUNCTION, EXEC, COPYBOOK or OTHER."""
    name = e['callee'].upper()
    if name.startswith(('CICS.', 'SQL.')):
        return 'EXEC', name
    text = src.line(e['file'], e['line']).split('*>')[0].upper()
    n = re.escape(name)
    end = r'(?![A-Z0-9_-])'
    if re.search(r'\bCALL\s+["\']' + n + r'["\']', text):
        return 'CALL', name
    if re.search(r'\bCALL\s+' + n + end, text):
        return 'CALL', '<DYNAMIC>'  # through a data item or a program pointer
    if re.search(r'\bPERFORM\s+' + n + end, text):
        return 'PERFORM', name
    if re.search(r'FUNCTION\s+' + n + end, text):
        return 'FUNCTION', name
    if re.search(r'\bCOPY\b', text):
        return 'COPYBOOK', name
    return 'OTHER', name


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--gen', required=True)
    ap.add_argument('--ref', required=True)
    ap.add_argument('--ref-name', default='reference')
    ap.add_argument('--corpus', required=True)
    ap.add_argument('--failures', help='files the reference tool rejected (cobc_edges.py --failures)')
    ap.add_argument('--skip', action='append', default=[], help='files the reference did not read')
    ap.add_argument('--md', required=True)
    ap.add_argument('--json', required=True)
    ap.add_argument('--examples', type=int, default=25)
    ap.add_argument('--require-match', action='store_true',
                    help='exit 1 on any site, caller or resolution difference (for CI against cobc)')
    args = ap.parse_args()

    src = Source(args.corpus)
    rejected = {r['file'] for r in load_jsonl(args.failures)} if args.failures else set()
    covered = set()
    programs = set()
    for dirpath, dirnames, files in os.walk(args.corpus):
        dirnames[:] = [d for d in dirnames if not d.startswith('.')]
        for f in files:
            if not f.lower().endswith(('.cob', '.cbl')):
                continue
            rel = os.path.relpath(os.path.join(dirpath, f), args.corpus).replace(os.sep, '/')
            # Every file that defines a program counts, the skipped ones too: a
            # code generation template defines the program built from it.
            for line in src.lines(rel):
                m = PROGRAM_ID_RE.match(line)
                if m:
                    programs.add(m.group(1).upper())
            if any(fnmatch.fnmatch(rel, s) for s in args.skip) or rel in rejected:
                continue
            covered.add(rel)

    ref = [r for r in load_jsonl(args.ref) if r['file'] in covered]
    for r in ref:
        r['target'] = r['target'].upper()
        r['program'] = r.get('program', '').upper()
        r['paragraph'] = r.get('paragraph', '').upper()
    gen_all = [e for e in load_jsonl(args.gen) if e.get('lang') == 'cobol' and e['file'] in covered]
    gen = []
    left_out = collections.Counter()
    for e in gen_all:
        e['verb'], e['target'] = gen_verb(src, e)
    # A name written as FUNCTION X anywhere is an intrinsic, also where a COPY
    # brought it in and the line shows only the COPY.
    intrinsics = {e['target'] for e in gen_all if e['verb'] == 'FUNCTION'}
    for e in gen_all:
        if e['verb'] == 'COPYBOOK' and e['target'] in intrinsics:
            e['verb'] = 'FUNCTION'
        verb = e['verb']
        if verb in ('FUNCTION', 'EXEC'):
            left_out[verb] += 1
            continue
        gen.append(e)

    # 1. Sites written in the file itself.
    def key(r):
        return (r['file'], r['line'], r['verb'], r['target'])
    ref_in = collections.defaultdict(list)
    for r in ref:
        if not r.get('copybook'):
            ref_in[key(r)].append(r)
    gen_in = collections.defaultdict(list)
    for e in gen:
        if e['verb'] in ('CALL', 'PERFORM'):
            gen_in[key(e)].append(e)
    pairs, only_gen, only_ref = [], [], []
    for k in sorted(set(ref_in) | set(gen_in)):
        g, r = gen_in.get(k, []), ref_in.get(k, [])
        n = min(len(g), len(r))
        pairs += list(zip(g[:n], r[:n]))
        only_gen += g[n:]
        only_ref += r[n:]

    # Sites a COPY brought in, by (file, target).
    ref_cb = collections.Counter((r['file'], r['target']) for r in ref if r.get('copybook'))
    gen_cb = collections.Counter((e['file'], e['target']) for e in gen if e['verb'] == 'COPYBOOK')
    cb_matched = sum((ref_cb & gen_cb).values())
    cb_only_ref = sorted((ref_cb - gen_cb).elements())
    cb_only_gen = sorted((gen_cb - ref_cb).elements())
    other = [e for e in gen if e['verb'] == 'OTHER']

    # 2. Callers on matched sites.
    caller_checked = caller_agree = 0
    caller_diff = []
    for g, r in pairs:
        if not r['program']:
            continue
        caller_checked += 1
        gp, gq = g.get('caller_program', '').upper(), g.get('caller_paragraph', '').upper()
        if (gp, gq) == (r['program'], r['paragraph']):
            caller_agree += 1
        else:
            caller_diff.append({'file': g['file'], 'line': g['line'], 'target': g['target'],
                                'gen': [gp, gq], args.ref_name: [r['program'], r['paragraph']]})

    # 3. Resolution on matched sites.
    res = collections.Counter()
    res_bad = []
    for g, r in pairs:
        rid = g.get('resolved_id', '')
        if g['verb'] == 'PERFORM':
            ok = rid.startswith(g['file'] + '#') and re.search(r'[:.]' + re.escape(g['target']) + r'$', rid)
            kind = 'perform'
        elif g['target'] == '<DYNAMIC>':
            res['dynamic'] += 1
            continue
        elif g['target'] in programs:
            ok = rid.endswith('#module:' + g['target'])
            kind = 'call_to_program'
        else:
            ok = rid == '' or rid.startswith('<')
            kind = 'call_outside'
        res[kind + ('_ok' if ok else '_wrong')] += 1
        if not ok:
            res_bad.append({'file': g['file'], 'line': g['line'], 'verb': g['verb'], 'target': g['target'],
                            'resolved_id': rid, 'source': src.line(g['file'], g['line']).strip()})

    ref_in_total = sum(len(v) for v in ref_in.values())
    gen_in_total = sum(len(v) for v in gen_in.values())
    summary = {
        'reference': args.ref_name,
        'files_compared': len(covered),
        'files_rejected_by_reference': len(rejected),
        'sites_in_file': {'reference': ref_in_total, 'gen_system': gen_in_total, 'matched': len(pairs),
                          'only_reference': len(only_ref), 'only_gen_system': len(only_gen)},
        'sites_from_copybooks': {'reference': sum(ref_cb.values()), 'gen_system': sum(gen_cb.values()),
                                 'matched': cb_matched, 'only_reference': len(cb_only_ref),
                                 'only_gen_system': len(cb_only_gen)},
        'gen_system_left_out': dict(left_out),
        'gen_system_unclassified': len(other),
        'callers': {'checked': caller_checked, 'agree': caller_agree, 'differ': len(caller_diff)},
        'resolution': dict(sorted(res.items())),
    }
    show = lambda r: {**{k: r[k] for k in ('file', 'line', 'verb', 'target')},
                      'source': src.line(r['file'], r['line']).strip()}
    detail = {
        'summary': summary,
        'only_reference': [show(r) for r in only_ref],
        'only_gen_system': [show(e) for e in only_gen],
        'copybook_only_reference': cb_only_ref,
        'copybook_only_gen_system': cb_only_gen,
        'gen_system_unclassified': [show(e) for e in other],
        'caller_differences': caller_diff,
        'resolution_wrong': res_bad,
    }
    with open(args.json, 'w', encoding='utf-8', newline='\n') as f:
        json.dump(detail, f, indent=1)

    pct = lambda a, b: f'{100 * a / b:.1f}%' if b else 'n/a'
    s, c = summary['sites_in_file'], summary['sites_from_copybooks']
    md = [f'# gen-system against {args.ref_name}', '',
          f'{len(covered)} files compared; {len(rejected)} rejected by {args.ref_name} and left out.', '',
          '| | ' + args.ref_name + ' | gen-system | both | only ' + args.ref_name + ' | only gen-system |',
          '|---|---|---|---|---|---|',
          f"| CALL and PERFORM sites in the file | {s['reference']} | {s['gen_system']} | {s['matched']} "
          f"({pct(s['matched'], s['reference'])} of {args.ref_name}) | {s['only_reference']} | {s['only_gen_system']} |",
          f"| sites from copybooks, by file and target | {c['reference']} | {c['gen_system']} | {c['matched']} "
          f"| {c['only_reference']} | {c['only_gen_system']} |", '']
    if caller_checked:
        md += [f'Callers: on {caller_checked} matched sites, both name the same program and paragraph on '
               f'{caller_agree} ({pct(caller_agree, caller_checked)}).', '']
    md += ['Resolution on matched sites: ' + ', '.join(f'{k} {v}' for k, v in sorted(res.items())) + '.', '',
           'Left out of gen-system: ' + (', '.join(f'{v} {k}' for k, v in sorted(left_out.items())) or 'none') +
           f'; {len(other)} edges matched no CALL, PERFORM, FUNCTION or COPY on their line.', '']

    def section(title, rows, fmt):
        if not rows:
            return
        md.extend([f'## {title} ({len(rows)})', ''])
        md.extend(fmt(r) for r in rows[:args.examples])
        if len(rows) > args.examples:
            md.append(f'- ... {len(rows) - args.examples} more in the JSON report')
        md.append('')
    site = lambda r: f"- `{r['file']}:{r['line']}` {r['verb']} {r['target']}: `{r['source']}`"
    section(f'Only {args.ref_name}', detail['only_reference'], site)
    section('Only gen-system', detail['only_gen_system'], site)
    section(f'From copybooks, only {args.ref_name}', cb_only_ref, lambda r: f'- `{r[0]}` {r[1]}')
    section('From copybooks, only gen-system', cb_only_gen, lambda r: f'- `{r[0]}` {r[1]}')
    section('gen-system edges with no CALL, PERFORM, FUNCTION or COPY on their line', detail['gen_system_unclassified'], site)
    section('Callers that differ', caller_diff,
            lambda r: f"- `{r['file']}:{r['line']}` {r['target']}: gen-system {r['gen']}, {args.ref_name} {r[args.ref_name]}")
    section('Resolution that looks wrong', res_bad,
            lambda r: f"- `{r['file']}:{r['line']}` {r['verb']} {r['target']} -> `{r['resolved_id'] or 'unresolved'}`: `{r['source']}`")
    with open(args.md, 'w', encoding='utf-8', newline='\n') as f:
        f.write('\n'.join(md))
    print(json.dumps(summary, indent=1))
    if args.require_match:
        diffs = (len(only_ref) + len(only_gen) + len(cb_only_ref) + len(cb_only_gen) +
                 len(caller_diff) + len(res_bad))
        if diffs:
            print(f'MISMATCH: {diffs} differences against {args.ref_name}; see {args.md}')
            raise SystemExit(1)
        print(f'MATCH: gen-system agrees with {args.ref_name} on every site, caller and resolution')


if __name__ == '__main__':
    main()
