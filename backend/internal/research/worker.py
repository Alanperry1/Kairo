"""Kairo trusted adapters. Reads one JSON request from stdin, emits one JSON result.
Replay: python3 worker.py < input.json > results.json
Custom programs run only inside an isolated worker container.
"""
import contextlib
import importlib.util
import importlib.metadata
import csv
import io
import json
import math
import os
import platform
import random
import statistics
import sys
import time
import pathlib
import shutil
import subprocess
import tempfile

# Defense in depth for a fixed, trusted worker. This is not an arbitrary-code sandbox.
try:
    import resource
    resource.setrlimit(resource.RLIMIT_CPU, (310, 310))
    resource.setrlimit(resource.RLIMIT_FSIZE, (16 * 1024 ** 2, 16 * 1024 ** 2))
    resource.setrlimit(resource.RLIMIT_NOFILE, (256, 256))
except ImportError:
    pass


def csv_text(header, rows):
    stream = io.StringIO()
    writer = csv.writer(stream)
    writer.writerow(header)
    writer.writerows(rows)
    return stream.getvalue()


def projectile(p):
    v, angle, g = p['velocity_m_s'], math.radians(p['angle_degrees']), p['gravity_m_s2']
    dt = p['time_step_s']
    def integrate(cd, step, collect):
        k = .5 * p['air_density_kg_m3'] * cd * p['area_m2'] / p['mass_kg']
        state = (0., 0., v * math.cos(angle), v * math.sin(angle))
        samples = [(0., *state[:2])]
        stride = max(1, math.ceil(p['max_time_s'] / step / 1500))
        def derivative(s):
            speed = math.hypot(s[2], s[3])
            return (s[2], s[3], -k * speed * s[2], -g - k * speed * s[3])
        for n in range(math.ceil(p['max_time_s'] / step)):
            old = state
            a = derivative(old)
            b = derivative(tuple(old[i] + step * a[i] / 2 for i in range(4)))
            c = derivative(tuple(old[i] + step * b[i] / 2 for i in range(4)))
            d = derivative(tuple(old[i] + step * c[i] for i in range(4)))
            state = tuple(old[i] + step * (a[i] + 2*b[i] + 2*c[i] + d[i]) / 6 for i in range(4))
            if not all(math.isfinite(x) for x in state):
                raise ValueError('Numerical instability: reduce time_step_s or drag stiffness')
            if state[1] < 0:
                fraction = old[1] / (old[1] - state[1])
                x = old[0] + fraction * (state[0] - old[0])
                t = (n + fraction) * step
                samples.append((t, x, 0.))
                return x, t, samples
            if collect and (n + 1) % stride == 0:
                samples.append(((n + 1) * step, *state[:2]))
        raise ValueError('Flight did not finish before max_time_s; explicitly increase max_time_s')
    rows, series, ranges = [], [], []
    analytical = v*v*math.sin(2*angle)/g
    for cd in p['drag_coefficients']:
        distance, flight, samples = integrate(cd, dt, True)
        refined, _, _ = integrate(cd, dt / 2, False)
        if abs(distance-refined) > max(.002, abs(refined)*.005):
            raise ValueError('Time-step convergence check failed; reduce time_step_s')
        if cd == 0 and abs(distance-analytical) > max(.002, analytical*.005):
            raise ValueError('No-drag analytical control check failed')
        ranges.append({'drag_coefficient': cd, 'range_m': distance, 'flight_time_s': flight,
                       'half_step_range_m': refined, 'step_difference_m': abs(distance-refined)})
        rows.extend((cd, t, x, y) for t, x, y in samples)
        series.append({'name': f'Cd = {cd:g}', 'points': [{'x': x, 'y': y} for _, x, y in samples]})
    return {'kind': 'simulation', 'metrics': {'ranges': ranges, 'analytical_no_drag_range_m': analytical,
             'integrator': 'RK4', 'time_step_s': dt, 'deterministic_runs': len(ranges), 'independent_replications': 0},
            'findings': [f"Cd={x['drag_coefficient']:g}: range {x['range_m']:.4f} m; flight {x['flight_time_s']:.4f} s." for x in ranges],
            'limitations': ['Numerical simulation, not physical measurements.',
                'Constant quadratic drag, still air, flat ground, no spin; launch and impact heights are equal.',
                'Time-step differences are numerical diagnostics, not statistical confidence intervals.',
                'Trajectory CSV is sampled to at most approximately 1502 points per coefficient.'],
            'series': series, 'csv': csv_text(['drag_coefficient', 'time_s', 'x_m', 'y_m'], rows)}


