package research

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	root := t.TempDir()
	store, err := OpenStore(testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := Config{DataDir: root, Python: "python3", WorkerMode: "local", Workers: 1, ModelProvider: "disabled", ModelName: "test-model"}
	reg := NewRegistry("python3", "local", true)
	model := NewModelClient("disabled", "", "", "")
	jobs := NewJobs(store, reg, PythonExecutor{Python: "python3", Mode: "local"}, model, root, "local", "", 1)
	s := &Server{cfg, store, reg, model, jobs}
	t.Cleanup(jobs.Close)
	return s, s.Handler()
}
func request(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func requireStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("got HTTP %d want %d: %s", w.Code, status, w.Body.String())
	}
}
func samplePlan(t *testing.T, name string) Plan {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", name+"-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p Plan
	if err = json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func createPlanned(t *testing.T, h http.Handler, p Plan) Experiment {
	t.Helper()
	w := request(t, h, "POST", "/api/v1/experiments", map[string]string{"question": p.ResearchQuestion})
	requireStatus(t, w, 201)
	var e Experiment
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	w = request(t, h, "PUT", "/api/v1/experiments/"+e.ID+"/plan", p)
	requireStatus(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	return e
}
func waitState(t *testing.T, s *Store, id string, statuses ...string) Experiment {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		e, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, status := range statuses {
			if e.Status == status {
				return e
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	e, _ := s.Get(id)
	t.Fatalf("timed out waiting for %v; got %s: %s", statuses, e.Status, e.Error)
	return e
}
func TestPhysicsWorkflowAndReproducibleExport(t *testing.T) {
	s, h := testServer(t)
	p := samplePlan(t, "projectile")
	e := createPlanned(t, h, p)
	base := "/api/v1/experiments/" + e.ID
	requireStatus(t, request(t, h, "POST", base+"/run", nil), 202)
	requireStatus(t, request(t, h, "POST", base+"/run", nil), 409)
	s.Jobs.Start()
	e = waitState(t, s.Store, e.ID, "completed", "failed")
	if e.Status != "completed" {
		t.Fatal(e.Error)
	}
	if e.Result.Kind != "simulation" || e.Result.NarrativeSource != "deterministic" {
		t.Fatalf("unexpected evidence: %+v", e.Result)
	}
	ranges := e.Result.Metrics["ranges"].([]any)
	baseline := ranges[0].(map[string]any)["range_m"].(float64)
	dragged := ranges[2].(map[string]any)["range_m"].(float64)
	if math.Abs(baseline-900/9.81) > .01 || dragged >= baseline {
		t.Fatalf("invalid physics: baseline=%g drag=%g", baseline, dragged)
	}
	requireStatus(t, request(t, h, "GET", base+"/report", nil), 200)
	w := request(t, h, "GET", base+"/bundle", nil)
	requireStatus(t, w, 200)
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		reader, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name], err = io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	prefix := "attempt-001/"
	for _, name := range []string{"worker.py", "input.json", "results.json", "provenance.json", "report.md", "chart.svg", "data.csv", "checksums.json", "REPRODUCE.md"} {
		if len(files[prefix+name]) == 0 {
			t.Fatalf("missing %s", name)
		}
	}
	cmd := exec.Command("python3", "-I", "-c", string(files[prefix+"worker.py"]))
	cmd.Stdin = bytes.NewReader(files[prefix+"input.json"])
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var replay Result
	if err = json.Unmarshal(output, &replay); err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(e.Result.Metrics)
	rerun, _ := json.Marshal(replay.Metrics)
	if !bytes.Equal(original, rerun) {
		t.Fatal("replayed metrics differ")
	}
	w = request(t, h, "GET", base+"/events/stream", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "event: completed") || !strings.Contains(w.Body.String(), "event: end") {
		t.Fatalf("missing final SSE events: %s", w.Body.String())
	}
	requireStatus(t, request(t, h, "POST", base+"/run", nil), 409)
}
func TestValidationVersioningAndCRUD(t *testing.T) {
	s, h := testServer(t)
	p := samplePlan(t, "projectile")
	e := createPlanned(t, h, p)
	base := "/api/v1/experiments/" + e.ID
	p.Hypothesis = "An explicitly revised hypothesis"
	requireStatus(t, request(t, h, "PUT", base+"/plan", p), 200)
	history, err := s.Store.PlanHistory(e.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("history: %v %v", history, err)
	}
	p.Adapter = "shell.execute"
	requireStatus(t, request(t, h, "PUT", base+"/plan", p), 422)
	requireStatus(t, request(t, h, "PATCH", base, map[string]string{"question": "A new research question?"}), 200)
	e, _ = s.Store.Get(e.ID)
	if e.Plan != nil || e.Status != "draft" {
		t.Fatal("question change did not invalidate plan")
	}
	requireStatus(t, request(t, h, "POST", base+"/run", nil), 409)
	requireStatus(t, request(t, h, "POST", base+"/plan", nil), 503)
	requireStatus(t, request(t, h, "GET", "/api/v1/experiments?limit=-1", nil), 400)
	requireStatus(t, request(t, h, "GET", base+"/events?after=-1", nil), 400)
	requireStatus(t, request(t, h, "POST", "/api/v1/experiments", map[string]any{"question": "test question", "unexpected": true}), 400)
	requireStatus(t, request(t, h, "DELETE", base, nil), 204)
	requireStatus(t, request(t, h, "GET", base, nil), 404)
}
func TestAuthAndCORS(t *testing.T) {
	s, _ := testServer(t)
	s.Config.Token = strings.Repeat("x", 32)
	s.Config.Origins = []string{"http://localhost:5173"}
	h := s.Handler()
	requireStatus(t, request(t, h, "GET", "/healthz", nil), 200)
	requireStatus(t, request(t, h, "GET", "/api/v1/settings", nil), 401)
	r := httptest.NewRequest("GET", "/api/v1/settings", nil)
	r.Header.Set("Authorization", "Bearer "+s.Config.Token)
	r.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	requireStatus(t, w, 200)
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatal("missing CORS header")
	}
	if strings.Contains(w.Body.String(), s.Config.Token) {
		t.Fatal("token leaked")
	}
	r.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	requireStatus(t, w, 403)
}
func upload(t *testing.T, h http.Handler, name, data string) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(part, data)
	_ = mw.Close()
	r := httptest.NewRequest("POST", "/api/v1/datasets", &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestDatasetsAndStatistics(t *testing.T) {
	s, h := testServer(t)
	requireStatus(t, upload(t, h, "bad.csv", "group,group\nA,1\n"), 422)
	requireStatus(t, upload(t, h, "bad.csv", "group,value\nA,1,2\n"), 422)
	raw, err := os.ReadFile("../../examples/groups.csv")
	if err != nil {
		t.Fatal(err)
	}
	w := upload(t, h, "groups.csv", string(raw))
	requireStatus(t, w, 201)
	var d Dataset
	if err = json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Rows != 12 || len(d.Preview) != 5 {
		t.Fatalf("bad dataset: %+v", d)
	}
	p := samplePlan(t, "statistics")
	w = request(t, h, "POST", "/api/v1/experiments", map[string]string{"question": p.ResearchQuestion, "dataset_id": d.ID})
	requireStatus(t, w, 201)
	var e Experiment
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	base := "/api/v1/experiments/" + e.ID
	requireStatus(t, request(t, h, "PUT", base+"/plan", p), 200)
	if exec.Command("python3", "-I", "-c", "import scipy").Run() != nil {
		t.Skip("SciPy unavailable; upload and planning validated")
	}
	requireStatus(t, request(t, h, "POST", base+"/run", nil), 202)
	s.Jobs.Start()
	e = waitState(t, s.Store, e.ID, "completed", "failed")
	if e.Status != "completed" {
		t.Fatal(e.Error)
	}
	m := e.Result.Metrics
	if math.Abs(m["mean_difference_a_minus_b"].(float64)+4) > .00001 || m["p_value"].(float64) >= .05 {
		t.Fatalf("unexpected statistics: %+v", m)
	}
	if err = os.WriteFile(filepath.Join(s.Config.DataDir, "datasets", d.ID+".csv"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Jobs.Input(e); err == nil {
		t.Fatal("tampered dataset accepted")
	}
}
func TestRegistryRejectsDangerousParameters(t *testing.T) {
	reg := NewRegistry("python3", "local", false)
	p := samplePlan(t, "projectile")
	if err := reg.Validate(p, true); err != nil {
		t.Fatal(err)
	}
	var params map[string]any
	_ = json.Unmarshal(p.Parameters, &params)
	params["command"] = "arbitrary code"
	p.Parameters, _ = json.Marshal(params)
	if reg.Validate(p, true) == nil {
		t.Fatal("unknown code parameter accepted")
	}
	delete(params, "command")
	params["drag_coefficients"] = []float64{.1, .5}
	p.Parameters, _ = json.Marshal(params)
	if reg.Validate(p, true) == nil {
		t.Fatal("missing baseline accepted")
	}
	p = samplePlan(t, "statistics")
	if reg.Validate(p, true) == nil {
		t.Fatal("unavailable SciPy accepted")
	}
}
func TestSortingWorker(t *testing.T) {
	p := samplePlan(t, "sorting")
	p.Parameters = json.RawMessage(`{"sizes":[10,30],"trials":3,"seed":42}`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := (PythonExecutor{Python: "python3", Mode: "local"}).Execute(ctx, WorkerInput{Plan: &p})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != "benchmark" || res.Metrics["correctness_checks_passed"] != true || len(res.Series) != 3 {
		t.Fatalf("bad benchmark: %+v", res)
	}
}

type blockingExecutor struct {
	start chan struct{}
	once  sync.Once
}

func (b *blockingExecutor) Execute(ctx context.Context, input WorkerInput) (Result, error) {
	b.once.Do(func() { close(b.start) })
	<-ctx.Done()
	return Result{}, ctx.Err()
}
func TestCancellationDoesNotBecomeFailure(t *testing.T) {
	s, h := testServer(t)
	blocked := &blockingExecutor{start: make(chan struct{})}
	s.Jobs.executor = blocked
	e := createPlanned(t, h, samplePlan(t, "projectile"))
	base := "/api/v1/experiments/" + e.ID
	requireStatus(t, request(t, h, "POST", base+"/run", nil), 202)
	s.Jobs.Start()
	select {
	case <-blocked.start:
	case <-time.After(3 * time.Second):
		t.Fatal("worker not started")
	}
	requireStatus(t, request(t, h, "DELETE", base, nil), 409)
	requireStatus(t, request(t, h, "POST", base+"/cancel", nil), 200)
	s.Jobs.Close()
	e, _ = s.Store.Get(e.ID)
	if e.Status != "cancelled" {
		t.Fatalf("cancellation became %s", e.Status)
	}
}
func TestRestartRecoveryAndPersistence(t *testing.T) {
	path := testDatabaseURL(t)
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, status := range []string{"running", "planning", "queued", "draft"} {
		if err = store.Create(Experiment{ID: status, Title: status, Status: status, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()
	store, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Recover(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"running", "planning"} {
		e, _ := store.Get(id)
		if e.Status != "failed" {
			t.Fatalf("%s not recovered", id)
		}
	}
	e, _ := store.Get("queued")
	if e.Status != "queued" {
		t.Fatal("durable queue lost")
	}
	_, err = store.Get("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
