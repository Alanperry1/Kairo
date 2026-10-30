# Kario backend

A Go API for general computational research, backed by PostgreSQL and isolated Python workers. It powers the React dashboard through the local Vite proxy.

## Setup

Follow the [root README](../README.md#run-locally) to configure the single root `.env`, connect your remote PostgreSQL database (such as Neon), build the Docker worker, and start both services. Neon deploy writes `DATABASE_URL` and `DATABASE_URL_UNPOOLED` into that same `.env`. The Go API uses the direct URL for schema initialization and the pooled URL for normal queries, keeping database credentials on the server.

From the project root, start the API with:

```sh
cd backend
go run -buildvcs=false ./cmd/server
```

The default listener is `127.0.0.1:8080`. `GET /healthz` checks process health. The startup log identifies the loaded env file. Both services use the root `.env`; no backend-specific env file is needed.

## Architecture

| Component | Responsibility |
| --- | --- |
| Go HTTP API | Experiments, projects, datasets, results, downloads, and configuration |
| PostgreSQL | Experiment state, immutable plan versions, queue, events, and artifact metadata |
| Model client | Ollama or OpenAI-compatible planning and evidence interpretation |
| Registry and validator | Execution capabilities, schemas, dependencies, and resource checks |
| Background workers | Durable job claiming, progress, deadlines, cancellation, and recovery |
| Python worker | Scientific computations and structured results |
| Artifact storage | Source, inputs, results, charts, reports, hashes, and reproduction bundles |

Use one API process per database and data directory. Multiple worker goroutines are supported inside that process; this is not a distributed queue or multi-user account system.

## General experiments

The `python.experiment` adapter accepts a scientific domain, methodology, assumptions, parameters, seed, dependency declarations, and custom Python code. The code defines:

```python
def run_experiment(inputs, dataset_rows):
    # Compute and return metrics, findings, limitations, and optional plot/CSV data.
    ...
```

The method is shaped around the actual question. The planner does not substitute one of the examples for an unrelated study. See [GENERAL_EXPERIMENTS.md](GENERAL_EXPERIMENTS.md) for the full contract and [custom-plan.json](examples/custom-plan.json) for a biology study.

Plans can be saved and reviewed before a worker is available. Execution requires an available isolated Docker worker and installed dependencies. With AI disabled, users can save research questions and submit manual plans.

## Optional example adapters

| Adapter | Example | Dependencies |
| --- | --- | --- |
| `physics.projectile` | RK4 trajectories, a no-drag control, and a half-step numerical check | Python standard library |
| `cs.sorting` | Paired workloads, shuffled order, correctness checks, and repeated timings | Python standard library |
| `statistics.welch` | Two-sided Welch test, raw mean difference, and a 95% interval | SciPy |

These examples remain available for demonstrations and existing plans. Custom experiments are not limited to these methods. The synthetic [groups.csv](examples/groups.csv) can be used with the statistical example.

`WORKER_MODE=local` permits only these fixed scripts. For the statistical example in local mode, install [requirements-worker.txt](requirements-worker.txt) into a dedicated virtual environment and set `PYTHON_BIN` to that interpreter in the root `.env`.

## Execution and artifacts

Custom code runs only in Docker. The API supplies no host mounts, backend credentials, or network access to the worker. The container runs as an unprivileged user with a read-only filesystem, a bounded temporary filesystem, and CPU, memory, process, and timeout limits. The image must exist before execution; jobs do not install packages.

A successful attempt saves:

- `plan.json` and `input.json`: reviewed method, parameters, and selected data.
- `worker.py` and, for custom studies, `experiment.py`: exact execution source.
- `results.json`, `data.csv`, `chart.svg`, and `report.md`: computed evidence and explanations.
- `provenance.json`, `requirements.txt`, and `checksums.json`: runtime versions, dependency information, seeds, and hashes.
- `REPRODUCE.md`: instructions for rerunning the study.

Each attempt has its own directory. Reports are Markdown and charts are SVG. PDF rendering is not implemented. AI interpretation is labeled separately from computed evidence.

Interrupted planning/running jobs become failed on restart; queued jobs remain queued. Replanning and retries require explicit requests. Completed experiments cannot be rerun or replanned; create another experiment for a new study.

## API and authentication

See [API.md](API.md) for endpoints, request bodies, state transitions, errors, and event streaming.

If `API_TOKEN` is set, every route except `/healthz` requires its bearer token. The local Vite proxy reads the same token from the root `.env` automatically. A non-loopback API listener requires a token with at least 24 characters. Model credentials stay on the server and are not returned by settings endpoints.

Health/readiness checks do not verify live model connectivity. Missing models, dependencies, data, or resources are reported explicitly.

## Checks

From `backend/`:

```sh
TEST_DATABASE_URL="YOUR_TEST_DATABASE_URL" go test -race ./...
go vet ./...
go build -buildvcs=false -o researchos ./cmd/server
```

Include real Docker execution after building the worker image:

```sh
KAIRO_TEST_DOCKER=1 TEST_DATABASE_URL="YOUR_TEST_DATABASE_URL" go test -race ./...
```

Tests cover custom scientific domains, Docker-only execution, installed dependencies, arbitrary dataset columns, real custom worker execution when enabled, source export, numerical results, plan history, persistence, recovery, cancellation, auth, and model protocols. Model protocol tests use mock endpoints; they do not verify live model quality.

For an optional direct API smoke test, run `python3 examples/smoke.py` from this directory with the API already running. This script reads `API_TOKEN` and `API_BASE_URL` from its process environment, not from `.env`; pass a token when API authentication is enabled. The root `npm run test:integration` uses the authenticated Vite proxy instead.

Database tests create and drop isolated schemas. They use `TEST_DATABASE_URL` from the shell or root `.env`; use a separate remote test database or Neon branch with a direct connection. Without it, database tests are skipped. See the [SQLite migration instructions](../README.md#moving-existing-sqlite-records) to preserve old records.
