package research

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func completedDocumentExperiment(t *testing.T, s *Server) Experiment {
	t.Helper()
	now := time.Now().UTC()
	p := samplePlan(t, "custom")
	e := Experiment{ID: newID("exp"), Title: "Population & growth_100%", Question: p.ResearchQuestion, Status: "completed", Attempt: 1, PlanVersion: 2, Progress: 100, Plan: &p, Result: &Result{Kind: "simulation", Metrics: map[string]any{"final_population": 99.4, "initial_population": 10}, Findings: []string{"The computed final population is 99.4 individuals."}, Limitations: []string{"Illustrative model only; no observed biological evidence."}, PythonVersion: "3.12.0", Dependencies: map[string]string{"python": "3.12.0"}, CSV: "time,population\n0,10\n30,99.4\n"}, CreatedAt: now, UpdatedAt: now}
	if err := s.Store.Create(e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestResearchDocumentsPersistDownloadAndCascade(t *testing.T) {
	s, h := testServer(t)
	e := completedDocumentExperiment(t, s)
	w := request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/documents", map[string]string{"kind": "manuscript"})
	requireStatus(t, w, 201)
	var d ResearchDocument
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Kind != "manuscript" || d.Source != "deterministic" || d.Attempt != 1 || d.PlanVersion != 2 || !strings.Contains(d.Content, `\begin{document}`) || !strings.Contains(d.Content, "99.4") {
		t.Fatal("manuscript lost recorded evidence")
	}
	saved, err := s.loadDocument(context.Background(), d.ID)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(saved.Evidence)
	if saved.EvidenceSHA256 != hex.EncodeToString(hash[:]) {
		t.Fatal("snapshot hash mismatch")
	}
	w = request(t, h, "GET", "/api/v1/documents?kind=manuscript", nil)
	requireStatus(t, w, 200)
	var listing struct {
		Data []ResearchDocument `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &listing)
	if len(listing.Data) != 1 || listing.Data[0].Content != "" || len(listing.Data[0].Evidence) != 0 {
		t.Fatal("document listing overfetches content or lost metadata")
	}
	w = request(t, h, "GET", "/api/v1/documents/"+d.ID+"/download", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Header().Get("Content-Disposition"), ".tex") || w.Body.String() != d.Content {
		t.Fatal("LaTeX download mismatch")
	}
	w = request(t, h, "GET", "/api/v1/documents/"+d.ID+"/bundle", nil)
	requireStatus(t, w, 200)
	archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string][]byte{}
	for _, entry := range archive.File {
		reader, _ := entry.Open()
		found[entry.Name], _ = io.ReadAll(reader)
		reader.Close()
	}
	if string(found["main.tex"]) != d.Content || !bytes.Equal(found["evidence.json"], saved.Evidence) || len(found["results.json"]) == 0 || len(found["experiment.py"]) == 0 {
		t.Fatal("source bundle is incomplete")
	}
	w = request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/documents", map[string]string{"kind": "report"})
	requireStatus(t, w, 201)
	if !strings.Contains(w.Body.String(), "Recorded metrics") {
		t.Fatal("report missing recorded metrics")
	}
	requireStatus(t, request(t, h, "DELETE", "/api/v1/experiments/"+e.ID, nil), 204)
	requireStatus(t, request(t, h, "GET", "/api/v1/documents/"+d.ID, nil), 404)
}

func TestResearchDocumentRequiresCompletedEvidence(t *testing.T) {
	s, h := testServer(t)
	now := time.Now().UTC()
	e := Experiment{ID: newID("exp"), Question: "What changes growth?", Title: "Draft", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := s.Store.Create(e); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/documents", map[string]string{"kind": "manuscript"}), 409)
	requireStatus(t, request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/documents", map[string]string{"kind": "shell"}), 400)
	requireStatus(t, request(t, h, "GET", "/api/v1/documents?kind=unknown", nil), 400)
}

func TestWritingAgentUsesEvidenceAndRejectsIncompleteDraft(t *testing.T) {
	e := Experiment{Title: "Computed growth", Question: "How does growth change?", Plan: &Plan{Hypothesis: "A model hypothesis", Methodology: "Exact logistic equation"}, Result: &Result{Metrics: map[string]any{"final_population": 99.4}, Findings: []string{"Recorded final value is 99.4"}}}
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if !strings.Contains(body.Messages[1].Content, "99.4") || !strings.Contains(body.Messages[0].Content, "Never invent") {
			t.Error("writing prompt lost evidence constraints")
		}
		content := `{"title":"Missing sections"}`
		if calls == 0 {
			content = `{"title":"Growth study","abstract":"Recorded model findings.","introduction":"A computational question.","methods":"An exact equation.","results":"The recorded final population is 99.4.","discussion":"Limited to the recorded simulation.","conclusion":"Review before extrapolating."}`
		}
		calls++
		respond(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}, "finish_reason": "stop"}}})
	}))
	defer upstream.Close()
	m := NewModelClient("compatible", upstream.URL, "test-writer", "")
	draft, source, err := m.DraftResearchDocument(context.Background(), e, "manuscript")
	if err != nil || source != "model" || !strings.Contains(draft.Results, "99.4") {
		t.Fatalf("writing failed: %v", err)
	}
	if _, _, err = m.DraftResearchDocument(context.Background(), e, "report"); err == nil {
		t.Fatal("partial model draft silently used fallback sections")
	}
}

func TestLatexExportEscapesTextAndCompiles(t *testing.T) {
	e := Experiment{ID: "exp_latex", Title: `Growth & 100%_test \input{/etc/passwd}`, Question: "A test", Attempt: 1, PlanVersion: 1, Plan: &Plan{Methodology: "A & B", Assumptions: []string{"Growth_rate > 0"}, Limitations: []string{"No observed data"}}, Result: &Result{Metrics: map[string]any{"mean_%": 2.5}, Findings: []string{"A computed value of 2.5."}, PythonVersion: "3.12.0"}}
	draft, _, err := NewModelClient("disabled", "", "", "").DraftResearchDocument(context.Background(), e, "manuscript")
	if err != nil {
		t.Fatal(err)
	}
	source := renderResearchDocument(e, draft, "manuscript")
	if strings.Contains(source, `\input{/etc/passwd}`) || !strings.Contains(source, `\&`) || !strings.Contains(source, `\%`) || !strings.Contains(source, `\_`) {
		t.Fatal("unescaped text could become executable LaTeX")
	}
	engine, err := exec.LookPath("pdflatex")
	if err != nil {
		t.Skip("pdfLaTeX is not installed; source escaping was checked")
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "main.tex"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, engine, "--no-shell-escape", "--interaction=nonstopmode", "--halt-on-error", "main.tex")
	cmd.Dir = dir
	// The compiler does not need the application's credentials.
	for _, entry := range os.Environ() {
		key := strings.SplitN(entry, "=", 2)[0]
		if !strings.Contains(key, "KEY") && !strings.Contains(key, "TOKEN") && !strings.Contains(key, "DATABASE_URL") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("LaTeX source did not compile: %v\n%s", err, output)
	}
	if info, err := os.Stat(filepath.Join(dir, "main.pdf")); err != nil || info.Size() == 0 {
		t.Fatal("LaTeX did not produce a PDF")
	}
}

func TestLiveWritingAgentDocument(t *testing.T) {
	if os.Getenv("KAIRO_TEST_LIVE_MODEL") != "1" {
		t.Skip("opt-in live writing agent test")
	}
	s, h := testServer(t)
	s.Model = NewModelClient(os.Getenv("MODEL_PROVIDER"), os.Getenv("MODEL_URL"), os.Getenv("MODEL_NAME"), os.Getenv("MODEL_API_KEY"))
	e := completedDocumentExperiment(t, s)
	w := request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/documents", map[string]string{"kind": "manuscript"})
	requireStatus(t, w, 201)
	var d ResearchDocument
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Source != "model" || !strings.Contains(d.Content, "99.4") {
		t.Fatal("missing AI draft or recorded metrics")
	}
	saved, err := s.loadDocument(context.Background(), d.ID)
	if err != nil || saved.Content != d.Content {
		t.Fatal("AI manuscript was not persisted")
	}
	t.Log("PASS: live writing agent → manuscript API → Postgres → LaTeX source with recorded metrics")
}
