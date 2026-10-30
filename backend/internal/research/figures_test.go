package research

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image/png"
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

func TestFigureCatalogPreservesEvidence(t *testing.T) {
	result := &Result{XAxisLabel: "Time (s)", YAxisLabel: "Population", Series: []Series{{Name: "Control", Points: []Point{{X: 0, Y: 10}, {X: 1, Y: 15}, {X: 2, Y: 19}}}}}
	plots := figureCatalog(result)
	if len(plots) != 1 || plots[0].Series[0].Points[1].Y != 15 || plots[0].XLabel != "Time (s)" {
		t.Fatal("figure source data changed")
	}
	if _, err := selectFigureSpecs(result, []string{"invented-plot"}); err == nil {
		t.Fatal("invented figure accepted")
	}
	if _, err := selectFigureSpecs(result, []string{"recorded-series", "recorded-series"}); err == nil {
		t.Fatal("duplicate figures accepted")
	}
	if plots = figureCatalog(&Result{Metrics: map[string]any{"mean": 2.0, "runtime": 10.0}}); len(plots) != 0 {
		t.Fatal("unrelated scalar metrics compared as if they had shared units")
	}
	if plots = figureCatalog(&Result{CSV: "time,value\n0,1\n1,missing\n2,3\n"}); len(plots) != 0 {
		t.Fatal("invalid CSV observations were silently dropped")
	}
	plots = figureCatalog(&Result{Metrics: map[string]any{"control": map[string]any{"mean_ms": 1.0}, "treatment": map[string]any{"mean_ms": 2.0}}})
	if len(plots) != 1 || plots[0].Kind != "bar" || plots[0].Values[1] != 2 {
		t.Fatal("matched-condition metric comparison missing")
	}
}
func TestMatplotlibFiguresAndLatexCompile(t *testing.T) {
	s := &Server{Config: Config{Python: "python3", WorkerMode: "local", DataDir: t.TempDir()}}
	result := &Result{XAxisLabel: "Time (s)", YAxisLabel: "Recorded value", Series: []Series{{Name: "Control", Points: []Point{{X: 0, Y: 1}, {X: 1, Y: 2}, {X: 2, Y: 3}}}, {Name: "Treatment", Points: []Point{{X: 0, Y: 1}, {X: 1, Y: 3}, {X: 2, Y: 5}}}}}
	figures, assets, err := s.renderDocumentFigures(context.Background(), figureCatalog(result))
	if err != nil {
		t.Fatal(err)
	}
	if len(figures) != 1 || !strings.HasPrefix(figures[0].Renderer, "Matplotlib ") {
		t.Fatal("renderer provenance missing")
	}
	if _, err = png.DecodeConfig(bytes.NewReader(assets[figures[0].Name])); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(assets[figures[0].PDF], []byte("%PDF-")) {
		t.Fatal("vector PDF missing")
	}
	e := Experiment{ID: "exp_figures", Title: "Growth & comparison", Question: "A numerical comparison", Plan: &Plan{Methodology: "Recorded deterministic computation", Assumptions: []string{"Model inputs are fixed"}, Limitations: []string{"Illustrative data"}}, Result: result}
	d := documentSections{Title: e.Title, Abstract: "A recorded comparison.", Introduction: e.Question, Methods: e.Plan.Methodology, Results: "The saved series are plotted in Figure 1.", Discussion: "No observed causal evidence.", Conclusion: "Review the model assumptions."}
	source := renderResearchDocument(e, d, "manuscript", figures)
	if !strings.Contains(source, `\includegraphics[width=0.96\linewidth]{figures/figure-01.pdf}`) || !strings.Contains(source, `\caption{`) {
		t.Fatal("LaTeX figure not embedded")
	}
	if !strings.Contains(renderResearchDocument(e, d, "report", figures), "![Figure 1](figures/figure-01.png)") {
		t.Fatal("Markdown figure not embedded")
	}
	dir := t.TempDir()
	for name, raw := range assets {
		file := filepath.Join(dir, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(file), 0700)
		if err = os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "main.tex"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if preview := os.Getenv("KAIRO_FIGURE_PREVIEW_DIR"); preview != "" {
		_ = os.MkdirAll(preview, 0700)
		_ = os.WriteFile(filepath.Join(preview, "evidence-figure.png"), assets[figures[0].Name], 0600)
		_ = os.WriteFile(filepath.Join(preview, "evidence-figure.pdf"), assets[figures[0].PDF], 0600)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	replay := exec.CommandContext(ctx, "python3", "-I", "figures.py", "--export")
	replay.Dir = dir
	replay.Stdin = bytes.NewReader(assets["figure-data.json"])
	replay.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "MPLCONFIGDIR=" + filepath.Join(dir, "mpl-cache"), "OPENBLAS_NUM_THREADS=1", "OMP_NUM_THREADS=1"}
	output, replayErr := replay.CombinedOutput()
	cancel()
	if replayErr != nil || !strings.Contains(string(output), "Wrote 1 evidence figures") {
		t.Fatalf("plot source did not reproduce: %v %s", replayErr, output)
	}
	if engine, err := exec.LookPath("pdflatex"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, engine, "--no-shell-escape", "--interaction=nonstopmode", "--halt-on-error", "main.tex")
		cmd.Dir = dir
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "TMPDIR=" + dir}
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("manuscript with vector figure did not compile: %v\n%s", err, output)
		}
		if preview := os.Getenv("KAIRO_FIGURE_PREVIEW_DIR"); preview != "" {
			raw, err := os.ReadFile(filepath.Join(dir, "main.pdf"))
			if err != nil {
				t.Fatal(err)
			}
			_ = os.WriteFile(filepath.Join(preview, "manuscript-preview.pdf"), raw, 0600)
			_ = os.WriteFile(filepath.Join(preview, "main.tex"), []byte(source), 0600)
		}
	}
}
func TestDocumentFiguresServeExportAndMigrate(t *testing.T) {
	s, h := testServer(t)
	s.Registry.SetAvailableModules([]string{"matplotlib"})
	e := completedDocumentExperiment(t, s)
	w := request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/documents", map[string]any{"kind": "manuscript"})
	requireStatus(t, w, 201)
	var d ResearchDocument
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	if len(d.Figures) != 1 {
		t.Fatal("meaningful CSV figure was not generated")
	}
	base := "/api/v1/documents/" + d.ID
	w = request(t, h, "GET", base+"/figures/"+d.Figures[0].Name, nil)
	requireStatus(t, w, 200)
	if w.Header().Get("Content-Type") != "image/png" {
		t.Fatal("incorrect PNG response")
	}
	requireStatus(t, request(t, h, "GET", base+"/figures/figures.py", nil), 404)
	w = request(t, h, "GET", base+"/bundle", nil)
	requireStatus(t, w, 200)
	archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, entry := range archive.File {
		reader, _ := entry.Open()
		files[entry.Name], _ = io.ReadAll(reader)
		reader.Close()
	}
	for _, name := range []string{"main.tex", "figures/figure-01.png", "figures/figure-01.pdf", "figures.py", "figure-data.json", "requirements-figures.txt", "figures.json"} {
		if len(files[name]) == 0 {
			t.Fatal("figure source bundle missing", name)
		}
	}
	objects := &memoryObjects{}
	if _, err = MigrateArtifacts(context.Background(), s.Store, s.Config.DataDir, objects, true); err != nil {
		t.Fatal(err)
	}
	s.Objects = objects
	loaded, err := s.loadDocument(context.Background(), d.ID)
	if err != nil || len(loaded.Figures) != 1 {
		t.Fatal("figures lost in migration", err)
	}
	if _, err = os.Stat(filepath.Join(s.Config.DataDir, filepath.FromSlash(documentPrefix(d)+d.Figures[0].Name))); !os.IsNotExist(err) {
		t.Fatal("verified local figure retained after migration")
	}
	requireStatus(t, request(t, h, "GET", base+"/figures/"+d.Figures[0].Name, nil), 200)
	_ = objects.Put(context.Background(), documentPrefix(d)+d.Figures[0].Name, "image/png", []byte("corrupt"))
	requireStatus(t, request(t, h, "GET", base+"/figures/"+d.Figures[0].Name, nil), 500)
	requireStatus(t, request(t, h, "GET", base+"/bundle", nil), 500)
	w = request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/documents", map[string]any{"kind": "report", "include_figures": false})
	requireStatus(t, w, 201)
	d = ResearchDocument{}
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	if len(d.Figures) != 0 || strings.Contains(d.Content, "![Figure") {
		t.Fatal("text-only request included figures")
	}
}
func TestWritingAgentCanChooseEvidenceFigures(t *testing.T) {
	result := &Result{Series: []Series{{Name: "Values", Points: []Point{{X: 0, Y: 1}, {X: 1, Y: 2}}}}}
	if _, err := selectFigureSpecs(result, []string{}); err != nil {
		t.Fatal("optional figures rejected")
	}
	specs, err := selectFigureSpecs(result, []string{"recorded-series"})
	if err != nil || len(specs) != 1 {
		t.Fatal(err)
	}
	if len(figureCatalog(&Result{Findings: []string{"No quantitative data"}})) != 0 {
		t.Fatal("figures invented without numeric evidence")
	}
}

