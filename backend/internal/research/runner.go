package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed worker.py
var workerCode string

//go:embed runtimes.json
var runtimeCatalog string

type WorkerInput struct {
	DatasetStream    bool                  `json:"dataset_stream,omitempty"`
	Dataset          *Dataset              `json:"dataset,omitempty"`
	WriteDatasetRows func(io.Writer) error `json:"-"`
	Plan             *Plan                 `json:"plan"`
	DatasetRows      []map[string]string   `json:"dataset_rows,omitempty"`
}
type Executor interface {
	Execute(context.Context, WorkerInput) (Result, error)
}
type PythonExecutor struct{ Python, Mode, Image string }
type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errors.New("worker output exceeded limit")
	}
	return b.Buffer.Write(p)
}
func (p PythonExecutor) Execute(ctx context.Context, input WorkerInput) (Result, error) {
	var result Result
	if input.Plan == nil {
		return result, errors.New("experiment plan is required")
	}
	if (input.Plan.Adapter == "python.experiment" || input.Plan.Adapter == "language.experiment") && p.Mode != "docker" {
		return result, errors.New("custom experiment code requires an isolated Docker worker; local execution is disabled")
	}
	data, err := json.Marshal(input)
	if err != nil {
		return result, err
	}
	var cmd *exec.Cmd
	if p.Mode == "docker" {
		name := newID("researchos-worker")
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "--pull=never", "-i", "--name", name, "--network", "none", "--read-only", "--memory", "768m", "--cpus", "1", "--pids-limit", "64", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", "65534:65534", "--tmpfs", "/tmp:size=256m,exec,nosuid,nodev", "-e", "OPENBLAS_NUM_THREADS=1", "-e", "OMP_NUM_THREADS=1", "-e", "KAIRO_ISOLATED_WORKER=1", "-e", "GOCACHE=/tmp/go-cache", "-e", "GOPATH=/tmp/go", "-e", "GOMAXPROCS=1", "-e", "HOME=/tmp", "-e", "MPLCONFIGDIR=/tmp/matplotlib", p.Image, "python3", "-I", "-c", workerCode)
		defer func() {
			cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = exec.CommandContext(cleanCtx, "docker", "rm", "-f", name).Run()
		}()
	} else {
		cmd = exec.CommandContext(ctx, p.Python, "-I", "-c", workerCode)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "OPENBLAS_NUM_THREADS=1", "OMP_NUM_THREADS=1", "MKL_NUM_THREADS=1", "PYTHONDONTWRITEBYTECODE=1"}
	}
	var inputDone chan error
	var inputPipe *io.PipeReader
	if input.DatasetStream {
		if input.WriteDatasetRows == nil {
			return result, errors.New("dataset stream is unavailable")
		}
		reader, writer := io.Pipe()
		inputPipe = reader
		inputDone = make(chan error, 1)
		go func() {
			_, err := writer.Write(append(append([]byte("KAIRO_DATASET_STREAM\n"), data...), '\n'))
			if err == nil {
				err = input.WriteDatasetRows(writer)
			}
			_ = writer.CloseWithError(err)
			inputDone <- err
		}()
		cmd.Stdin = reader
	} else {
		cmd.Stdin = bytes.NewReader(data)
	}
	stdout := &limitedBuffer{limit: 16 << 20}
	stderr := &limitedBuffer{limit: 16 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Run()
	if inputPipe != nil {
		_ = inputPipe.Close()
		inputErr := <-inputDone
		if err == nil && inputErr != nil {
			return result, fmt.Errorf("dataset streaming failed: %w", inputErr)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, fmt.Errorf("scientific worker failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err = json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return result, fmt.Errorf("invalid worker result: %w", err)
	}
	if result.Kind == "" || result.Metrics == nil || len(result.Findings) == 0 {
		return result, errors.New("worker returned incomplete evidence")
	}
	for _, s := range result.Series {
		for _, p := range s.Points {
			if !finite(p.X) || !finite(p.Y) {
				return result, errors.New("non-finite plot coordinates")
			}
		}
	}
	return result, nil
}

type Jobs struct {
	Objects           ObjectStorage
	store             *Store
	registry          *Registry
	executor          Executor
	model             *ModelClient
	root, mode, image string
	workers           int
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	wake              chan struct{}
	mu                sync.Mutex
	active            map[string]context.CancelFunc
}

func NewJobs(store *Store, registry *Registry, executor Executor, model *ModelClient, root, mode, image string, workers int) *Jobs {
	ctx, cancel := context.WithCancel(context.Background())
	return &Jobs{store: store, registry: registry, executor: executor, model: model, root: root, mode: mode, image: image, workers: workers, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), active: map[string]context.CancelFunc{}}
}
func (j *Jobs) Start() {
	for n := 0; n < j.workers; n++ {
		j.wg.Add(1)
		go j.loop()
	}
}
func (j *Jobs) Close() { j.cancel(); j.wg.Wait() }
func (j *Jobs) Wake() {
	select {
	case j.wake <- struct{}{}:
	default:
	}
}
func (j *Jobs) Cancel(id string, attempt int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if cancel := j.active[fmt.Sprintf("%s:%d", id, attempt)]; cancel != nil {
		cancel()
	}
}
func (j *Jobs) loop() {
	defer j.wg.Done()
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-j.ctx.Done():
			return
		default:
		}
		runs, _, err := j.store.List("queued", "", "", 1, 0)
		if err != nil {
			slog.Error("load queue", "error", err)
		}
		if len(runs) > 0 {
			e, err := j.store.Mutate(runs[0].ID, "running", "Scientific worker started", func(e *Experiment) error {
				if e.Status != "queued" {
					return ErrConflict
				}
				e.Status = "running"
				e.Progress = 15
				return nil
			})
			if err == nil {
				j.run(e)
			}
			continue
		}
		select {
		case <-j.ctx.Done():
			return
		case <-j.wake:
		case <-timer.C:
		}
	}
}
func (j *Jobs) Input(e Experiment) (WorkerInput, error) {
	input := WorkerInput{Plan: e.Plan}
	if e.Plan == nil {
		return input, errors.New("plan is required")
	}
	if e.Plan.Adapter != "statistics.welch" && !isCustomAdapter(e.Plan.Adapter) {
		return input, nil
	}
	if isCustomAdapter(e.Plan.Adapter) && e.DatasetID == "" {
		return input, nil
	}
	if e.DatasetID == "" {
		return input, errors.New("attach a CSV dataset before statistical execution")
	}
	d, err := j.store.Dataset(e.DatasetID)
	if err != nil {
		return input, err
	}
	var params GeneralParams
	if isCustomAdapter(e.Plan.Adapter) {
		if err := json.Unmarshal(e.Plan.Parameters, &params); err != nil {
			return input, err
		}
	}
	large := d.Size > InlineDatasetBytes || d.Rows > InlineDatasetRows
	if large && params.DatasetMode != "stream" {
		return input, errors.New("this dataset needs dataset_mode=stream with a Python row-iterator method; replan in Python or attach a smaller file")
	}
	if params.DatasetMode == "stream" {
		if e.Plan.Adapter != "python.experiment" {
			return input, errors.New("large dataset streaming requires Python")
		}
		input.DatasetStream = true
		input.Dataset = &d
		input.WriteDatasetRows = func(writer io.Writer) error { return j.streamDatasetRows(j.ctx, d, writer) }
		return input, nil
	}
	raw, err := readObject(j.ctx, j.objectStorage(), "datasets/"+d.ID+".csv", InlineDatasetBytes)
	if err != nil {
		return input, err
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != d.SHA256 {
		return input, errors.New("dataset integrity check failed")
	}
	reader := csv.NewReader(bytes.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return input, err
	}
	if isCustomAdapter(e.Plan.Adapter) {
		for {
			row, readErr := reader.Read()
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return input, readErr
			}
			record := map[string]string{}
			for i, name := range header {
				record[name] = row[i]
			}
			input.DatasetRows = append(input.DatasetRows, record)
		}
		return input, nil
	}
	var c StatisticsParams
	if err = json.Unmarshal(e.Plan.Parameters, &c); err != nil {
		return input, err
	}
	columns := map[string]int{}
	for i, h := range header {
		columns[h] = i
	}
	if _, ok := columns[c.GroupColumn]; !ok {
		return input, errors.New("group_column is not in the dataset")
	}
	if _, ok := columns[c.ValueColumn]; !ok {
		return input, errors.New("value_column is not in the dataset")
	}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return input, err
		}
		input.DatasetRows = append(input.DatasetRows, map[string]string{c.GroupColumn: row[columns[c.GroupColumn]], c.ValueColumn: row[columns[c.ValueColumn]]})
	}
	return input, nil
}
func (j *Jobs) run(e Experiment) {
	ctx, cancel := context.WithTimeout(j.ctx, time.Duration(e.Plan.TimeoutSeconds)*time.Second)
	defer cancel()
	j.mu.Lock()
	key := fmt.Sprintf("%s:%d", e.ID, e.Attempt)
	j.active[key] = cancel
	j.mu.Unlock()
	defer func() { j.mu.Lock(); delete(j.active, key); j.mu.Unlock() }()
	current, err := j.store.Get(e.ID)
	if err != nil || current.Status != "running" || current.Attempt != e.Attempt {
		return
	}
	_ = j.store.Event(e.ID, "input_validation", "Checking dataset integrity, plan requirements, and worker libraries")
	input, err := j.Input(e)
	if err == nil {
		err = j.registry.Validate(*e.Plan, true)
	}
	var result Result
	if err == nil {
		_ = j.store.Event(e.ID, "executing", "Running the reviewed experiment code in the scientific worker")
		result, err = j.executor.Execute(ctx, input)
	}
	if err != nil {
		j.fail(e.ID, e.Attempt, err)
		return
	}
	attempt := e.Attempt
	_, err = j.store.Mutate(e.ID, "analyzing", "Computed evidence received; preparing report", func(e *Experiment) error {
		if e.Status != "running" || e.Attempt != attempt {
			return ErrConflict
		}
		e.Progress = 75
		return nil
	})
	if err != nil {
		return
	}
	result.Limitations = append(result.Limitations, e.Plan.Limitations...)
	result.NarrativeSource = "deterministic"
	result.Warnings = []string{}
	if j.model.Provider != "disabled" {
		narrative, modelErr := j.model.Explain(ctx, e, result)
		if modelErr != nil {
			result.Warnings = append(result.Warnings, "AI interpretation unavailable; deterministic evidence and report are retained.")
			_ = j.store.Event(e.ID, "warning", "AI interpretation failed; preserving computed evidence")
		} else {
			result.Narrative = narrative
			result.NarrativeSource = "model:" + j.model.Model
		}
	}
	if ctx.Err() != nil {
		j.fail(e.ID, e.Attempt, ctx.Err())
		return
	}
	_ = j.store.Event(e.ID, "packaging", "Uploading computed results, source, and reproduction files to object storage")
	if err = j.writeArtifactsWithContext(ctx, e, input, result); err != nil {
		j.fail(e.ID, e.Attempt, fmt.Errorf("artifact packaging: %w", err))
		return
	}
	_, err = j.store.Mutate(e.ID, "completed", "Experiment completed; reproducibility artifacts saved", func(e *Experiment) error {
		if e.Status != "running" || e.Attempt != attempt {
			return ErrConflict
		}
		e.Status = "completed"
		e.Progress = 100
		e.Result = &result
		return nil
	})
	if err != nil && !errors.Is(err, ErrConflict) {
		slog.Error("complete experiment", "id", e.ID, "error", err)
	}
}
func (j *Jobs) fail(id string, attempt int, cause error) {
	_, err := j.store.Mutate(id, "failed", "Experiment failed: "+cause.Error(), func(e *Experiment) error {
		if e.Status != "running" || e.Attempt != attempt {
			return ErrConflict
		}
		e.Status = "failed"
		e.Error = cause.Error()
		return nil
	})
	if err != nil && !errors.Is(err, ErrConflict) {
		slog.Error("mark failure", "error", err)
	}
}
func (j *Jobs) writeArtifacts(e Experiment, input WorkerInput, result Result) error {
	return j.writeArtifactsWithContext(context.Background(), e, input, result)
}
func (j *Jobs) writeArtifactsWithContext(ctx context.Context, e Experiment, input WorkerInput, result Result) (err error) {
	prefix := fmt.Sprintf("attempt-%03d", e.Attempt)
	files := map[string][]byte{}
	encode := func(name string, v any) error {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		files[name] = b
		return nil
	}
	if err = encode("input.json", input); err != nil {
		return err
	}
	if err = encode("plan.json", e.Plan); err != nil {
		return err
	}
	if err = encode("results.json", result); err != nil {
		return err
	}
	if input.DatasetStream {
		files["prepare-stream.py"] = []byte(prepareStreamCode)
	}
	files["worker.py"] = []byte(workerCode)
	files["runtimes.json"] = []byte(runtimeCatalog)
	if e.Plan.Code != "" {
		files[sourceFilename(*e.Plan)] = []byte(e.Plan.Code)
	}
	files["data.csv"] = []byte(result.CSV)
	files["chart.svg"] = []byte(renderSVG(result, e.Plan.Adapter))
	files["report.md"] = []byte(renderReport(e, result))
	deps := []string{"# Runtime: " + planLanguage(*e.Plan) + " " + result.RuntimeVersion, "# Python worker " + result.PythonVersion}
	for name, v := range result.Dependencies {
		if name != "python" && planLanguage(*e.Plan) == "python" {
			deps = append(deps, name+"=="+v)
		}
	}
	sort.Strings(deps)
	files["requirements.txt"] = []byte(strings.Join(deps, "\n") + "\n")
	files["REPRODUCE.md"] = []byte("# Reproduce this computational experiment\n\nUse the Python version in provenance.json. Install requirements.txt if nonempty. Run:\n\n```sh\npython3 -m pip install -r requirements.txt\npython3 -I worker.py < input.json > replay.json\n```\n\nCompare replay.json metrics with results.json. Physics and statistical computations are deterministic within floating-point/library differences. Benchmark timing varies across hardware and load. AI narrative is separately labeled and is not reproduced by this command. input.json includes the exact selected data values for statistical analyses. Template adapters use fixed code. General experiments include custom experiment.py, which must be reviewed and replayed in an isolated Docker worker. For general replay use the recorded worker image with --network none --read-only --memory 768m --cpus 1 --pids-limit 64 --cap-drop ALL --security-opt no-new-privileges --user 65534:65534 -e KAIRO_ISOLATED_WORKER=1, mounting the extracted attempt directory read-only; run python3 -I worker.py with input.json on stdin.\n")
	if isCustomAdapter(e.Plan.Adapter) {
		files["REPRODUCE.md"] = []byte("# Reproduce this custom experiment\n\nReview the experiment source and methodology before running custom code. Use the same worker image and dependency versions recorded in provenance.json. The commands below use the default local image name; replace it if provenance records another image.\n\nFrom the extracted attempt directory:\n\n```sh\ndocker run --rm -i --pull=never --network none --read-only \\\n  --memory 768m --cpus 1 --pids-limit 64 --cap-drop ALL \\\n  --security-opt no-new-privileges --user 65534:65534 \\\n  --tmpfs /tmp:size=256m,exec,nosuid,nodev \\\n  --mount \"type=bind,src=$(pwd),dst=/experiment,readonly\" \\\n  -w /experiment -e KAIRO_ISOLATED_WORKER=1 -e HOME=/tmp -e GOCACHE=/tmp/go-cache -e GOPATH=/tmp/go -e GOMAXPROCS=1 -e MPLCONFIGDIR=/tmp/matplotlib \\\n  -e OPENBLAS_NUM_THREADS=1 -e OMP_NUM_THREADS=1 \\\n  researchos-worker:local python3 -I worker.py < input.json > replay.json\n```\n\ninput.json contains the exact reviewed code, parameters, and selected dataset rows. Compare replay.json metrics with results.json. Floating-point/library differences and benchmark timing can vary. AI interpretation is not reproduced. No API token or model credentials are needed.\n")
	}
	if input.DatasetStream {
		files["REPRODUCE.md"] = append(files["REPRODUCE.md"], []byte("\n## Streaming dataset replay\n\nDownload the exact dataset from the workspace to dataset.csv and verify its SHA-256 against input.json.dataset.sha256. Replace the input.json stdin redirection in the Docker command with this pipeline: python3 prepare-stream.py | docker run [same isolation flags] IMAGE python3 -I worker.py > replay.json. The helper reads input.json and dataset.csv incrementally and checks the full checksum. The full dataset is not embedded into this source ZIP.\n")...)
	}
	var dataset *Dataset
	if e.DatasetID != "" {
		d, err := j.store.Dataset(e.DatasetID)
		if err != nil {
			return err
		}
		dataset = &d
	}
	workerHash := sha256.Sum256([]byte(workerCode))
	provenance := map[string]any{"experiment_id": e.ID, "attempt": e.Attempt, "plan_version": e.PlanVersion, "plan_source": e.Plan.Source, "planner_model": e.Plan.Model, "narrative_source": result.NarrativeSource, "adapter": e.Plan.Adapter, "worker_sha256": hex.EncodeToString(workerHash[:]), "python_version": result.PythonVersion, "language": planLanguage(*e.Plan), "runtime_version": result.RuntimeVersion, "dependencies": result.Dependencies, "dataset": dataset, "execution_mode": j.mode, "worker_image": j.image, "api_go_version": runtime.Version(), "api_platform": runtime.GOOS + "/" + runtime.GOARCH, "created_at": time.Now().UTC(), "timeout_seconds": e.Plan.TimeoutSeconds, "seed_and_parameters": e.Plan.Parameters}
	if e.Plan.Code != "" {
		hash := sha256.Sum256([]byte(e.Plan.Code))
		provenance["experiment_code_sha256"] = hex.EncodeToString(hash[:])
	}
	if err = encode("provenance.json", provenance); err != nil {
		return err
	}
	artifacts := []Artifact{}
	checksums := map[string]string{}
	for name, data := range files {
		hash := sha256.Sum256(data)
		checksums[name] = hex.EncodeToString(hash[:])
	}
	if err = encode("checksums.json", checksums); err != nil {
		return err
	}
	for name, data := range files {
		hash := sha256.Sum256(data)
		mime := "application/json"
		switch filepath.Ext(name) {
		case ".md", ".txt", ".py", ".js", ".R", ".go", ".cpp", ".jl", ".rs", ".rb":
			mime = "text/plain; charset=utf-8"
		case ".csv":
			mime = "text/csv; charset=utf-8"
		case ".svg":
			mime = "image/svg+xml"
		}
		artifacts = append(artifacts, Artifact{prefix + "/" + name, mime, int64(len(data)), hex.EncodeToString(hash[:])})
	}
	objectPrefix := "experiments/" + e.ID + "/" + prefix + "/"
	committed := false
	defer func() {
		if !committed {
			cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = j.objectStorage().DeletePrefix(cleanup, objectPrefix)
		}
	}()
	for _, artifact := range artifacts {
		name := strings.TrimPrefix(artifact.Name, prefix+"/")
		if err = j.objectStorage().Put(ctx, objectPrefix+name, artifact.ContentType, files[name]); err != nil {
			return err
		}
	}
	if err = j.store.SaveArtifacts(e.ID, artifacts); err != nil {
		return err
	}
	committed = true
	return nil
}
func renderReport(e Experiment, r Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nResearch question: %s\n\n## Method\n\n%s\n\nHypothesis: %s\n\nAdapter: `%s`; plan version: %d; attempt: %d.\n\n## Assumptions\n\n", e.Title, e.Question, e.Plan.Methodology, e.Plan.Hypothesis, e.Plan.Adapter, e.PlanVersion, e.Attempt)
	for _, s := range e.Plan.Assumptions {
		fmt.Fprintf(&b, "- %s\n", s)
	}
	b.WriteString("\n## Computed findings\n\n")
	for _, s := range r.Findings {
		fmt.Fprintf(&b, "- %s\n", s)
	}
	b.WriteString("\n## Limitations\n\n")
	for _, s := range r.Limitations {
		fmt.Fprintf(&b, "- %s\n", s)
	}
	if r.Narrative != "" {
		fmt.Fprintf(&b, "\n## AI interpretation (not independently verified)\n\n%s\n\nSource: %s\n", r.Narrative, r.NarrativeSource)
	}
	b.WriteString("\n## Evidence and reproduction\n\nSee results.json for computed metrics and REPRODUCE.md for replay instructions. Numerical diagnostics are not experimental replications.\n")
	for _, s := range r.Warnings {
		fmt.Fprintf(&b, "\nWarning: %s\n", s)
	}
	return b.String()
}
func renderSVG(result Result, adapter string) string {
	series := result.Series
	minX, maxX, maxY, minY := 0., 0., 0., 0.
	for _, s := range series {
		for _, p := range s.Points {
			maxX = math.Max(maxX, p.X)
			minX = math.Min(minX, p.X)
			maxY = math.Max(maxY, p.Y)
			minY = math.Min(minY, p.Y)
		}
	}
	if maxX == minX {
		maxX = minX + 1
	}
	if maxY == minY {
		maxY = minY + 1
	}
	colors := []string{"#b9a6f5", "#7fb8de", "#91c5a4", "#d9b37e"}
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 440" role="img" aria-label="Computed experiment results"><rect width="800" height="440" fill="#151518"/><g font-family="sans-serif" font-size="12" fill="#a6a1b2">`)
	xlabel, ylabel := "x", "y"
	if adapter == "physics.projectile" {
		xlabel, ylabel = "x (m)", "height (m)"
	}
	if adapter == "cs.sorting" {
		xlabel, ylabel = "input size", "median runtime (ms)"
	}
	if adapter == "statistics.welch" {
		xlabel, ylabel = "group (0=A, 1=B)", "mean value"
	}
	if result.XAxisLabel != "" {
		xlabel = result.XAxisLabel
	}
	if result.YAxisLabel != "" {
		ylabel = result.YAxisLabel
	}
	for i := 0; i < 5; i++ {
		y := 340 - float64(i)*70
		value := minY + (maxY-minY)*float64(i)/4
		fmt.Fprintf(&b, `<line x1="65" x2="750" y1="%g" y2="%g" stroke="#29262f"/><text x="10" y="%g">%.3g</text>`, y, y, y+4, value)
		x := 65 + float64(i)*171.25
		fmt.Fprintf(&b, `<text x="%g" y="363">%.3g</text>`, x, minX+(maxX-minX)*float64(i)/4)
	}
	fmt.Fprintf(&b, `<text x="65" y="28">%s</text><text x="360" y="391">%s</text>`, html.EscapeString(ylabel), html.EscapeString(xlabel))
	for i, s := range series {
		color := colors[i%len(colors)]
		fmt.Fprintf(&b, `<polyline fill="none" stroke="%s" stroke-width="2" points="`, color)
		for _, p := range s.Points {
			fmt.Fprintf(&b, "%.2f,%.2f ", 65+(p.X-minX)/(maxX-minX)*685, 340-(p.Y-minY)/(maxY-minY)*280)
		}
		b.WriteString(`"/>`)
		fmt.Fprintf(&b, `<text x="%d" y="420" fill="%s">%s</text>`, 65+(i%4)*175, color, html.EscapeString(s.Name))
	}
	b.WriteString("</g></svg>")
	return b.String()
}