def insertion_sort(a):
    for i in range(1, len(a)):
        value, j = a[i], i-1
        while j >= 0 and a[j] > value:
            a[j+1] = a[j]
            j -= 1
        a[j+1] = value
    return a


def merge_sort(a):
    if len(a) < 2:
        return a
    middle = len(a)//2
    left, right = merge_sort(a[:middle]), merge_sort(a[middle:])
    out, i, j = [], 0, 0
    while i < len(left) and j < len(right):
        if left[i] <= right[j]:
            out.append(left[i]); i += 1
        else:
            out.append(right[j]); j += 1
    return out + left[i:] + right[j:]


def benchmark(p):
    rng = random.Random(p['seed'])
    algorithms = {'insertion_sort': insertion_sort, 'merge_sort': merge_sort, 'builtin_sorted': sorted}
    rows, summaries = [], []
    for fn in algorithms.values():
        fn([3, 2, 1])  # untimed warmup
    for size in p['sizes']:
        timings = {name: [] for name in algorithms}
        for trial in range(p['trials']):
            data = [rng.randint(-size, size) for _ in range(size)]
            expected = sorted(data)
            names = list(algorithms)
            rng.shuffle(names)
            for name in names:
                work = data.copy()  # copying excluded from timed region
                start = time.perf_counter_ns()
                actual = algorithms[name](work)
                elapsed = (time.perf_counter_ns()-start)/1_000_000
                if actual != expected:
                    raise ValueError(f'{name} failed correctness check')
                timings[name].append(elapsed)
                rows.append((name, size, trial+1, elapsed))
        for name, values in timings.items():
            summaries.append({'algorithm': name, 'size': size, 'trials': len(values),
                'median_ms': statistics.median(values), 'mean_ms': statistics.mean(values),
                'stdev_ms': statistics.stdev(values), 'min_ms': min(values), 'max_ms': max(values)})
    return {'kind': 'benchmark', 'metrics': {'summaries': summaries, 'seed': p['seed'], 'correctness_checks_passed': True},
        'findings': [f"n={size}: lowest median runtime was {min((s for s in summaries if s['size']==size), key=lambda s:s['median_ms'])['algorithm']}." for size in p['sizes']],
        'limitations': ['Wall-clock timings depend on hardware, runtime, and concurrent system load.',
            'Python built-in sorted is implemented differently from the pure-Python teaching algorithms; this is a practical implementation comparison.',
            'Inputs are paired across algorithms; order is shuffled. Reported spread describes repeated timings, not universal performance.'],
        'series': [{'name': name, 'points': [{'x': s['size'], 'y': s['median_ms']} for s in summaries if s['algorithm']==name]} for name in algorithms],
        'csv': csv_text(['algorithm', 'input_size', 'trial', 'elapsed_ms'], rows)}


