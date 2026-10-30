package research

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOllamaPlannerAndSingleRepair(t *testing.T) {
	p := samplePlan(t, "projectile")
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["stream"] != false || req["format"] != "json" {
			t.Error("structured output not requested")
		}
		text := `{"adapter":"shell.execute"}`
		if calls.Add(1) > 1 {
			if !strings.Contains(req["prompt"].(string), "Previous model response") || !strings.Contains(req["prompt"].(string), "shell.execute") {
				t.Error("repair did not include the failed response")
			}
			b, _ := json.Marshal(p)
			text = string(b)
		}
		respond(w, 200, map[string]any{"response": text, "done": true})
	}))
	defer upstream.Close()
	m := NewModelClient("ollama", upstream.URL, "qwen3:test", "")
	e := Experiment{Question: p.ResearchQuestion}
	got, err := m.Plan(context.Background(), e, NewRegistry("python3", "local", true), nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || got.Source != "model" || got.Model != "qwen3:test" {
		t.Fatalf("unexpected plan: %+v calls=%d", got, calls.Load())
	}
}
func TestCompatibleProvider(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("wrong endpoint or authorization")
		}
		respond(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "Evidence-based explanation"}, "finish_reason": "stop"}}})
	}))
	defer upstream.Close()
	m := NewModelClient("compatible", upstream.URL+"/v1", "open-weight-model", "secret")
	text, err := m.Generate(context.Background(), "system", "prompt", false)
	if err != nil || text != "Evidence-based explanation" {
		t.Fatalf("%q %v", text, err)
	}
}
func TestModelFailureDoesNotFabricatePlan(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte("secret upstream detail"))
	}))
	defer upstream.Close()
	m := NewModelClient("ollama", upstream.URL, "qwen3:test", "")
	_, err := m.Plan(context.Background(), Experiment{Question: "test question"}, NewRegistry("python3", "local", false), nil)
	if err == nil || strings.Contains(err.Error(), "secret upstream detail") {
		t.Fatalf("bad upstream error: %v", err)
	}
}
func TestInvalidModelPlanBecomesNeedsReview(t *testing.T) {
	s, _ := testServer(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"response": `{"error":"This question needs laboratory equipment"}`, "done": true})
	}))
	defer upstream.Close()
	s.Config.ModelProvider = "ollama"
	s.Model = NewModelClient("ollama", upstream.URL, "qwen3:test", "")
	h := s.Handler()
	w := request(t, h, "POST", "/api/v1/experiments", map[string]string{"question": "Can you synthesize a new compound?"})
	requireStatus(t, w, 201)
	var e Experiment
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	requireStatus(t, request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/plan", nil), 422)
	e, _ = s.Store.Get(e.ID)
	if e.Status != "needs_review" || e.Plan != nil {
		t.Fatal("unsupported scientific request was not blocked")
	}
}

