# General computational experiments

Kario's orchestration is domain independent. The three original examples are conveniences; they do not define the set of research questions. General studies use `python.experiment` and their own domain, methodology, variables, parameters, code, and outputs.

## Plan and execute

1. Create an experiment with the actual research question and optionally an uploaded CSV dataset.
2. Generate a plan using an open-weight model, or PUT a manual plan.
3. Review the hypothesis, method, assumptions, parameters, and Python source.
4. Explicitly POST `/run`. Custom code requires `WORKER_MODE=docker` and a built worker image.
5. Inspect computed results, diagnostics, and limitations. Download source and exact inputs for reproduction.

`examples/custom-plan.json` explores logistic population growth, a biology study outside the original three adapters. Other questions can use the same interface without adding another adapter.

## General plan fields

All normal Plan fields remain required. The domain can be any nonempty scientific label of at most 80 characters. Additional fields:

```json
{
  "adapter": "python.experiment",
  "domain": "biology",
  "parameters": {
    "inputs": {"initial_population": 10, "growth_rate": 0.3},
    "seed": 42,
    "required_modules": ["math"]
  },
  "code": "def run_experiment(inputs, dataset_rows):\n    ..."
}
```

Code is limited to 64,000 bytes. The seed is a nonnegative safe integer. Required module names must be valid top-level identifiers; modules unavailable in the probed worker are rejected before execution. Custom packages must be installed in the worker image before starting a run. The default worker image includes Python, NumPy and SciPy; the API discovers installed modules at startup. Restart after rebuilding the image.

The script defines:

```python
def run_experiment(inputs, dataset_rows):
    # inputs: your scientific parameters
    # dataset_rows: a list of CSV dictionaries, or [] when none is attached
    return {
        "kind": "simulation",  # also benchmark, observational_analysis, computational_experiment
        "metrics": {"actual_computed_quantity": 1.0},
        "findings": ["Explain an actual computed value here."],
        "limitations": ["State what the computation does not establish."],
        "series": [{"name": "A condition", "points": [{"x": 0, "y": 1.0}]}],
        "x_axis_label": "Time (s)",
        "y_axis_label": "Measured quantity (units)",
        "csv": "time,value\n0,1.0\n",
    }
```

Metrics must be a nonempty JSON dictionary. Findings and limitations must each contain 1–100 nonempty strings. A result may have up to eight series with 2,000 finite x/y points each. CSV is optional, limited to 8 MiB. The overall output limit is 16 MiB. Axis labels include the actual quantities and units. Values shown in the contract above are examples, not measured findings.

Do not read stdin or emit a replacement JSON result. Return the result dictionary. Print diagnostics, if necessary; stdout is redirected to the bounded worker log. Seeds initialize Python random and NumPy's legacy global RNG when declared; explicitly seed any independently constructed generators in your code.

## Isolation and limitations

The production Go executor independently refuses custom code in local mode. Docker workers have no network, no host mounts, no API/model credentials, a read-only root filesystem, an unprivileged user, dropped capabilities, a bounded temporary filesystem, CPU/memory/PID limits and a 1–300 second deadline. The worker image must already exist locally. Scripts cannot request package installation or retrieve external data during execution. Upload data first or build the needed packages into the image.

This broadens computational methods, not physical laboratory access. Missing data, specialist libraries, equipment, approvals or sufficient resources remain explicit constraints. Execution and schema checks do not establish scientific validity: review generated code and study design, verify controls and independent checks, and interpret uncertainty appropriately. Kario does not automatically change a hypothesis or significance threshold after observing outcomes.

General runs export the harness, custom `experiment.py`, exact input data, plan, results, charts, report, dependency versions and code hashes. Reproduce custom source in an isolated container with the same worker image and dependency versions, following the bundle instructions. AI interpretation is separately labeled from computed evidence.
