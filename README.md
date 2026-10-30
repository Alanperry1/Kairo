# Kairo

A general-purpose AI research workspace for designing, running, and reproducing computational experiments.

Start with your own research question. Review the proposed method and code, run the experiment in an isolated worker, and inspect the evidence. Physics, chemistry, biology, statistics, computer science, and economics are examples of research domains—not a fixed list of experiment types.

## What it does

- **Custom experiments:** define your own question, hypothesis, method, inputs, and scientific domain.
- **Open-weight AI planning:** connect Ollama or an OpenAI-compatible model server to generate research plans and experiment code.
- **Review before execution:** inspect assumptions, requirements, parameters, and code before starting a run.
- **Isolated execution:** run custom code in Docker with time, CPU, memory, process, and network restrictions.
- **Evidence and reproduction:** view findings, plots, limitations, and downloadable code, data, reports, and reproduction instructions.
- **Reports and manuscripts:** generate saved Markdown reports and LaTeX research drafts from completed runs. The writing agent can select useful Matplotlib figures from saved evidence and embed them in either format. Download `.tex` or `.md`, or a ZIP containing the draft and its recorded evidence. Compile manuscripts with LuaLaTeX (including in Overleaf); review claims, authorship, and citations before publication.
- **Workspace navigation:** Overview, Experiments, Projects, Datasets, Reports, Manuscripts, and Activity.
- **Interactive Kairo:** open Kairo in the sidebar for a back-and-forth research conversation. It asks contextual follow-ups with clickable choices, accepts free-text answers, and proposes a question and language for review. Select “Discuss this experiment with Kairo” from an experiment to discuss its saved method and evidence. Conversation history stays in this open app session; it is not saved to Postgres.
- **Research management:** organize projects, upload CSV datasets, track progress, cancel runs, and review plan history.

The homepage includes a sliding **Design → Execute → Evaluate** preview. In the experiment workspace, the microphone starts an ElevenLabs conversation powered by an open-weight Qwen model. Discuss the current method or findings, apply a research-question draft after reviewing it, or ask “Generate a report” / “Generate a LaTeX manuscript” for a completed experiment. Generated drafts are saved in the document library with the run attempt, plan version, and evidence hash. Updating an existing agent’s tools uses `node --env-file=.env scripts/setup-voice-agent.mjs --update`. Voice conversations can also display clickable follow-up questions; selecting an answer sends it back to the connected voice agent.

The workspace voice control is a compact microphone; wake-listening preferences are inside its expanded panel. The homepage microphone remains a short browser greeting.

## Run locally

### 1. Requirements

| Tool | Requirement |
| --- | --- |
| Node.js | 22.12 or newer |
| Go | 1.25 or newer |
| Docker | Running daemon; required for custom experiment workers |
| PostgreSQL | A remote database, such as Neon |
| Python | 3.10+ on the host for backend tests, local example workers, or Python smoke scripts; the Docker image includes Python |

### 2. Configure one `.env`

Both services automatically read the **same `.env` in the project root**. Keep your existing file. For a fresh checkout, create it with:

```dotenv
# Frontend connection
API_PROXY_TARGET=http://127.0.0.1:8080
VITE_API_BASE_URL=/api/v1

# Go API and persistent storage
ADDR=127.0.0.1:8080
DATA_DIR=./backend/data
DATABASE_URL="postgresql://USER:PASSWORD@YOUR-ENDPOINT-pooler.neon.tech/neondb?sslmode=require&channel_binding=require"
DATABASE_URL_UNPOOLED="postgresql://USER:PASSWORD@YOUR-ENDPOINT.neon.tech/neondb?sslmode=require&channel_binding=require"
API_TOKEN=

# AI planning: disabled | ollama | compatible | google
MODEL_PROVIDER=disabled
MODEL_URL=http://127.0.0.1:11434
MODEL_NAME=qwen3:8b
MODEL_API_KEY=

# Experiment execution
WORKER_MODE=docker
WORKER_IMAGE=researchos-worker:local
WORKERS=1
PYTHON_BIN=python3
CORS_ORIGINS=

# Live research conversations (server only)
ELEVENLABS_API_KEY=
ELEVENLABS_AGENT_ID=
```

No `export` or `source` command is needed. Shell variables override the file. Relative data and worker paths resolve against the root `.env` directory. Restart both services after changing configuration.