def welch(p, rows):
    import scipy
    from scipy import stats
    a, b = [], []
    for row in rows:
        group = row[p['group_column']]
        if group not in (p['group_a'], p['group_b']):
            raise ValueError('Dataset contains groups outside the two pre-specified groups; filter explicitly before upload')
        try:
            value = float(row[p['value_column']])
        except (ValueError, KeyError):
            raise ValueError('Every selected value must be numeric; missing values must be handled explicitly')
        if not math.isfinite(value):
            raise ValueError('Non-finite observations are not allowed')
        (a if group == p['group_a'] else b).append(value)
    if len(a) < 3 or len(b) < 3:
        raise ValueError('Each group needs at least 3 observations')
    va, vb = statistics.variance(a), statistics.variance(b)
    se2 = va/len(a) + vb/len(b)
    denom = (va/len(a))**2/(len(a)-1) + (vb/len(b))**2/(len(b)-1)
    if se2 <= 0 or denom <= 0:
        raise ValueError('Both groups have zero variance; Welch inference is undefined')
    df = se2**2/denom
    delta = statistics.mean(a)-statistics.mean(b)
    test = stats.ttest_ind(a, b, equal_var=False, alternative='two-sided')
    critical = stats.t.ppf(.975, df)
    ci = [delta-critical*math.sqrt(se2), delta+critical*math.sqrt(se2)]
    shapiro = {}
    for name, values in [(p['group_a'], a), (p['group_b'], b)]:
        shapiro[name] = float(stats.shapiro(values).pvalue) if 3 <= len(values) <= 5000 and len(set(values)) > 1 else None
    return {'kind': 'observational_analysis', 'metrics': {'test': 'Welch two-sided t-test', 'alpha': p['alpha'],
        'group_a': p['group_a'], 'group_b': p['group_b'], 'n_a': len(a), 'n_b': len(b),
        'mean_a': statistics.mean(a), 'mean_b': statistics.mean(b), 'mean_difference_a_minus_b': delta,
        'confidence_level': .95, 'confidence_interval': ci, 't_statistic': float(test.statistic),
        'degrees_of_freedom': df, 'p_value': float(test.pvalue), 'shapiro_diagnostic_p_values': shapiro},
        'findings': [f'Mean difference (A minus B): {delta:.6g}; 95% CI [{ci[0]:.6g}, {ci[1]:.6g}].',
            f'Two-sided Welch p-value: {float(test.pvalue):.6g}; alpha was pre-specified as 0.05.'],
        'limitations': ['Observational group comparison does not establish causality.',
            'Independence, representative sampling, and lack of confounding require study-design review; they cannot be verified from this CSV.',
            'Welch t-test is pre-specified, not selected by a normality pre-test. Shapiro p-values are descriptive diagnostics only.',
            'Small samples, skewness, outliers and multiple testing may undermine inference; review distributions and study context.'],
        'series': [{'name': 'Group mean', 'points': [{'x': 0, 'y': statistics.mean(a)}, {'x': 1, 'y': statistics.mean(b)}]}],
        'csv': csv_text(['group', 'value'], [(p['group_a'], x) for x in a]+[(p['group_b'], x) for x in b]),
        'dependencies': {'scipy': scipy.__version__}}


def custom_experiment(plan, rows):
    if os.environ.get('KAIRO_ISOLATED_WORKER') != '1':
        raise ValueError('Custom code requires an isolated Docker worker')
    config = plan['parameters']
    required = config.get('required_modules', [])
    for name in required:
        if importlib.util.find_spec(name) is None:
            raise ValueError(f'Required module {name!r} is not installed in the worker image')
    loaded_before = set(sys.modules)
    random.seed(config['seed'])
    if 'numpy' in required or 'scipy' in required:
        import numpy
        numpy.random.seed(config['seed'] % (2**32))
    namespace = {'__name__': 'kairo_experiment'}
    # The Go runner permits this branch only in a network-disabled container,
    # with no host mounts or secrets and fixed process/memory/CPU limits.
    with contextlib.redirect_stdout(sys.stderr):
        exec(compile(plan['code'], 'experiment.py', 'exec'), namespace)
        function = namespace.get('run_experiment')
        if not callable(function):
            raise ValueError('Define run_experiment(inputs, dataset_rows)')
        result = function(config['inputs'], rows)
    validate_custom_result(result)
    versions = {}
    distributions = importlib.metadata.packages_distributions()
    used = {name.split('.')[0] for name in set(sys.modules)-loaded_before} | set(required)
    for name in used:
        for distribution in distributions.get(name, []):
            versions[distribution] = importlib.metadata.version(distribution)
    result['dependencies'] = versions
    result['limitations'].append('Custom experiment code requires methodological review; execution validates implemented computations, not the scientific truth of the model.')
    return result


