package research

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