func TestCancelledPlannerCannotOverwriteNewPlan(t *testing.T) {
	s, _ := testServer(t)
	p := samplePlan(t, "projectile")
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := p
		if calls.Add(1) == 1 {
			close(started)
			<-release
			current.Hypothesis = "Stale hypothesis"
		} else {
			current.Hypothesis = "New explicitly reviewed hypothesis"
		}
		b, _ := json.Marshal(current)
		respond(w, 200, map[string]any{"response": string(b), "done": true})
	}))
	defer upstream.Close()
	s.Config.ModelProvider = "ollama"
	s.Model = NewModelClient("ollama", upstream.URL, "qwen3:test", "")
	h := s.Handler()
	w := request(t, h, "POST", "/api/v1/experiments", map[string]string{"question": p.ResearchQuestion})
	requireStatus(t, w, 201)
	var e Experiment
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	base := "/api/v1/experiments/" + e.ID
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request(t, h, "POST", base+"/plan", nil) }()
	<-started
	requireStatus(t, request(t, h, "POST", base+"/cancel", nil), 200)
	requireStatus(t, request(t, h, "POST", base+"/plan", nil), 200)
	close(release)
	requireStatus(t, <-done, 409)
	e, _ = s.Store.Get(e.ID)
	if e.Plan.Hypothesis != "New explicitly reviewed hypothesis" || e.PlanVersion != 1 {
		t.Fatalf("stale plan overwrote new plan: %+v", e)
	}
}
func TestExplanationFailurePreservesEvidence(t *testing.T) {
	s, h := testServer(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer upstream.Close()
	s.Jobs.model = NewModelClient("ollama", upstream.URL, "qwen3:test", "")
	e := createPlanned(t, h, samplePlan(t, "projectile"))
	requireStatus(t, request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/run", nil), 202)
	s.Jobs.Start()
	e = waitState(t, s.Store, e.ID, "completed", "failed")
	if e.Status != "completed" || e.Result.NarrativeSource != "deterministic" || len(e.Result.Warnings) == 0 {
		t.Fatalf("evidence not preserved: %+v", e)
	}
}

func TestGoogleGemmaProvider(t *testing.T) {
	for _, jsonMode := range []bool{true, false} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.URL.Path != "/v1beta/models/gemma-4-26b-a4b-it:generateContent" || r.Header.Get("x-goog-api-key") != "test-private-key" || r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
				t.Error("wrong native Google request or credential transport")
			}
			var body struct {
				SystemInstruction struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"systemInstruction"`
				Contents []struct {
					Role  string `json:"role"`
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"contents"`
				GenerationConfig map[string]any `json:"generationConfig"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			if body.SystemInstruction.Parts[0].Text != "Return JSON" || body.Contents[0].Parts[0].Text != "my question" || body.Contents[0].Role != "user" {
				t.Error("lost system instruction or question")
			}
			if jsonMode && body.GenerationConfig["responseMimeType"] != "application/json" {
				t.Error("JSON output was not requested")
			}
			if !jsonMode && body.GenerationConfig["responseMimeType"] != nil {
				t.Error("JSON mode forced on narrative")
			}
			if body.GenerationConfig["thinkingConfig"].(map[string]any)["thinkingLevel"] != "minimal" {
				t.Error("Gemma thinking was not disabled")
			}
			respond(w, 200, map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"parts": []any{map[string]any{"text": "Reasoning that must not enter the research plan", "thought": true}, map[string]any{"text": `{"ok":true}`}}}}}})
		}))
		model := NewModelClient("google", upstream.URL+"/v1beta", "gemma-4-26b-a4b-it", "test-private-key")
		text, err := model.Generate(context.Background(), "Return JSON", "my question", jsonMode)
		upstream.Close()
		if err != nil || text != `{"ok":true}` {
			t.Fatalf("final text was not isolated: %q %v", text, err)
		}
	}
}

func TestGoogleRejectsBlockedIncompleteAndThinkingOnlyAnswers(t *testing.T) {
	for _, response := range []string{
		`{"promptFeedback":{"blockReason":"SAFETY"}}`,
		`{"candidates":[{"finishReason":"MAX_TOKENS","content":{"parts":[{"text":"partial"}]}}]}`,
		`{"candidates":[{"finishReason":"STOP","content":{"parts":[{"thought":true,"text":"reasoning only"}]}}]}`,
	} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(response))
		}))
		_, err := NewModelClient("google", upstream.URL, "gemma-4-26b-a4b-it", "test-key").Generate(context.Background(), "system", "question", true)
		upstream.Close()
		if err == nil {
			t.Fatal("non-final model output was accepted")
		}
	}
}

// Opt-in smoke test: this sends one small request to the configured Google API.
func TestLiveGoogleGemmaConnection(t *testing.T) {
	if os.Getenv("KAIRO_TEST_LIVE_MODEL") != "1" {
		t.Skip("set KAIRO_TEST_LIVE_MODEL=1 to test configured Google credentials")
	}
	if os.Getenv("MODEL_PROVIDER") != "google" {
		t.Skip("this smoke test requires MODEL_PROVIDER=google")
	}
	m := NewModelClient("google", os.Getenv("MODEL_URL"), os.Getenv("MODEL_NAME"), os.Getenv("MODEL_API_KEY"))
	text, err := m.Generate(context.Background(), "Return exactly one JSON object without Markdown.", `Return {"ok":true}.`, true)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err = json.Unmarshal([]byte(text), &result); err != nil || !result.OK {
		t.Fatal("Google did not return the expected JSON object")
	}
	t.Log("Google Gemma authentication, native generation, and JSON output verified")
}

func TestModelPlanTimeoutUsesWorkerBudgetWithoutChangingStudy(t *testing.T) {
	for _, tc := range []struct {
		name          string
		timeout, want int
		omit          bool
	}{
		{"omitted", 0, 120, true}, {"zero", 0, 120, false}, {"valid", 90, 90, false}, {"maximum", 300, 300, false}, {"oversized", 600, 300, false}, {"negative", -1, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := samplePlan(t, "custom")
			original.TimeoutSeconds = tc.timeout
			raw, _ := json.Marshal(original)
			if tc.omit {
				var fields map[string]json.RawMessage
				_ = json.Unmarshal(raw, &fields)
				delete(fields, "timeout_seconds")
				raw, _ = json.Marshal(fields)
			}
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				respond(w, 200, map[string]any{"response": string(raw), "done": true})
			}))
			defer upstream.Close()
			got, err := NewModelClient("ollama", upstream.URL, "test-model", "").Plan(context.Background(), Experiment{Question: original.ResearchQuestion}, NewRegistry("python3", "docker", true), nil)
			if tc.timeout < 0 {
				if err == nil || calls.Load() != 2 {
					t.Fatal("invalid negative timeout must still fail after repair", err, calls.Load())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.TimeoutSeconds != tc.want || calls.Load() != 1 {
				t.Fatal("invalid model budget caused an unnecessary repair or failure", got.TimeoutSeconds, calls.Load())
			}
			var expectedParams, actualParams any
			_ = json.Unmarshal(original.Parameters, &expectedParams)
			_ = json.Unmarshal(got.Parameters, &actualParams)
			if got.Code != original.Code || got.ResearchQuestion != original.ResearchQuestion || got.Hypothesis != original.Hypothesis || got.Methodology != original.Methodology || !reflect.DeepEqual(expectedParams, actualParams) {
				t.Fatal("time-budget normalization changed the scientific study")
			}
		})
	}
	p := samplePlan(t, "custom")
	p.TimeoutSeconds = 600
	if err := NewRegistry("python3", "docker", true).Validate(p, false); err == nil || !strings.Contains(err.Error(), "timeout_seconds") {
		t.Fatal("manual plan validation no longer enforces the worker limit", err)
	}
}