def validate_custom_result(result):
    if not isinstance(result, dict):
        raise ValueError('run_experiment must return a result dictionary')
    if result.get('kind') not in ('simulation', 'benchmark', 'observational_analysis', 'computational_experiment'):
        raise ValueError('Result kind must identify the computational study type')
    if not isinstance(result.get('metrics'), dict) or not result['metrics']:
        raise ValueError('Computed metrics must be a nonempty dictionary')
    for field in ('findings', 'limitations'):
        values = result.get(field)
        if not isinstance(values, list) or not values or len(values) > 100 or not all(isinstance(v, str) and 0 < len(v) <= 10000 for v in values):
            raise ValueError(f'Result {field} must contain 1–100 nonempty strings')
    series = result.get('series', [])
    if not isinstance(series, list) or len(series) > 8:
        raise ValueError('At most 8 plot series are allowed')
    for line in series:
        if not isinstance(line, dict) or not isinstance(line.get('name'), str) or not isinstance(line.get('points'), list) or len(line['points']) > 2000:
            raise ValueError('Each series needs a name and at most 2000 x/y points')
        for point in line['points']:
            if not isinstance(point, dict) or not all(isinstance(point.get(k), (int, float)) and math.isfinite(point[k]) for k in ('x','y')):
                raise ValueError('Plot points must contain finite numeric x and y values')
    for axis in ('x_axis_label', 'y_axis_label'):
        if not isinstance(result.get(axis, ''), str) or len(result.get(axis, '')) > 150:
            raise ValueError('Axis labels must be strings of at most 150 characters')
    if not isinstance(result.get('csv', ''), str) or len(result.get('csv', '')) > 8*1024*1024:
        raise ValueError('CSV output must be a string of at most 8 MiB')


def runtime_catalog():
    # Configuration is part of the image/reproduction bundle, never supplied by a plan.
    for path in (pathlib.Path('/opt/kairo/runtimes.json'), pathlib.Path('runtimes.json')):
        if path.exists():
            return json.loads(path.read_text())
    return {}


def capabilities():
    modules = set(sys.stdlib_module_names) | set(importlib.metadata.packages_distributions())
    modules = sorted(name for name in modules if name.isidentifier() and not name.startswith('_') and importlib.util.find_spec(name))
    languages = [{'id': 'python', 'available': True, 'version': platform.python_version(), 'modules': modules, 'libraries': [name for name in modules if name not in sys.stdlib_module_names], 'extension': 'py'}]
    for language, spec in runtime_catalog().items():
        installed = shutil.which(spec['version'][0]) is not None
        version = ''
        packages = spec.get('modules', [])
        if installed:
            try:
                version = subprocess.check_output(spec['version'], stderr=subprocess.STDOUT, text=True, timeout=5).strip().split('\n')[0]
                if language == 'javascript':
                    raw = subprocess.check_output(['node', '-e', 'console.log(JSON.stringify(require("module").builtinModules))'], text=True, timeout=5)
                    packages = sorted(set(name.removeprefix('node:') for name in json.loads(raw)))
                if language == 'go':
                    packages = subprocess.check_output(['go', 'list', 'std'], text=True, timeout=10).splitlines()
                if language == 'cpp':
                    headers = set(packages)
                    for directory in pathlib.Path('/usr/include/c++').glob('*'):
                        if directory.is_dir():
                            headers.update(header.name for header in directory.iterdir() if header.is_file())
                    packages = sorted(headers)
                if language == 'r':
                    raw = subprocess.check_output(['Rscript', '--vanilla', '-e', 'p<-installed.packages();cat(jsonlite::toJSON(as.list(setNames(as.character(p[,"Version"]),p[,"Package"])),auto_unbox=TRUE))'], text=True, timeout=5)
                    packages = list(json.loads(raw))
            except (subprocess.SubprocessError, ValueError):
                installed = False
        languages.append({'id': language, 'available': installed, 'version': version, 'modules': packages if installed else [], 'libraries': (packages if language == 'r' else (['nlohmann/json.hpp'] if language == 'cpp' else [])) if installed else [], 'extension': spec['extension']})
    return {'modules': modules, 'languages': languages}


