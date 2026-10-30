"""Kairo's fixed Matplotlib renderer. It accepts recorded data, never Python code.
Replay: python3 figures.py --export < figure-data.json
"""
import base64
import io
import json
import math
import platform
import sys
import textwrap
from pathlib import Path

import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt

plt.rcParams.update({
    'font.family': 'DejaVu Sans', 'font.size': 10, 'text.usetex': False,
    'text.parse_math': False, 'pdf.fonttype': 42, 'axes.spines.top': False,
    'axes.spines.right': False, 'axes.edgecolor': '#bdc9c1',
    'axes.labelcolor': '#344c3d', 'text.color': '#2c4435',
    'xtick.color': '#58705f', 'ytick.color': '#58705f', 'grid.color': '#e7ede9',
})
colors = ['#3f8057', '#3e7ca6', '#b5803c', '#8576a6', '#a8584f', '#428b8b', '#77764d', '#8b637c']
request = json.load(sys.stdin)
if not isinstance(request.get('figures'), list) or not 1 <= len(request['figures']) <= 3:
    raise ValueError('Supply 1–3 evidence figures')
output = []
for index, spec in enumerate(request['figures'], 1):
    if spec['kind'] in ('bar', 'histogram') and not all(math.isfinite(v) for v in spec['values']):
        raise ValueError('Plot values must be finite')
    fig, ax = plt.subplots(figsize=(7.2, 4.5), layout='constrained')
    ax.set_axisbelow(True)
    ax.grid(axis='y', linewidth=0.7)
    if spec['kind'] in ('line', 'scatter'):
        for i, series in enumerate(spec['series']):
            x = [point['x'] for point in series['points']]
            y = [point['y'] for point in series['points']]
            if not all(math.isfinite(v) for v in x + y):
                raise ValueError('Plot values must be finite')
            if spec['kind'] == 'line':
                ax.plot(x, y, label=series['name'], color=colors[i % len(colors)], linewidth=1.8,
                        linestyle=['-', '--', '-.', ':'][i % 4], marker='o' if len(x) < 12 else None, markersize=3)
            else:
                ax.scatter(x, y, label=series['name'], color=colors[i % len(colors)], s=14, alpha=0.8)
        if len(spec['series']) > 1:
            ax.legend(frameon=False, fontsize=8, loc='best')
    elif spec['kind'] == 'bar':
        ax.bar(spec['labels'], spec['values'], color=colors[0], width=0.65)
        ax.tick_params(axis='x', labelsize=8, labelrotation=25 if len(spec['labels']) > 4 else 0)
        low, high = min(spec['values']), max(spec['values'])
        ax.set_ylim(bottom=min(0, low * 1.12), top=max(0, high * 1.12) or 1)
    elif spec['kind'] == 'histogram':
        ax.hist(spec['values'], bins=min(40, max(5, int(math.sqrt(len(spec['values']))))),
                color=colors[0], edgecolor='white', linewidth=0.8)
    else:
        raise ValueError('Unsupported figure type')
    ax.set_title(textwrap.fill(spec['title'], 65), loc='left', fontsize=12, pad=16)
    ax.set_xlabel(textwrap.fill(spec['x_label'], 55), labelpad=9)
    ax.set_ylabel(textwrap.fill(spec['y_label'], 45), labelpad=9)
    images = {}
    for extension in ('png', 'pdf'):
        buffer = io.BytesIO()
        metadata = {'Software': 'Kairo / Matplotlib'} if extension == 'png' else {'Creator': 'Kairo / Matplotlib', 'CreationDate': None}
        fig.savefig(buffer, format=extension, dpi=180, facecolor='white', metadata=metadata)
        raw = buffer.getvalue()
        images[extension] = base64.b64encode(raw).decode('ascii') if '--export' not in sys.argv else ''
        if '--export' in sys.argv:
            Path('figures').mkdir(exist_ok=True)
            Path(f'figures/figure-{index:02d}.{extension}').write_bytes(raw)
    plt.close(fig)
    output.append({'id': spec['id'], **images})
if '--export' in sys.argv:
    print(f'Wrote {len(output)} evidence figures (PNG and PDF) to figures/.')
else:
    json.dump({'version': matplotlib.__version__, 'python': platform.python_version(), 'figures': output}, sys.stdout)
