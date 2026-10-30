package research

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneralPlanSupportsNewScientificDomains(t *testing.T) {
	p := samplePlan(t, "custom")
	r := NewRegistry("python3", "local", true)
	for _, domain := range []string{"biology", "economics", "materials_science", "mathematics"} {
		p.Domain = domain
		if err := r.Validate(p, false); err != nil {
			t.Fatalf("general domain %s rejected: %v", domain, err)
		}
	}
	if err := r.Validate(p, true); err == nil {
		t.Fatal("local generated-code execution was accepted")
	}
	p.Code = ""
	if err := r.Validate(p, false); err == nil {
		t.Fatal("missing code accepted")
	}
}
func TestGeneralPlannerDoesNotForceAnExample(t *testing.T) {
	p := samplePlan(t, "custom")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !strings.Contains(req["system"].(string), "general-purpose") {
			t.Error("general planner instructions missing")
		}
		b, _ := json.Marshal(p)
		respond(w, 200, map[string]any{"response": string(b), "done": true})
	}))
	defer upstream.Close()
	model := NewModelClient("ollama", upstream.URL, "test-model", "")
	plan, err := model.Plan(context.Background(), Experiment{Question: p.ResearchQuestion}, NewRegistry("python3", "local", true), nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Adapter != "python.experiment" || plan.Domain != "biology" || plan.Code == "" {
		t.Fatalf("custom question mapped to wrong plan: %+v", plan)
	}
}
func TestGeneralExecutorCannotRunCustomCodeLocally(t *testing.T) {
	p := samplePlan(t, "custom")
	p.Code = "raise RuntimeError('SHOULD_NOT_EXECUTE')"
	_, err := (PythonExecutor{Python: "python3", Mode: "local"}).Execute(context.Background(), WorkerInput{Plan: &p})
	if err == nil || !strings.Contains(err.Error(), "Docker") || strings.Contains(err.Error(), "SHOULD_NOT_EXECUTE") {
		t.Fatalf("local execution not blocked: %v", err)
	}
}
func TestGeneralAPISavesPlanButRefusesUnavailableExecution(t *testing.T) {
	s, h := testServer(t)
	e := createPlanned(t, h, samplePlan(t, "custom"))
	w := request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/run", nil)
	requireStatus(t, w, 422)
	stored, err := s.Store.Get(e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "planned" || stored.Attempt != 0 {
		t.Fatal("unavailable custom run altered state")
	}
}
func trustedGeneralFixture(t *testing.T) Result {
	t.Helper()
	p := samplePlan(t, "custom")
	input, _ := json.Marshal(WorkerInput{Plan: &p})
	// This unit test executes only the checked-in, trusted biology fixture.
	// Production custom plans are independently restricted to Docker in Go.
	cmd := exec.Command("python3", "-I", "-c", workerCode)
	cmd.Env = append(os.Environ(), "KAIRO_ISOLATED_WORKER=1")
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("trusted custom fixture: %v: %s", err, out)
	}
	var result Result
	if err = json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestGeneralWorkerComputesBiologyModel(t *testing.T) {
	result := trustedGeneralFixture(t)
	if result.Kind != "simulation" || len(result.Series) != 2 || result.XAxisLabel == "" {
		t.Fatalf("bad custom result: %+v", result)
	}
	a := result.Metrics["K=100"].(map[string]any)["final_population"].(float64)
	b := result.Metrics["K=200"].(map[string]any)["final_population"].(float64)
	if a < 99 || b < 199 || b <= a {
		t.Fatalf("bad logistic model: %g %g", a, b)
	}
}
func TestGeneralArtifactsIncludeCustomSourceAndAxes(t *testing.T) {
	s, h := testServer(t)
	e := createPlanned(t, h, samplePlan(t, "custom"))
	e.Attempt = 1
	result := trustedGeneralFixture(t)
	if err := s.Jobs.writeArtifacts(e, WorkerInput{Plan: e.Plan}, result); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(s.Config.DataDir, "experiments", e.ID, "attempt-001")
	source, err := os.ReadFile(filepath.Join(base, "experiment.py"))
	if err != nil || string(source) != e.Plan.Code {
		t.Fatal("exact custom source not exported")
	}
	chart, err := os.ReadFile(filepath.Join(base, "chart.svg"))
	if err != nil || !strings.Contains(string(chart), "Population (individuals)") {
		t.Fatal("custom axis labels missing")
	}
	provenance, err := os.ReadFile(filepath.Join(base, "provenance.json"))
	if err != nil || !strings.Contains(string(provenance), "experiment_code_sha256") {
		t.Fatal("code provenance missing")
	}
}
func TestGeneralModuleRequirementsAreChecked(t *testing.T) {
	r := NewRegistry("python3", "docker", false)
	r.SetAvailableModules([]string{"numpy"})
	p := samplePlan(t, "custom")
	p.Parameters = json.RawMessage(`{"inputs":{},"seed":42,"required_modules":["missing_science_library"]}`)
	if err := r.Validate(p, true); err == nil || !strings.Contains(err.Error(), "missing_science_library") {
		t.Fatalf("missing dependency not reported: %v", err)
	}
}

func TestGeneralDockerWorkflow(t *testing.T) {
	if os.Getenv("KAIRO_TEST_DOCKER") != "1" {
		t.Skip("set KAIRO_TEST_DOCKER=1 after building the worker image")
	}
	t.Setenv("API_TOKEN", "sentinel-not-for-worker")
	s, h := testServer(t)
	s.Config.WorkerMode = "docker"
	s.Registry = NewRegistry("python3", "docker", true)
	s.Jobs.registry = s.Registry
	s.Jobs.executor = PythonExecutor{Mode: "docker", Image: "researchos-worker:local"}
	s.Jobs.mode = "docker"
	s.Jobs.image = "researchos-worker:local"
	p := samplePlan(t, "custom")
	p.Code += "\nbase_run=run_experiment\ndef run_experiment(inputs,dataset_rows):\n    import os\n    result=base_run(inputs,dataset_rows)\n    result['metrics']['worker_uid']=os.geteuid()\n    result['metrics']['backend_secret_present']='API_TOKEN' in os.environ\n    return result\n"
	e := createPlanned(t, h, p)
	requireStatus(t, request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/run", nil), 202)
	s.Jobs.Start()
	e = waitState(t, s.Store, e.ID, "completed", "failed")
	if e.Status != "completed" {
		t.Fatal(e.Error)
	}
	if e.Result.Metrics["worker_uid"].(float64) != 65534 || e.Result.Metrics["backend_secret_present"] != false {
		t.Fatal("container received backend privileges or credentials")
	}
	if e.Plan.Domain != "biology" || e.Result.Kind != "simulation" {
		t.Fatal("custom domain workflow did not execute")
	}
	if !strings.Contains(e.Result.CSV, "\n") {
		t.Fatal("custom CSV output has no row delimiters")
	}
	requireStatus(t, request(t, h, "GET", "/api/v1/experiments/"+e.ID+"/bundle", nil), 200)
}

func TestGeneralInputUsesArbitraryDatasetColumns(t *testing.T) {
	s, h := testServer(t)
	w := upload(t, h, "observations.csv", "time,population,condition\n0,10,baseline\n1,12,treatment\n")
	requireStatus(t, w, 201)
	var d Dataset
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	p := samplePlan(t, "custom")
	w = request(t, h, "POST", "/api/v1/experiments", map[string]string{"question": p.ResearchQuestion, "dataset_id": d.ID})
	requireStatus(t, w, 201)
	var e Experiment
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	w = request(t, h, "PUT", "/api/v1/experiments/"+e.ID+"/plan", p)
	requireStatus(t, w, 200)
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	input, err := s.Jobs.Input(e)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.DatasetRows) != 2 || input.DatasetRows[1]["population"] != "12" || input.DatasetRows[1]["condition"] != "treatment" {
		t.Fatalf("generic data columns were lost: %+v", input.DatasetRows)
	}
}
