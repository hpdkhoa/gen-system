"""Build the static site: the landing page is hand written; this script makes the demo.

It copies the current Studio page (generator/internal/webui/studio-v23.html)
into public/demo/index.html with the demo shim and a small switcher, and
snapshots three projects into public/demo/data/ with studio-snapshot.

Usage, from the repository root:

    python site/build.py                  # clones CobolCraft at the pinned commit into a temp folder
    python site/build.py --cobolcraft DIR # uses an existing CobolCraft checkout

Needs Go and git. The snapshot loads each project from a temporary copy, so
the .gen cache the loader writes never lands in the repository.
"""
import argparse
import re
import os
import shutil
import subprocess
import sys
import tempfile

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SITE = os.path.join(REPO, 'site')
DEMO = os.path.join(SITE, 'public', 'demo')
COBOLCRAFT_URL = 'https://github.com/meyfa/CobolCraft.git'
COBOLCRAFT_PIN = 'c6f34f90ecdff94c3171c2b67334f9a5dc6acffb'  # the benchmark's pinned commit

DEMO_HEAD = '<script src="shim.js"></script>\n<link rel="stylesheet" href="demo.css"/>\n'
DEMO_BODY = '''<div id="demo-bar">
  <a class="demo-home" href="../">gen-system</a>
  <span class="demo-tag">read only demo</span>
  <a data-p="payroll" href="?p=payroll">COBOL payroll</a>
  <a data-p="shop" href="?p=shop">Go shop</a>
  <a data-p="cobolcraft" href="?p=cobolcraft">CobolCraft</a>
</div>
<script>
(function () {
  var p = new URLSearchParams(location.search).get('p') || 'payroll';
  document.querySelectorAll('#demo-bar [data-p]').forEach(function (a) {
    if (a.getAttribute('data-p') === p) a.classList.add('on');
  });
})();
</script>
'''


def run(cmd, **kw):
    print('+', ' '.join(cmd))
    subprocess.run(cmd, check=True, **kw)


def build_page():
    src = os.path.join(REPO, 'generator', 'internal', 'webui', 'studio-v23.html')
    html = open(src, encoding='utf-8').read()
    for marker in ('<head>', '<body>'):
        if html.count(marker) != 1:
            sys.exit('studio page: expected one ' + marker)
    html = html.replace('<head>', '<head>\n' + DEMO_HEAD, 1)
    html = html.replace('<body>', '<body>\n' + DEMO_BODY, 1)
    html = html.replace('<title>', '<title>Demo: ', 1)
    # The badge shows the Studio's internal version; the demo says what it is.
    html = re.sub(r'(<span class="logo-badge mono">)[^<]*(</span>)', r'\1demo\2', html, count=1)
    with open(os.path.join(DEMO, 'index.html'), 'w', encoding='utf-8', newline='\n') as f:
        f.write(html)


def snapshot(tool, tmp, name, src, display, copybooks=None, stdlib='none'):
    work = os.path.join(tmp, 'src', name)
    if os.path.abspath(src) != os.path.abspath(work):
        shutil.copytree(src, work, ignore=shutil.ignore_patterns('.git', '.gen'))
    out = os.path.join(DEMO, 'data', name)
    shutil.rmtree(out, ignore_errors=True)
    cmd = [tool, '--root', work, '--name', display, '--stdlib', stdlib, '--out', out]
    if copybooks:
        cmd += ['--copybooks', os.pathsep.join(os.path.join(work, c) for c in copybooks)]
    run(cmd)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--cobolcraft', help='an existing CobolCraft checkout at the pinned commit')
    args = ap.parse_args()
    os.makedirs(os.path.join(DEMO, 'data'), exist_ok=True)
    build_page()
    # The landing page shows the README's images; keep one source for them.
    img = os.path.join(SITE, 'public', 'img')
    shutil.rmtree(img, ignore_errors=True)
    shutil.copytree(os.path.join(REPO, 'docs', 'img'), img)
    with tempfile.TemporaryDirectory() as tmp:
        tool = os.path.join(tmp, 'studio-snapshot' + ('.exe' if os.name == 'nt' else ''))
        run(['go', 'build', '-o', tool, './generator/cmd/studio-snapshot'], cwd=REPO)
        ex = os.path.join(REPO, 'examples')
        snapshot(tool, tmp, 'payroll', os.path.join(ex, 'payroll'), 'examples/payroll',
                 copybooks=['copybooks'], stdlib='auto')
        snapshot(tool, tmp, 'shop', os.path.join(ex, 'shop'), 'examples/shop')
        cc = args.cobolcraft
        if not cc:
            cc = os.path.join(tmp, 'src', 'cobolcraft')
            run(['git', 'clone', '-q', '--filter=blob:none', COBOLCRAFT_URL, cc])
            run(['git', 'checkout', '-q', COBOLCRAFT_PIN], cwd=cc)
        snapshot(tool, tmp, 'cobolcraft', cc, 'CobolCraft',
                 copybooks=[os.path.join('src', '_copybooks'), os.path.join('codegen', '_copybooks')],
                 stdlib='auto')
        shutil.copy(os.path.join(cc, 'LICENSE'), os.path.join(DEMO, 'data', 'cobolcraft', 'LICENSE.txt'))
    print('demo built in', DEMO)


if __name__ == '__main__':
    main()