def language_experiment(plan, rows):
    if os.environ.get('KAIRO_ISOLATED_WORKER') != '1':
        raise ValueError('Custom code requires an isolated Docker worker')
    language = plan['language']
    spec = runtime_catalog().get(language)
    if not spec or not shutil.which(spec['version'][0]):
        raise ValueError(f'Language {language!r} is not installed in the worker image')
    config = plan['parameters']
    # No shell interpolation. All commands come from the image-owned catalog.
    with tempfile.TemporaryDirectory(prefix='kairo-') as directory:
        source = pathlib.Path(directory) / ('experiment.' + spec['extension'])
        source.write_text(plan['code'])
        binary = pathlib.Path(directory) / 'experiment'
        def command(values):
            return [value.replace('{source}', str(source)).replace('{binary}', str(binary)) for value in values]
        if spec.get('build'):
            subprocess.run(command(spec['build']), check=True, stdout=sys.stderr, stderr=sys.stderr)
        payload = json.dumps({'inputs': config['inputs'], 'dataset_rows': rows, 'seed': config['seed']})
        # Spool stdout to disk: RLIMIT_FSIZE prevents an unbounded allocation.
        with tempfile.TemporaryFile() as output:
            subprocess.run(command(spec['run']), input=payload.encode(), check=True, stdout=output, stderr=sys.stderr)
            output.seek(0)
            result = json.load(output)
    validate_custom_result(result)
    result['language'] = language
    result['runtime_version'] = subprocess.check_output(spec['version'], stderr=subprocess.STDOUT, text=True, timeout=5).strip().split('\n')[0]
    result['dependencies'] = {language: result['runtime_version']}
    if language == 'r':
        raw = subprocess.check_output(['Rscript', '--vanilla', '-e', 'p<-installed.packages();cat(jsonlite::toJSON(as.list(setNames(as.character(p[,"Version"]),p[,"Package"])),auto_unbox=TRUE))'], text=True, timeout=5)
        installed = json.loads(raw)
        result['dependencies'].update({name: installed[name] for name in config.get('required_modules', []) if name in installed})
    result['limitations'].append('Custom experiment code requires methodological review; execution checks computations, not the scientific truth of the model.')
    return result


def main():
    request = json.load(sys.stdin)
    plan, rows = request['plan'], request.get('dataset_rows', [])
    adapter, p = plan['adapter'], plan['parameters']
    if adapter == 'python.experiment':
        result = custom_experiment(plan, rows)
    elif adapter == 'language.experiment':
        result = language_experiment(plan, rows)
    elif adapter == 'physics.projectile':
        result = projectile(p)
    elif adapter == 'cs.sorting':
        result = benchmark(p)
    elif adapter == 'statistics.welch':
        result = welch(p, rows)
    else:
        raise ValueError('Unknown registered adapter')
    result.setdefault('language', 'python')
    result.setdefault('runtime_version', platform.python_version())
    result['python_version'] = platform.python_version()
    result.setdefault('dependencies', {})
    result['dependencies']['python'] = platform.python_version()
    result['narrative_source'] = 'deterministic'
    result['warnings'] = []
    json.dump(result, sys.stdout, allow_nan=False)

if __name__ == '__main__':
    try:
        if "--capabilities" in sys.argv:
            json.dump(capabilities(), sys.stdout)
        else:
            main()
    except Exception as exc:
        print(f'{type(exc).__name__}: {exc}', file=sys.stderr)
        sys.exit(1)