func TestWritingAgentSelectsOnlyAvailableFigureIDs(t *testing.T) {
	e := Experiment{Question: "How does model output change over time?", Title: "Recorded study", Plan: &Plan{Methodology: "Recorded simulation"}, Result: &Result{Series: []Series{{Name: "Control", Points: []Point{{X: 0, Y: 1}, {X: 1, Y: 2}}}}}}
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		prompt := body["prompt"].(string)
		if !strings.Contains(prompt, "available_figures") || !strings.Contains(prompt, "recorded-series") {
			t.Error("writing agent did not receive evidence figure choices")
		}
		id := "recorded-series"
		if calls > 0 {
			id = "invented"
		}
		calls++
		reply := documentSections{Title: "Recorded study", Abstract: "A simulation.", Introduction: "A research question.", Methods: "Recorded method.", Results: "Recorded values appear in Figure 1.", Discussion: "Simulation only.", Conclusion: "Review assumptions.", Figures: []string{id}}
		raw, _ := json.Marshal(reply)
		respond(w, 200, map[string]any{"response": string(raw), "done": true})
	}))
	defer upstream.Close()
	model := NewModelClient("ollama", upstream.URL, "open-weight-test", "")
	draft, _, err := model.DraftResearchDocument(context.Background(), e, "report")
	if err != nil || len(draft.Figures) != 1 {
		t.Fatal("valid figure selection failed", err)
	}
	if _, _, err = model.DraftResearchDocument(context.Background(), e, "report"); err == nil {
		t.Fatal("agent invented an unavailable figure")
	}
}
func TestLiveR2MatplotlibDocuments(t *testing.T) {
	if os.Getenv("KAIRO_TEST_R2") != "1" {
		t.Skip("opt-in live R2 document figures")
	}
	s, h := testServer(t)
	objects, err := NewObjectStorage(Config{StorageBackend: "r2", R2Endpoint: os.Getenv("R2_ENDPOINT"), R2Bucket: os.Getenv("R2_BUCKET"), R2AccessKey: os.Getenv("R2_ACCESS_KEY_ID"), R2SecretKey: os.Getenv("R2_SECRET_ACCESS_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	s.Objects = objects
	s.Config.WorkerMode = "docker"
	s.Config.WorkerImage = "researchos-worker:local"
	s.Registry.SetAvailableModules([]string{"matplotlib"})
	e := completedDocumentExperiment(t, s)
	t.Cleanup(func() { _ = objects.DeletePrefix(context.Background(), "experiments/"+e.ID+"/") })
	for _, kind := range []string{"report", "manuscript"} {
		w := request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/documents", map[string]string{"kind": kind})
		requireStatus(t, w, 201)
		var d ResearchDocument
		_ = json.Unmarshal(w.Body.Bytes(), &d)
		if len(d.Figures) != 1 {
			t.Fatal("no actual Matplotlib figure generated")
		}
		base := "/api/v1/documents/" + d.ID
		w = request(t, h, "GET", base+"/figures/"+d.Figures[0].Name, nil)
		requireStatus(t, w, 200)
		if _, err = png.DecodeConfig(bytes.NewReader(w.Body.Bytes())); err != nil {
			t.Fatal(err)
		}
		requireStatus(t, request(t, h, "GET", base+"/figures/"+d.Figures[0].PDF, nil), 200)
		w = request(t, h, "GET", base+"/bundle", nil)
		requireStatus(t, w, 200)
		archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]bool{}
		for _, entry := range archive.File {
			found[entry.Name] = true
		}
		for _, name := range []string{d.Figures[0].Name, d.Figures[0].PDF, "figure-data.json", "figures.py"} {
			if !found[name] {
				t.Fatal("cloud source bundle missing", name)
			}
		}
		if kind == "manuscript" && !strings.Contains(d.Content, `\includegraphics`) {
			t.Fatal("LaTeX did not include figure")
		}
		if kind == "report" && !strings.Contains(d.Content, "![Figure 1]") {
			t.Fatal("Markdown did not include figure")
		}
	}
	if _, err = os.Stat(filepath.Join(s.Config.DataDir, "experiments")); !os.IsNotExist(err) {
		t.Fatal("figure files were written to local artifact storage")
	}
	t.Log("PASS Docker Matplotlib → R2 PNG/PDF and plotting source → embedded report/LaTeX → authenticated image and ZIP downloads, with no local artifact files")
}

func TestLiveWritingAgentFigureChoice(t *testing.T) {
	if os.Getenv("KAIRO_TEST_LIVE_MODEL") != "1" {
		t.Skip("opt-in live writing-agent figure selection")
	}
	result := trustedGeneralFixture(t)
	plan := samplePlan(t, "custom")
	e := Experiment{Title: "Logistic growth sensitivity", Question: plan.ResearchQuestion, Plan: &plan, Result: &result}
	model := NewModelClient(os.Getenv("MODEL_PROVIDER"), os.Getenv("MODEL_URL"), os.Getenv("MODEL_NAME"), os.Getenv("MODEL_API_KEY"))
	draft, source, err := model.DraftResearchDocument(context.Background(), e, "manuscript", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = selectFigureSpecs(&result, draft.Figures); err != nil {
		t.Fatal(err)
	}
	if source != "model" {
		t.Fatal("live writing agent was not used")
	}
	t.Logf("PASS live writing agent returned a complete manuscript and selected %d valid evidence figure(s): %v", len(draft.Figures), draft.Figures)
}