`MODEL_PROVIDER=disabled` supports saved research questions and manual plans. Automatic method and code generation requires a running model server.

### 3. Install dependencies and build the worker

From the project root:

```sh
npm install
docker build -f backend/Dockerfile.worker -t researchos-worker:local backend
```

Create a remote PostgreSQL database in Neon, open **Connect**, and paste its connection string into `DATABASE_URL` in the existing root `.env`. Keep the connection string's SSL and channel-binding options. The Go API keeps the existing pgx driver, initializes its schema through `DATABASE_URL_UNPOOLED`, then uses pooled `DATABASE_URL` for application queries. If the direct variable is omitted, it falls back to `DATABASE_URL`. No local PostgreSQL installation or database container is needed. See [Neon's Go guide](https://neon.com/docs/guides/go).

`DATABASE_URL` and `DATABASE_URL_UNPOOLED` are read only by the Go backend. Do not prefix them with `VITE_`.

This checkout is linked through `.neon` to project `wispy-smoke-95886533`, branch `production`, in organization `org-bitter-glade-50634694`. `neon.ts` declares Postgres only. To reconcile that configuration and refresh the database URLs in the existing `.env`:

```sh
neon deploy
```

If the CLI needs authentication, run `neon login` and finish the browser sign-in first. Neon deploy provisions the declared backend services; run or host the React frontend and Go server separately.

The worker image includes **Python, JavaScript (Node.js), R, Go, and C++**. Choose a language when creating an experiment, or leave **Let the agent choose** selected. The agent uses the detected runtime/library catalog and explains its choice in the reviewed plan. A selected language is enforced by the backend. Missing runtimes or packages prevent execution and are named explicitly.

Python includes NumPy, SciPy, pandas, Polars, scikit-learn, statsmodels, SymPy, NetworkX, Matplotlib, seaborn, scikit-image, and Biopython. R includes jsonlite, data.table, ggplot2, MASS, Matrix, and the standard statistical libraries. JavaScript and Go include their standard libraries; C++ also includes nlohmann/json. The library list in the experiment form comes from the running worker.

Add Python packages to `backend/requirements-worker.txt`, or install other language libraries in `backend/Dockerfile.worker`; rebuild the image and restart the backend. Additional language names can be entered in the form. To execute them, install the runtime and its libraries in the worker image and add an entry to `backend/internal/research/runtimes.json` (extension, run/version argument arrays, optional build arguments, and installed module names). Arguments use `{source}` and `{binary}` placeholders and run without a shell. Rust, Ruby, and Julia are excluded from experiment language choices and plan validation. Packages are not installed during experiment execution.

Python plans use `python.experiment` and define `run_experiment(inputs, dataset_rows)`. Other languages use `language.experiment`, set `language` to its catalog ID, read `{inputs, dataset_rows, seed}` JSON on stdin, and print one JSON result using the same metrics/findings/limitations schema. Complete executable source, language/runtime provenance, and replay instructions are saved in the reproduction bundle.

### 4. Start both services

**Terminal 1 — backend**, starting from the project root:

```sh
npm run dev:api
```

**Terminal 2 — frontend**, from the project root:

```sh
npm run dev
```

| Page or service | URL |
| --- | --- |
| Homepage | http://localhost:5173/ |
| Research workspace | http://localhost:5173/app |
| API | http://127.0.0.1:8080/api/v1 |
| Health check | http://127.0.0.1:8080/healthz |

The npm cache, Go module/build caches, temporary build files, and backend binaries stay under the ignored `backend/data/` folder on the project drive. `npm run build:api` builds a persistent backend binary there. Docker keeps its existing system storage location.

Keep both terminals running. Vite forwards API requests to Go. The default proxy setup needs no additional CORS configuration.

## Store research artifacts in Cloudflare R2

R2 uses the server's S3-compatible client. The bucket endpoint is not an upload credential. In the existing root `.env`:

```dotenv
R2_ENDPOINT=https://1a299745bf65705ce606f74b58c0756c.r2.cloudflarestorage.com
R2_BUCKET=kairo
R2_ACCESS_KEY_ID=YOUR_R2_ACCESS_KEY_ID
R2_SECRET_ACCESS_KEY=YOUR_R2_SECRET_ACCESS_KEY
STORAGE_BACKEND=local
```

Create an R2 token with **Object Read & Write** access scoped to `kairo` using [Cloudflare's authentication instructions](https://developers.cloudflare.com/r2/api/tokens/). Keys stay in the Go server; downloads remain behind the existing API authentication. No public bucket access or browser R2 CORS configuration is required.

For existing research, finish/cancel active jobs and stop the backend before migration:

```sh
npm run migrate:storage -- --delete-local
```

This uploads database-referenced datasets, attempt files, and generated document source/evidence. It downloads each uploaded object to verify its SHA-256 and size, then clears legacy document bodies from Postgres and, with `--delete-local`, removes verified local research files. A failed upload/verification preserves local files. A repeat run can verify objects already migrated; mismatching existing objects are never overwritten. Project source, `.env`, Docker images, caches, and development logs are outside the migration.

If historical files were already lost, migration first tries to recover representations of saved results, but accepts them only when their SHA-256 and size match the original artifact record. The default migration stops at anything still missing. `--allow-missing` explicitly permits cutover while retaining those references, recording the missing object names in its report, and backing up saved experiment records to R2. It never invents replacement results or overwrites a conflicting object.

After migration succeeds, set `STORAGE_BACKEND=r2` in the same `.env` and start the API with `npm run dev:api`. Fresh deployments with no existing research can set R2 mode immediately after adding credentials. R2 mode fails explicitly for missing credentials or storage errors and never falls back to saving research files on disk. Postgres keeps experiment state and artifact/document metadata. ZIP downloads are assembled from stored objects as a stream without local archive files.

## Datasets up to 5 GB

Each CSV can be up to **5 GB (5,000,000,000 bytes)**. There is no 50,000-row storage limit. Uploads are validated incrementally and sent to R2 with multipart uploads using a bounded 16 MiB part buffer; the API does not spool the file to local disk. Invalid, over-limit, and interrupted uploads abort their multipart upload instead of publishing incomplete datasets. Files retain their exact bytes and SHA-256 (including a UTF-8 BOM, if present); the preview shows five rows. CSV structure limits remain: 2–100 columns, unique trimmed column names, cells up to 10,000 characters, physical lines up to 1 MiB, and records up to 2 MiB.

Use **Datasets** to upload a file or a folder of CSVs, search public catalogs, or paste a direct public HTTPS CSV URL. Public search covers UK and California open-data catalogs and reports unavailable sources explicitly. An import downloads directly from the source into R2 after CSV validation. Kairo can search on your behalf and display verified candidates; click **Import to R2** to store a public source, or **Use dataset** to select an existing workspace file. Source URLs are recorded for imported files. Private/gated sources and non-CSV archives are not supported by the public importer.

Choose an existing dataset in the experiment form or Kairo conversation. Naming a file in the workspace lets the agent identify it without uploading it again. Selecting a browser folder uploads its CSV files; the server does not browse arbitrary folders on your computer.

Storage capacity is separate from worker memory. Datasets over 20 MiB or 100,000 rows require a Python custom plan with `parameters.dataset_mode="stream"`. The method receives a single-pass `dataset_rows` iterator and should use incremental aggregates or bounded batches. The API streams every row from R2 and checks the source hash; it never silently samples the file. Other languages and fixed example adapters currently accept only smaller inline datasets. Large analyses still have the configured per-plan timeout and container memory limits. Streaming reproduction bundles include `prepare-stream.py` and the dataset ID/hash; download the original CSV alongside the bundle before replaying.

Uploads/imports permit up to six hours at the API. If you deploy behind another reverse proxy, its body-size and timeout limits must also allow the chosen file size. Interrupted transfers must be restarted; resumable browser uploads are not implemented.

## Enable AI planning

Install and serve your chosen open-weight model using Ollama or a compatible model server. Then edit the existing root `.env`.

For Google-hosted Gemma (the configured research planner):

```dotenv
MODEL_PROVIDER=google
MODEL_URL=https://generativelanguage.googleapis.com/v1beta
MODEL_NAME=gemma-4-26b-a4b-it
MODEL_API_KEY=your_google_ai_studio_key
```

The Go backend calls Google's native API (`streamGenerateContent` for live tasks), requests JSON for research plans, and reads the model's output text. Gemma 4 thinking is set to minimal. Credentials remain in the root `.env`; the browser does not call the model API directly. This model connection is independent of the ElevenLabs voice agent.

For local Ollama:

```dotenv
MODEL_PROVIDER=ollama
MODEL_URL=http://127.0.0.1:11434
MODEL_NAME=qwen3:8b
```

For an OpenAI-compatible model server:

```dotenv
MODEL_PROVIDER=compatible
MODEL_URL=http://127.0.0.1:8000/v1
MODEL_NAME=your-served-model-name
MODEL_API_KEY=
```

Use the exact model name served by your endpoint. The compatible endpoint must support chat completions and JSON-object output. Set `MODEL_API_KEY` only if that service requires authentication. The backend connects to the model server; it does not download or serve model weights.

Restart the backend after changing these values.

## Workspace knowledge

Ask Kairo about saved experiments, current or historical methods/code, computed results, errors and activity, projects, datasets, reports, manuscripts, and registered text artifacts. Chat searches Postgres records, reads relevant evidence on demand, and displays **Sources consulted** buttons that open the source study or document. Workspace totals and status counts are supplied directly from the database. Follow-up searches can page beyond the initial catalog. New saved records are available immediately; no reindex command is needed.

Document bodies are retrieved from R2 after locating their title or linked experiment; full-text search inside every R2 document is not indexed. Chat uses bounded, explicitly marked excerpts. Dataset context includes metadata and five preview rows; analyzing all rows still requires an experiment. Registered text artifacts up to 1 MiB can be read after verifying their stored hash. Workspace retrieval does not read `.env`, credentials, arbitrary filesystem paths, or unregistered files. Relevant retrieved research text is sent to the configured model provider; voice lookup answers are also shared with ElevenLabs during a conversation. Research files remain in the configured object storage.

The ElevenLabs client tool `ask_workspace` uses the same streaming knowledge service. After changing the voice tool configuration, run `node --env-file=.env scripts/setup-voice-agent.mjs --update` and start a new voice conversation.

## Live agent activity

Chat, AI planning, and report/manuscript generation show live stages and a growing preview while the model writes. Experiment details also receive saved execution events for input checks, computation, analysis, and artifact upload. Two-second polling remains available if the execution event stream disconnects.

Previews are provisional output, not saved results or the model's private reasoning. A task reports completion only after the final response passes validation and any required object uploads and database writes succeed. Partial model output stays in browser/server memory; final research files use the configured object storage. Closing a chat or document panel cancels its request. If a stream disconnects, check the experiment or document list before retrying because saving may already have finished. Deploy reverse proxies with SSE buffering disabled.

## Create an experiment

1. Open the workspace and choose **New experiment → Custom experiment**.
2. Describe your question, data, and constraints. Attach a dataset or project when needed.
3. With AI enabled, generate a plan; otherwise save your question and add a manual plan.
4. Review the hypothesis, method, assumptions, parameters, and experiment code.
5. Select **Run experiment**.
6. Inspect **Results**, **Activity**, and **Artifacts**, then download the report or reproduction bundle.

You can also enter a question on the homepage and select **Plan my experiment**. It transfers to the custom experiment form through temporary tab-local storage; the question is not added to the URL.

A separate biology study is provided in [custom-plan.json](backend/examples/custom-plan.json). See the [general experiment contract](backend/GENERAL_EXPERIMENTS.md) to write your own plan and result-producing code.

## Research voice companion

Select the **microphone** on the Experiments page, in a new-experiment form, or in an experiment’s detail panel. Select **Start conversation** and allow microphone access. The panel shows the conversation transcript, listening/speaking state, mute and end controls, and a typed-message option. Closing the panel or leaving its view ends the session.

For voice activation, open **Settings → Hey Kairo** and enable the switch. Say **“Hey Kairo”** (also accepts **“Hey Kario”**) to open the panel and start the ElevenLabs greeting without another click. The setting is remembered on this browser and follows you across workspace pages. Close Settings and allow microphone access to start listening. Wake listening pauses while the voice panel is open or the tab is hidden and resumes when you close the panel or return to the tab. Switch it off to release the microphone. Your browser may send wake-listening audio to its speech-recognition service; this is separate from ElevenLabs conversation audio. Browser speech recognition has limited availability, so the **microphone button** remains available when it is unsupported.

The agent can read the current question, saved method, status, and bounded findings/metrics. Uploaded dataset rows, API tokens, and database credentials are not included. Audio and the selected experiment context are sent to ElevenLabs only when you start a conversation. The conversation history shown in the panel is kept in memory; ElevenLabs may retain conversation data under your account settings.

The `set_research_question` tool proposes a question; select **Use question** to apply it. On the list or an existing experiment, this opens a new draft form; in the new-experiment form, it fills the question field. It does not save, generate a plan, or start execution. Review the method and start runs using the normal workspace controls.

Credentials live in the single root `.env`. To connect another account, run ElevenLabs onboarding, then keep `ELEVENLABS_API_KEY` in the root `.env`. To create a new Kairo agent from the versioned configuration:

```sh
node --env-file=.env scripts/setup-voice-agent.mjs
```

The script reuses an existing `ELEVENLABS_AGENT_ID` and creates an agent only if the ID is empty. [config/voice-agent.mjs](config/voice-agent.mjs) defines its greeting, Qwen model, voice, instructions, and client tools. Change the remote agent configuration in ElevenLabs when updating an existing agent. Restart the Go API after changing credentials.

The Go endpoint `POST /api/v1/voice/session` exchanges the private key for a temporary WebSocket URL using the workspace’s existing authentication. The React SDK is loaded only when you open the voice panel. Use HTTPS or localhost for microphone access. Voice setup is independent of the experiment-planning model: `MODEL_PROVIDER=disabled` still permits voice discussions and saved questions, but does not enable AI plan generation.

## Authentication and workers

**`API_TOKEN`** protects the Go API. It is optional for local loopback use. If you set it, use at least 24 characters. Vite automatically reads it from the shared `.env` and attaches it to backend requests, including uploads and downloads. You do not need to paste it into the dashboard. It stays in the Vite server process and is not bundled into browser code.

**`WORKERS`** controls simultaneous experiments. Start with `1`; the backend supports `1–4`. More workers use more CPU and memory and may affect benchmark timings. Extra jobs wait in the PostgreSQL queue.

**`WORKER_MODE=docker`** is required for custom code. `local` mode runs only the fixed example adapters and needs host Python; the statistics example also needs SciPy. A missing worker or dependency produces an explicit error.

Keep secrets out of `VITE_*` variables: those values are public browser build configuration. The root `.env` is ignored by Git.

## Voice agents

The homepage introduces voice agents with **Powered by ElevenLabs** branding. The intended workflow is to discuss an idea, clarify the question, and continue into experiment planning.

Clicking the homepage microphone plays “Hey there, I’m Kairo, your AI research companion.” using browser speech synthesis, with stop/replay controls. This greeting needs no API key and does not record microphone input. Full audio conversations, an ElevenLabs agent connection, and voice-to-experiment handoff remain to be implemented.

## Development checks

Frontend tests and production build, from the project root:

```sh
npm test
npm run build
```

Backend tests and checks:

```sh
cd backend
TEST_DATABASE_URL="YOUR_TEST_DATABASE_URL" go test -race ./...
go vet ./...
```

Database tests use disposable schemas in `TEST_DATABASE_URL`, read from the shell or the same root `.env`. Use a separate database or Neon test branch with a direct connection and permission to create schemas. Without `TEST_DATABASE_URL`, database tests are skipped. The application database is never used as an automatic test fallback.

After building the Docker worker, include the real custom-experiment container test:

```sh
cd backend
KAIRO_TEST_DOCKER=1 TEST_DATABASE_URL="YOUR_TEST_DATABASE_URL" go test -race ./...
```

With both services running, check the actual frontend proxy and backend workflow:

```sh
npm run test:integration
```

Use a test data directory for integration checks. The script creates a project and CSV dataset, runs and deletes a test experiment, and retains the project and dataset because their deletion endpoints are not implemented. Set `INTEGRATION_API_URL` if your Vite proxy uses a different port.

## Build and deployment

```sh
npm run build
npm run preview
```

The frontend output is `dist/`. Vite preview uses the local API proxy and reads the shared `.env`.

For production, configure a server-side reverse proxy for `/api/v1` with authentication, or use a separate API origin with the appropriate CORS and application authentication. Vite's development proxy is not included in static build output. Run one Go API process per database and data directory; job recovery assumes a single API instance. Back up PostgreSQL and the artifact files in `DATA_DIR` together. Use the TLS settings required by your hosted PostgreSQL provider.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| Cannot connect to PostgreSQL | Check the remote `DATABASE_URL`, SSL options, and network access. |
| `ECONNREFUSED 127.0.0.1:8080` | Start the backend and check its terminal for startup errors. |
| Port `5173` is occupied | Stop the previous Vite process before starting another. |
| `401 Unauthorized` | Make sure both services read the same `API_TOKEN`, then restart them. |
| Custom execution is unavailable | Build the worker image, start Docker, and set `WORKER_MODE=docker`. |
| AI planning is disabled or unavailable | Configure a running model endpoint and its exact model name. |
| A required library is missing | Install it in the worker image, rebuild, and restart the backend. |
| Snap's Go launcher fails in a sandbox | Use `/snap/go/current/bin/go` in place of `go`. |

## Project structure

```text
src/
  HomePage.jsx           Homepage and research entry point
  App.jsx                Connected research dashboard
  components/            Forms, experiment views, charts, and workflow carousel
  api.js                 API client and authenticated downloads
backend/
  cmd/server/            Go API entry point
  internal/research/     Planning, validation, persistence, jobs, and execution
  examples/              Example datasets and experiment plans
  Dockerfile.worker      Isolated scientific worker image
scripts/                 Frontend proxy and integration checks
public/                  Favicon and homepage artwork
.env                     Shared local configuration, ignored by Git
```

Further documentation: [backend setup](backend/README.md), [API reference](backend/API.md), [custom experiment contract](backend/GENERAL_EXPERIMENTS.md), and [homepage artwork](docs/home-artwork.md).

## Report and manuscript figures

Leave **Include figures when useful** checked when generating a document, or uncheck it for a text-only draft. The writing agent chooses up to three figures from recorded series, complete numeric CSV columns, or matching metrics across conditions. If the evidence does not support a useful plot, no figure is added. It cannot invent data, fit a new trend, or add unrecorded confidence intervals through the figure renderer.

A fixed Matplotlib renderer runs inside the existing isolated worker (using the worker's installed Matplotlib); trusted local rendering is also available in local development mode. Images and plotting assets are stored with the document in R2. The viewer shows authenticated PNG previews; manuscripts embed vector PDFs using `graphicx`, while Markdown reports reference PNGs. For a complete document, download **Source, figures & evidence ZIP** and keep its `figures/` folder beside `main.tex` or `report.md`.

The ZIP includes `figures.py`, `figure-data.json`, and `requirements-figures.txt`. Reproduce the figures with:

```sh
python3 -m pip install -r requirements-figures.txt
python3 figures.py --export < figure-data.json
```

This re-plots the saved values without rerunning the experiment. PNG/PDF assets are checked against their saved SHA-256 before serving or packaging.

### Manuscript LaTeX format

New manuscript drafts use a 10-point, two-column conference-style article matching the supplied paper layout: 0.75-inch side margins, a full-width title/author/abstract block, keywords, compact numbered headings, a running header, page numbers, and booktabs metric tables. The writing agent supplies Introduction, Background and Related Work, Methodology, Experimental Setup, Results, Discussion, and Conclusion; recorded limitations and reproduction details remain included. Figures use the existing bundle-relative vector PDFs. Tables split into column-sized batches and abbreviate long values; full evidence stays in `evidence.json`.

Author names and affiliations remain editable placeholders. References are left for verified sources rather than copying the example paper's bibliography. Existing saved manuscripts keep their original source; generate a new draft to use this format. Compile the complete source bundle with pdfLaTeX, LuaLaTeX, or XeLaTeX and the standard LaTeX packages listed in `main.tex`.

### Generate a manuscript PDF

Open a saved manuscript and select **Generate & download PDF**. The app compiles its saved `main.tex` and vector figures twice, streams progress, and stores the PDF and a source/output hash manifest with that document in R2. Later downloads reuse the verified PDF; changed sources or a failed integrity check trigger recompilation. A compiled PDF is also included as `main.pdf` in the source bundle when available. Compilation failures leave the LaTeX and research evidence intact.

The ElevenLabs tool `generate_manuscript_pdf` provides the same action by voice. Start a new voice session after updating its configuration. PDF rendering requires Linux with user namespaces, Bubblewrap (`bwrap`), `/usr/bin/pdflatex`, Python 3, and `prlimit` (util-linux) on the API host. On Debian/Ubuntu the LaTeX dependencies are provided by `texlive-latex-extra`, `texlive-fonts-recommended`, and `lmodern`. No new `.env` variables are required. The compiler has no network or project/credential filesystem access; its bounded temporary files live in memory and disappear when the process exits. Only the finished PDF and its manifest persist in object storage.
