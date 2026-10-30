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
| POST | `/api/v1/experiments/{id}/plan/stream` | Stream stages and draft output, then return the validated saved plan |
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

## Live task streams

`POST /api/v1/assistant/chat/stream`, `POST /api/v1/experiments/{id}/plan/stream`, and `POST /api/v1/experiments/{id}/documents/stream` accept the same bodies and bearer authentication as their JSON equivalents. Responses use `text/event-stream` with JSON `data` and these event names:

- `status`: `{ "message": "Writing the manuscript draft" }`.
- `model_start`: `{ "provider": "google", "model": "..." }`; a new attempt resets the draft preview.
- `delta`: `{ "text": "..." }`, incremental model output. JSON fields may be incomplete until generation finishes.
- `heartbeat`: `{}` every eight seconds while the task is pending.
- `done`: `{ "status": 201, "result": { ... } }`, the original endpoint result after validation and saving.
- `error`: `{ "status": 422, "error": { "code": "...", "message": "..." } }`; a failed task does not emit `done`.

After streaming headers are sent, logical failure statuses appear in `error` events rather than the HTTP status. Do not treat an HTTP 200, partial draft, or disconnected stream as successful completion. Provider reasoning fields are excluded. Disconnecting cancels the request's upstream model call; check saved state before retrying if saving had begun. These POST streams are not resumable; durable execution events use the separate resumable GET endpoint above. Disable proxy response buffering for SSE.

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

CSV limits: 5 GB (5,000,000,000 bytes) per file, no stored row-count cap, 2–100 uniquely named trimmed columns, 10,000 characters per cell, physical lines up to 1 MiB, and multiline records up to 2 MiB. The original bytes (including any BOM) are stored and hashed. Preview includes five records. Uploads stream to object storage; R2 uses sequential 16 MiB multipart parts and aborts invalid/failed transfers. Upload/import endpoints permit six-hour read/write deadlines. A dataset over 20 MiB or 100,000 rows requires a Python plan with `parameters.dataset_mode="stream"`; `dataset_rows` is then an iterator rather than an in-memory list. No automatic truncation or sampling occurs.

Reports are Markdown, plots are SVG, downloadable results are JSON/CSV. PDF rendering is not included.

## General custom experiments

The examples do not limit supported scientific domains. Use `adapter: "python.experiment"` with any nonempty domain, `code` containing `run_experiment(inputs, dataset_rows)`, and parameters `{ "inputs": {...}, "seed": 42, "required_modules": ["math"] }`. The remaining Plan fields are unchanged. See `examples/custom-plan.json` for a full biology plan.

Manual/general plans can be saved before Docker is available. `POST .../run` returns `422` with the missing worker/dependency requirement instead of running generated code on the host. Tool registry entries distinguish `category: "capability"` from `category: "example"`; the general tool reports `available_modules`. Custom result series use `x_axis_label` and `y_axis_label` and can describe arbitrary measured quantities. Source code is versioned with the plan and saved as `experiment.py` in each successful attempt bundle.

## Research voice sessions

`POST /api/v1/voice/session` requires the same workspace bearer token as other API routes. It returns `{ "signed_url": "wss://api.elevenlabs.io/v1/convai/conversation?..." }`, a temporary ElevenLabs connection URL. The API key stays server-side and responses use `Cache-Control: no-store`. No body is required.

Configure `ELEVENLABS_API_KEY` and `ELEVENLABS_AGENT_ID` in the shared root `.env`. `GET /api/v1/settings` includes `voice_enabled`, which reports whether both settings exist; it does not perform a live credential check. Unconfigured sessions return `503 voice_not_configured`, provider authentication failures return `502 voice_credentials_rejected`, rate limits return `503 voice_busy`, and other provider/network errors return `502 voice_unavailable`. Provider error bodies and credentials are never forwarded.

### Language capabilities

`GET /api/v1/languages` returns `{data: [{id, available, version, modules, extension}]}` from the worker startup probe. Create an experiment with optional `language` (`auto`, `python`, `javascript`, `r`, `go`, `cpp`, or another runtime name). `PATCH /api/v1/experiments/{id}` can change `language` before execution and clears the prior plan. Explicit preferences are enforced when saving or generating plans. General non-Python plans use adapter `language.experiment` plus `language` and `language_reason`; the normal review/run endpoints apply. Uninstalled runtimes or required libraries reject execution with HTTP 422.

### Interactive research conversation

