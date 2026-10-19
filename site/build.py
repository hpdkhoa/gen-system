"""Build the static site: the landing page is hand written; this script makes the demo.

It copies the current Studio page (generator/internal/webui/studio-v23.html)
into public/demo/index.html with the demo shim and a small switcher, and
snapshots three projects into public/demo/data/ with studio-snapshot: the
hold code (Python: Pose Lab and Weapon Lab, the hand hold solver for FPS
assets), the Go shop example and the COBOL payroll example.

Usage, from the repository root:

    python site/build.py                  # clones the hold code's two repositories at their pinned commits
    python site/build.py --poselab DIR --weaponlab DIR  # uses existing checkouts at the pinned commits

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
# The hold code: two Python packages read as one project (both MIT, Khoa Hoang).
POSELAB_URL = 'https://github.com/hpdkhoa/poselab-mcp.git'
POSELAB_PIN = '66b5663775ec5ba83c1d405a385d676607cb8b8a'
WEAPONLAB_URL = 'https://github.com/hpdkhoa/weaponlab-mcp.git'
WEAPONLAB_PIN = '09e5dbaa6ee3aea0bb0a8e06f12fd2fbfd25ded1'

DEMO_HEAD = '<script src="shim.js"></script>\n<link rel="stylesheet" href="demo.css"/>\n'
DEMO_BODY = '''<div id="demo-bar">
  <a class="demo-home" href="../">gen-system</a>
  <span class="demo-tag">read only demo</span>
  <a data-p="hold" href="?p=hold">Hold code (Python)</a>
  <a data-p="shop" href="?p=shop">Go shop</a>
  <a data-p="payroll" href="?p=payroll">COBOL payroll</a>
</div>
<script>
(function () {
  var p = new URLSearchParams(location.search).get('p') || 'hold';
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
    # The badge reads "Studio"; the demo says what it is.
    html, n = re.subn(r'(<span class="logo-badge mono"[^>]*>)[^<]*(</span>)', r'\1demo\2', html, count=1)
    if n != 1:
        sys.exit('studio page: expected one logo badge')
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
    # The parser cache path is relative (.gen/ast_cache), so run from the temp
    # folder; from the repository root it would leave a .gen folder behind.
    run(cmd, cwd=tmp)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--poselab', help='an existing poselab-mcp checkout at the pinned commit')
    ap.add_argument('--weaponlab', help='an existing weaponlab-mcp checkout at the pinned commit')
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
        # the hold code: each repository's Python package side by side in one folder
        repos = {}
        for key, url, pin, given in (('poselab', POSELAB_URL, POSELAB_PIN, args.poselab),
                                     ('weaponlab', WEAPONLAB_URL, WEAPONLAB_PIN, args.weaponlab)):
            d = given or os.path.join(tmp, 'src', key + '-repo')
            if not given:
                run(['git', 'clone', '-q', '--filter=blob:none', url, d])
                run(['git', 'checkout', '-q', pin], cwd=d)
            repos[key] = d
        hold = os.path.join(tmp, 'src', 'hold')
        shutil.copytree(os.path.join(repos['poselab'], 'src', 'poselab_mcp'), os.path.join(hold, 'poselab_mcp'),
                        ignore=shutil.ignore_patterns('__pycache__', '.gen'))
        shutil.copytree(os.path.join(repos['weaponlab'], 'src', 'weaponlab'), os.path.join(hold, 'weaponlab'),
                        ignore=shutil.ignore_patterns('__pycache__', '.gen'))
        snapshot(tool, tmp, 'hold', hold, 'hold-code', stdlib='auto')
        for key in ('poselab', 'weaponlab'):
            shutil.copy(os.path.join(repos[key], 'LICENSE'), os.path.join(DEMO, 'data', 'hold', 'LICENSE-' + key + '.txt'))
    print('demo built in', DEMO)


if __name__ == '__main__':
    main()
