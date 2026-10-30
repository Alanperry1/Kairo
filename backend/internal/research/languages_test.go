package research

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestLanguageSelectionAndValidation(t *testing.T) {
	p := samplePlan(t, "custom")
	p.Adapter = "language.experiment"
	p.Language = "javascript"
	p.Parameters = json.RawMessage(`{"inputs":{},"seed":42,"required_modules":["fs"]}`)
	r := NewRegistry("python3", "docker", true)
	r.SetLanguages([]Language{{ID: "javascript", Available: true, Modules: []string{"fs"}}})
	if err := r.Validate(p, true); err != nil {
		t.Fatal(err)
	}
	if err := validateLanguageChoice("R", p); err == nil {
		t.Fatal("user language was ignored")
	}
	if err := validateLanguageChoice("auto", p); err != nil {
		t.Fatal(err)
	}
	p.Language = "julia"
	if err := r.Validate(p, true); err == nil {
		t.Fatal("uninstalled runtime allowed")
	}
	if err := r.Validate(p, false); err != nil {
		t.Fatal("unavailable plan cannot be reviewed", err)
	}
	if _, err := (PythonExecutor{Mode: "local"}).Execute(context.Background(), WorkerInput{Plan: &p}); err == nil {
		t.Fatal("custom code ran locally")
	}
	p.Adapter = "python.experiment"
	if err := r.Validate(p, false); err == nil {
		t.Fatal("language and adapter mismatch accepted")
	}
}
func TestLanguagePreferenceAPI(t *testing.T) {
	s, h := testServer(t)
	w := request(t, h, "POST", "/api/v1/experiments", map[string]string{"question": "Compare numerical integration methods", "language": "R"})
	requireStatus(t, w, 201)
	var e Experiment
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	if e.Language != "r" {
		t.Fatal("language not persisted")
	}
	p := samplePlan(t, "custom")
	p.ResearchQuestion = e.Question
	requireStatus(t, request(t, h, "PUT", "/api/v1/experiments/"+e.ID+"/plan", p), 422)
	requireStatus(t, request(t, h, "PATCH", "/api/v1/experiments/"+e.ID, map[string]string{"language": "python"}), 200)
	requireStatus(t, request(t, h, "PUT", "/api/v1/experiments/"+e.ID+"/plan", p), 200)
	requireStatus(t, request(t, h, "PATCH", "/api/v1/experiments/"+e.ID, map[string]string{"language": "javascript"}), 200)
	stored, _ := s.Store.Get(e.ID)
	if stored.Plan != nil || stored.Language != "javascript" {
		t.Fatal("changing language did not clear previous plan")
	}
	requireStatus(t, request(t, h, "POST", "/api/v1/experiments", map[string]string{"question": "Scientific question", "language": "python; rm -rf"}), 422)
	requireStatus(t, request(t, h, "GET", "/api/v1/languages", nil), 200)
}
func TestPlannerHonorsRequestedLanguage(t *testing.T) {
	p := samplePlan(t, "custom")
	p.Adapter = "language.experiment"
	p.Language = "r"
	p.LanguageReason = "R provides statistical tools for this study."
	p.Parameters = json.RawMessage(`{"inputs":{},"seed":42,"required_modules":["jsonlite"]}`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		raw, _ := json.Marshal(req)
		if !strings.Contains(string(raw), "Selected language: r") || !strings.Contains(string(raw), "language_reason") {
			t.Error("language choice absent from planner request")
		}
		b, _ := json.Marshal(p)
		respond(w, 200, map[string]any{"response": string(b), "done": true})
	}))
	defer upstream.Close()
	r := NewRegistry("python3", "docker", true)
	r.SetLanguages([]Language{{ID: "r", Available: true, Modules: []string{"jsonlite"}}})
	plan, err := NewModelClient("ollama", upstream.URL, "test", "").Plan(context.Background(), Experiment{Question: p.ResearchQuestion, Language: "r"}, r, nil)
	if err != nil || plan.Language != "r" {
		t.Fatal("R plan rejected", err)
	}
}
func TestDockerLanguageRuntimes(t *testing.T) {
	if os.Getenv("KAIRO_TEST_DOCKER") != "1" {
		t.Skip("opt-in Docker runtime tests")
	}
	image := os.Getenv("KAIRO_TEST_WORKER_IMAGE")
	if image == "" {
		image = "researchos-worker:languages"
	}
	probe := exec.Command("docker", "run", "--rm", "--pull=never", "--network", "none", "--read-only", "--memory", "768m", "--cpus", "1", "--pids-limit", "64", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", "65534:65534", "--tmpfs", "/tmp:size=32m,nosuid,nodev", image, "python3", "-I", "-c", workerCode, "--capabilities")
	output, err := probe.CombinedOutput()
	if err != nil {
		t.Fatalf("capabilities: %v %s", err, output)
	}
	var caps WorkerCapabilities
	if err = json.Unmarshal(output, &caps); err != nil {
		t.Fatal(err, string(output))
	}
	registry := NewRegistry("python3", "docker", true)
	registry.SetAvailableModules(caps.Modules)
	registry.SetLanguages(caps.Languages)
	for _, language := range []string{"python", "javascript", "r", "go", "cpp"} {
		t.Run(language, func(t *testing.T) {
			code, err := os.ReadFile("testdata/languages/experiment." + map[string]string{"python": "py", "javascript": "js", "r": "R", "go": "go", "cpp": "cpp"}[language])
			if err != nil {
				t.Fatal(err)
			}
			p := samplePlan(t, "custom")
			p.Adapter = "language.experiment"
			p.Language = language
			if language == "python" {
				p.Adapter = "python.experiment"
			}
			p.Code = string(code)
			p.TimeoutSeconds = 180
			p.Parameters = json.RawMessage(`{"inputs":{"a":2},"seed":42,"required_modules":[]}`)
			if err := registry.Validate(p, true); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
			defer cancel()
			result, err := (PythonExecutor{Mode: "docker", Image: image}).Execute(ctx, WorkerInput{Plan: &p, DatasetRows: []map[string]string{{"value": "3"}}})
			if err != nil {
				t.Fatal(err)
			}
			if result.Metrics["sum"] != float64(5) || result.Language != language || result.RuntimeVersion == "" {
				t.Fatalf("incorrect results: %+v", result)
			}
			if language == "javascript" && (result.Metrics["secret_present"] != false || result.Metrics["uid"] != float64(65534)) {
				t.Fatal("worker credentials or privileges leaked")
			}
			t.Log("PASS actual isolated computation in", language)
		})
	}
}

type captureTransport func(*http.Request) (*http.Response, error)

func (f captureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLiveLanguagePlanningAndExecution(t *testing.T) {
	if os.Getenv("KAIRO_TEST_LIVE_MODEL") != "1" || os.Getenv("KAIRO_TEST_DOCKER") != "1" {
		t.Skip("opt-in live planner and Docker test")
	}
	image := "researchos-worker:languages"
	cmd := exec.Command("docker", "run", "--rm", "--network", "none", "--read-only", "--user", "65534:65534", "--tmpfs", "/tmp:size=32m,nosuid,nodev", image, "python3", "-I", "-c", workerCode, "--capabilities")
	raw, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var caps WorkerCapabilities
	if err = json.Unmarshal(raw, &caps); err != nil {
		t.Fatal(err)
	}
	model := NewModelClient(os.Getenv("MODEL_PROVIDER"), os.Getenv("MODEL_URL"), os.Getenv("MODEL_NAME"), os.Getenv("MODEL_API_KEY"))
	var captured string
	model.Client.Transport = captureTransport(func(req *http.Request) (*http.Response, error) {
		res, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			return res, err
		}
		raw, err := io.ReadAll(res.Body)
		res.Body.Close()
		res.Body = io.NopCloser(bytes.NewReader(raw))
		var payload struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text    string `json:"text"`
						Thought bool   `json:"thought"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if json.Unmarshal(raw, &payload) == nil {
			captured = ""
			for _, c := range payload.Candidates {
				for _, p := range c.Content.Parts {
					if !p.Thought {
						captured += p.Text
					}
				}
			}
		}
		return res, err
	})
	for _, language := range []string{"r", "auto"} {
		t.Run(language, func(t *testing.T) {
			s, h := testServer(t)
			s.Registry = NewRegistry("python3", "docker", true)
			s.Registry.SetAvailableModules(caps.Modules)
			s.Registry.SetLanguages(caps.Languages)
			s.Model = model
			s.Config.ModelProvider = model.Provider
			question := "Compare forward Euler integration with the exact solution for dy/dt=-0.2*y, y(0)=10, dt=0.5, t=0..10. Compute the maximum absolute error and final simulated value. This is an illustrative numerical simulation with all inputs supplied; no external data needed."
			w := request(t, h, "POST", "/api/v1/experiments", map[string]string{"question": question, "language": language})
			requireStatus(t, w, 201)
			var e Experiment
			_ = json.Unmarshal(w.Body.Bytes(), &e)
			w = request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/plan", nil)
			if w.Code != 200 {
				t.Logf("Synthetic study response excerpt: %.1800s", captured)
			}
			requireStatus(t, w, 200)
			_ = json.Unmarshal(w.Body.Bytes(), &e)
			if e.Plan == nil || e.Plan.LanguageReason == "" {
				t.Fatal("missing plan or language rationale")
			}
			if language != "auto" && e.Plan.Language != language {
				t.Fatal("selected language ignored")
			}
			if err = s.Registry.Validate(*e.Plan, true); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			result, err := (PythonExecutor{Mode: "docker", Image: image}).Execute(ctx, WorkerInput{Plan: e.Plan})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Metrics) == 0 || result.Language != e.Plan.Language {
				t.Fatal("missing computed evidence")
			}
			e.Attempt = 1
			if err = s.Jobs.writeArtifacts(e, WorkerInput{Plan: e.Plan}, result); err != nil {
				t.Fatal(err)
			}
			t.Log("PASS live Gemma plan → persisted language preference →", e.Plan.Language, "computation → reproducibility artifacts")
		})
	}
}