`POST /api/v1/assistant/chat` accepts `{messages: [{role: "user" | "assistant", content: string}], experiment_id?: string}`. Supply 1–24 messages, end with a user turn, and keep total text under 32000 characters. Optional experiment context is loaded from the database. The response contains `message`, optional `follow_up: {prompt, options: string[]}` (2–5 distinct choices), and optional `draft: {question, language}`. A reply never saves or runs an experiment; the user explicitly reviews and submits a proposed draft. Invalid model output returns 502; provider throttling returns 429 with `Retry-After`. Chat history is provided by the client and is not persisted.

### Workspace knowledge in chat

Both JSON and streaming chat accept optional `dataset_id` as well as `experiment_id`. The assistant can request internal `workspace_search`, `workspace_offset`, and `workspace_read` actions. These are handled server-side within the authenticated request, with at most five model turns and a 64 KB retrieval payload budget. Search pages include at most 20 records, total match counts, and offsets. Retrieval covers experiments, projects, datasets, report/manuscript metadata, current and historical plans, activity, and registered artifact names. Saved document and permitted text artifact bodies are read from configured object storage on demand, with integrity checks. No new public read or mutation route is added.

Final replies may include `sources: [{key, kind, id, title, experiment_id?}]`. These references are built from records actually read by the server; model-supplied source lists are discarded. Tool requests are removed from the final response. Truncated text and dataset-preview scopes are explicitly marked in model context. Missing/unreadable records are reported as unavailable rather than fabricated. Chat streams emit search/read stages and fresh `model_start`/`delta` events for each retrieval turn. The voice client tool `ask_workspace` calls this same chat stream.

### Evidence figures in generated documents

`POST /api/v1/experiments/{id}/documents` accepts `{kind: "report" | "manuscript", include_figures?: boolean}`. Omit `include_figures` for automatic useful-figure selection; `false` requests text only. Successful document responses may include `figures: [{name, pdf, caption, source, kind, renderer}]`.

`GET /api/v1/documents/{documentID}/figures/{name...}` serves only the document's listed PNG/PDF figures behind the existing API authentication. Asset hashes are verified. The document bundle includes images, vector PDFs, the figure manifest, and fixed plotting code/data/requirements. LaTeX uses relative vector PDF paths, so use the complete ZIP when compiling a manuscript with figures. No generated Python code is executed by the figure renderer; the writing agent selects validated evidence sources, not numerical values or file paths.

### Dataset discovery and import

- `GET /api/v1/datasets/search?q=...`: search the workspace and public UK/California CSV catalogs. Returns `{workspace, public, warnings}`. Public candidates include `name`, `url`, `publisher`, and available license/description metadata. Catalog failures are reported, not replaced with invented results.
- `POST /api/v1/datasets/import`: `{url, name?}`. Downloads a public HTTPS CSV directly into R2, validates it, then returns saved dataset metadata. URL redirects and resolved addresses are checked; private-network URLs and credentials in URLs are rejected. Gated files and archive extraction are not supported.
- The assistant chat accepts optional `dataset_id`; drafts may propose an existing workspace `dataset_id`. A model `dataset_search` action triggers a bounded search and returns source-backed candidates to the UI. Import is a separate user action; chat does not claim a source is already imported.

### Manuscript PDF export

`POST /api/v1/documents/{documentID}/pdf` compiles a saved manuscript and returns `{name, sha256, source_sha256, size, compiled_at, compiler}` with HTTP 201, or HTTP 200 for a verified cached PDF. `/pdf/stream` provides the same operation with `status`, `heartbeat`, `done`, and `error` events. `GET /api/v1/documents/{documentID}/pdf` downloads the verified PDF as an authenticated attachment; it returns 404 before generation. Reports return 422. Compilation failures return 502 `pdf_compilation_failed` and do not modify the source or evidence. The source ZIP includes a verified `main.pdf` when available.

One compilation runs at a time. Bubblewrap isolates the compiler from network, workspace files and application environment variables; it exposes only the read-only system TeX runtime and memory-only working directories. Compilation uses two pdfLaTeX passes with shell escape disabled, a 75-second request timeout, bounded memory/files/output, and validated figure inputs. Finished PDFs and manifests are stored through the configured object storage and bound to the saved source/figure hashes.

### AI-generated execution budgets

The worker execution limit remains 1–300 seconds. During AI planning, an omitted or zero `timeout_seconds` receives a 120-second default, and suggestions above 300 are capped at 300 before validation. Streaming planning reports these adjustments. The harness changes only the execution budget, preserving the question, method, code, and scientific parameters. Negative values and invalid field types still require repair. Manually submitted plans must specify a valid timeout and are not normalized.
