# Kario API v1

Base URL: `http://127.0.0.1:8080`. The React dashboard uses `/api/v1` through its Vite development proxy by default.

Use `Authorization: Bearer <API_TOKEN>` on every route except `/healthz` if a token is configured. JSON bodies require `Content-Type: application/json`; unknown fields and multiple JSON values are rejected. JSON bodies are limited to 1 MiB. Resource IDs are opaque server-generated strings.

Errors use:

```json
{"error":{"code":"state_conflict","message":"operation conflicts with experiment state"}}
```

Common statuses: `400` malformed input, `401` missing/invalid token, `403` disallowed browser origin, `404` missing resource, `409` state conflict, `413` upload too large, `415` wrong content type, `422` invalid scientific inputs or unsuccessful planning, `503` model explicitly disabled. Internal errors return `500` with details confined to server logs. An unreachable model yields an explicit `planning_failed` response and `needs_review` state.

## Experiments

| Method | Route | Behavior |
|---|---|---|
| GET | `/api/v1/experiments` | List experiments |
| POST | `/api/v1/experiments` | Create a draft; returns `201` |
| GET | `/api/v1/experiments/{id}` | Full experiment, plan, result, progress/error |
| PATCH | `/api/v1/experiments/{id}` | Change permitted fields |
| DELETE | `/api/v1/experiments/{id}` | Delete experiment, events, plans, artifact metadata and files; returns `204` |
| POST | `/api/v1/experiments/{id}/plan` | Ask configured model for a validated plan; synchronous |
| PUT | `/api/v1/experiments/{id}/plan` | Validate and version a manual plan |
| GET | `/api/v1/experiments/{id}/plans` | Immutable plan revision history |
| POST | `/api/v1/experiments/{id}/run` | Queue the saved plan; returns `202` |
| POST | `/api/v1/experiments/{id}/cancel` | Cancel queued/running/planning work |
| GET | `/api/v1/experiments/{id}/events` | Persisted events, ascending by event ID |
| GET | `/api/v1/experiments/{id}/events/stream` | Resumable server-sent events |
| GET | `/api/v1/experiments/{id}/artifacts` | List names, MIME types, byte sizes, SHA-256 hashes |
| GET | `/api/v1/experiments/{id}/artifacts/{name}` | Download a registered artifact; name includes `attempt-001/…` |
| GET | `/api/v1/experiments/{id}/report` | Download current completed attempt's Markdown report |
| GET | `/api/v1/experiments/{id}/bundle` | ZIP containing all saved attempt artifacts |

Create:

```json
{
  "question": "How does air resistance affect projectile range?",
  "title": "Projectile dynamics",
  "project_id": "prj_...",
  "dataset_id": "ds_..."
}
```

Only `question` is required (5–4000 characters). Title defaults to the question (max supplied title: 200). Optional project/dataset IDs must exist. A new draft has no plan or results. Manual plan bodies follow the complete examples in `examples/*-plan.json`. The server overwrites `source` and `model` metadata when saving plans. A manual plan can be saved for an unavailable adapter, but execution is refused until the worker dependency is available.

PATCH accepts `title`, `question`, `project_id`, and `dataset_id`. No mutation is allowed while planning, queued, or running. Changing a question or dataset invalidates a saved plan and is allowed only before the first execution attempt. Empty project/dataset strings detach them. Title and project metadata remain editable when inactive.

List query parameters:

- `status`: exact lowercase state, e.g. `completed`, `needs_review`.
- `q`: case-insensitive substring of title, question or ID.
- `project_id`: exact project ID.
- `limit`: 1–200, default 50.
- `offset`: 0–1000000, default 0.

List response: `{"data":[...],"total":24,"limit":50,"offset":0}`. Most other collection endpoints use `{"data":[...]}`. Experiment listing is newest-updated first.

State flow:

```text
draft → planning → planned → queued → running → completed
              ↘ needs_review            ↘ failed
                 queued/running/planning → cancelled
```

A manual plan moves an inactive experiment directly to `planned`. Every run requires an explicit POST; creating a plan never starts execution. Only `planned`, `failed`, or `cancelled` experiments with a valid saved plan can run. Completed studies are immutable scientifically; create a new experiment to rerun them. Duplicate queue requests conflict. Each accepted run increments `attempt`; each saved plan increments `plan_version`.

Events accept `?after=<event-id>` (nonnegative) and return up to 200 events. SSE also accepts the `Last-Event-ID` header; the query parameter takes precedence. SSE emits `id`, `event`, and JSON `data`, sends heartbeat comments, and emits `event: end` before closing on a terminal state. A stream on a draft/planned experiment waits for later changes. With bearer auth, use authenticated fetch streaming or an SSE client that supports headers; native browser EventSource does not set custom authorization headers.

Cancellation commits the cancelled state before signalling the worker. A planning HTTP request may finish its in-flight model call, but its late response cannot overwrite the cancelled state. Startup recovery marks interrupted planning/running work failed; queued work stays queued.

## Projects, datasets, reports, dashboard

| Method | Route | Behavior |
|---|---|---|
| GET | `/healthz` | Process health; no auth |
| GET | `/readyz` | Database readiness; model connectivity is explicitly unchecked |
| GET | `/api/v1/projects` | List projects |
| POST | `/api/v1/projects` | Create with `{ "name":"…", "description":"…" }` |
| GET | `/api/v1/datasets` | List dataset metadata/previews |
| POST | `/api/v1/datasets` | Upload one multipart `file` field containing a CSV |
| GET | `/api/v1/datasets/{id}` | Inspect dataset columns/preview and hash |
| GET | `/api/v1/datasets/{id}/download` | Download original normalized CSV |
| GET | `/api/v1/tools` | Adapter capabilities, dependencies, examples and availability |
| GET | `/api/v1/reports` | Completed experiment report/bundle links |
| GET | `/api/v1/dashboard` | Actual totals, status counts and 7-day creation history (UTC) |
| GET | `/api/v1/activity` | Latest 50 persisted events, newest first |
| GET | `/api/v1/settings` | Non-secret model/worker configuration and adapter availability |

Dataset upload example:

```sh
curl -F 'file=@examples/groups.csv' http://127.0.0.1:8080/api/v1/datasets
```

CSV limits: 10 MiB file, 11 MiB total multipart body, 50,000 data rows, 2–100 uniquely named columns, 10,000 characters per cell. Headers must be trimmed and nonempty. Optional UTF-8 BOM is stripped; the hash refers to stored normalized bytes. Preview includes the first five records. Upload stores data; it does not automatically run an analysis. No network dataset connectors or instrument APIs are implemented.

Reports are Markdown, plots are SVG, downloadable results are JSON/CSV. PDF rendering is not included.

## General custom experiments

The examples do not limit supported scientific domains. Use `adapter: "python.experiment"` with any nonempty domain, `code` containing `run_experiment(inputs, dataset_rows)`, and parameters `{ "inputs": {...}, "seed": 42, "required_modules": ["math"] }`. The remaining Plan fields are unchanged. See `examples/custom-plan.json` for a full biology plan.

Manual/general plans can be saved before Docker is available. `POST .../run` returns `422` with the missing worker/dependency requirement instead of running generated code on the host. Tool registry entries distinguish `category: "capability"` from `category: "example"`; the general tool reports `available_modules`. Custom result series use `x_axis_label` and `y_axis_label` and can describe arbitrary measured quantities. Source code is versioned with the plan and saved as `experiment.py` in each successful attempt bundle.
