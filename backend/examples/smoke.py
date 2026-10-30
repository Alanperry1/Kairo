"""Run against a separately started API. Uses only Python's standard library."""
import argparse
import io
import json
import math
import os
from pathlib import Path
import tempfile
import time
import urllib.error
import urllib.request
import zipfile

parser = argparse.ArgumentParser()
parser.add_argument('--model', action='store_true', help='Request a real model plan instead of submitting the example')
args = parser.parse_args()
base = os.environ.get('API_BASE_URL', 'http://127.0.0.1:8080').rstrip('/')
token = os.environ.get('API_TOKEN', '')

def call(method, path, body=None, raw=False):
    headers = {}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers['Content-Type'] = 'application/json'
    req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=120) as response:
            output = response.read()
    except urllib.error.HTTPError as error:
        raise SystemExit(f'HTTP {error.code}: {error.read().decode()}')
    return output if raw else json.loads(output)

plan = json.loads(Path(__file__).with_name('projectile-plan.json').read_text())
experiment = call('POST', '/api/v1/experiments', {'question': plan['research_question'], 'title': 'Backend smoke test'})
path = '/api/v1/experiments/' + experiment['id']
print('Created', experiment['id'])
if args.model:
    call('POST', path + '/plan')
else:
    call('PUT', path + '/plan', plan)
call('POST', path + '/run')
for _ in range(360):
    experiment = call('GET', path)
    if experiment['status'] == 'completed':
        break
    if experiment['status'] in ('failed', 'cancelled', 'needs_review'):
        raise SystemExit(json.dumps(experiment, indent=2))
    time.sleep(.5)
else:
    raise SystemExit('Timed out waiting for completion')
metrics = experiment['result']['metrics']
assert experiment['result']['kind'] == 'simulation'
assert any(row['drag_coefficient'] == 0 for row in metrics['ranges'])
zero = next(row for row in metrics['ranges'] if row['drag_coefficient'] == 0)
assert math.isclose(zero['range_m'], metrics['analytical_no_drag_range_m'], rel_tol=.005, abs_tol=.002)
bundle = call('GET', path + '/bundle', raw=True)
with zipfile.ZipFile(io.BytesIO(bundle)) as archive:
    assert archive.testzip() is None
    assert 'attempt-001/worker.py' in archive.namelist()
output = Path(tempfile.gettempdir()) / (experiment['id'] + '-reproducibility.zip')
output.write_bytes(bundle)
print('Completed. Computed metrics:', json.dumps(metrics, indent=2))
print('Reproduction bundle:', output)
