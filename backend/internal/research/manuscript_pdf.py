"""Compile a saved manuscript in the memory-only Bubblewrap workspace."""
import base64
import json
import os
import pathlib
import re
import subprocess
import sys

request = json.load(sys.stdin)
root = pathlib.Path('/work')
(root / 'main.tex').write_text(request['source'], encoding='utf-8')
for name, encoded in request.get('figures', {}).items():
    if not re.fullmatch(r'figures/figure-0[1-3]\.pdf', name):
        raise ValueError('Invalid figure path')
    target = root / name
    target.parent.mkdir(exist_ok=True)
    target.write_bytes(base64.b64decode(encoded, validate=True))
env = dict(os.environ, openin_any='p', openout_any='p', shell_escape='f',
           TEXMFHOME='/work/texmf', TEXMFVAR='/work/texmf-var', TEXMFCONFIG='/work/texmf-config')
for _ in range(2):
    result = subprocess.run(['/usr/bin/pdflatex', '--no-shell-escape', '--interaction=nonstopmode',
                             '--halt-on-error', 'main.tex'], cwd=root, env=env,
                            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=35)
    if result.returncode:
        print(json.dumps({'error': 'The LaTeX source did not compile. Check the source and available LaTeX packages.'}))
        sys.exit(1)
pdf = (root / 'main.pdf').read_bytes()
if not pdf.startswith(b'%PDF-') or len(pdf) > 16 * 1024 * 1024:
    raise ValueError('Invalid or oversized PDF')
print(json.dumps({'pdf': base64.b64encode(pdf).decode('ascii')}))
