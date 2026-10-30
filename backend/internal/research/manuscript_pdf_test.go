package research

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxManuscriptPDFCompilation(t *testing.T) {
	s := &Server{}
	e := Experiment{ID: "exp_pdf", Title: "PDF export", Plan: &Plan{Domain: "simulation", Methodology: "Recorded calculation", Assumptions: []string{"Illustrative inputs"}}, Result: &Result{Metrics: map[string]any{"mean": 2.5}, Findings: []string{"The computed mean is 2.5."}}}
	draft, _, err := NewModelClient("disabled", "", "", "").DraftResearchDocument(context.Background(), e, "manuscript")
	if err != nil {
		t.Fatal(err)
	}
	d := ResearchDocument{Kind: "manuscript", Content: renderResearchDocument(e, draft, "manuscript")}
	raw, err := s.compileManuscript(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte("%PDF-")) {
		t.Fatal("not a compiled PDF")
	}
	if dir := os.Getenv("KAIRO_PDF_PREVIEW_DIR"); dir != "" {
		_ = os.MkdirAll(dir, 0700)
		if err = os.WriteFile(filepath.Join(dir, "compiled-manuscript.pdf"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	d.Content = "\\documentclass{article}\n\\begin{document}\n\\input{/etc/passwd}\n\\end{document}"
	if _, err = s.compileManuscript(context.Background(), d); err == nil {
		t.Fatal("compiler read outside its workspace")
	}
}

func TestManuscriptPDFRoutesPersistCacheAndVerify(t *testing.T) {
	s, h := testServer(t)
	s.Objects = &memoryObjects{}
	e := completedDocumentExperiment(t, s)
	w := request(t, h, http.MethodPost, "/api/v1/experiments/"+e.ID+"/documents", map[string]any{"kind": "manuscript", "include_figures": false})
	requireStatus(t, w, 201)
	var d ResearchDocument
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	requireStatus(t, request(t, h, "GET", "/api/v1/documents/"+d.ID+"/pdf", nil), 404)
	w = request(t, h, "POST", "/api/v1/documents/"+d.ID+"/pdf/stream", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "event: done") || strings.Contains(w.Body.String(), "event: error") {
		t.Fatal("PDF stream failed", w.Body.String())
	}
	w = request(t, h, "GET", "/api/v1/documents/"+d.ID+"/pdf", nil)
	requireStatus(t, w, 200)
	if w.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF-")) {
		t.Fatal("PDF download invalid")
	}
	first := append([]byte{}, w.Body.Bytes()...)
	w = request(t, h, "POST", "/api/v1/documents/"+d.ID+"/pdf", nil)
	requireStatus(t, w, 200)
	w = request(t, h, "GET", "/api/v1/documents/"+d.ID+"/pdf", nil)
	if !bytes.Equal(first, w.Body.Bytes()) {
		t.Fatal("cached PDF changed")
	}
	loaded, err := s.loadDocument(context.Background(), d.ID)
	if err != nil {
		t.Fatal(err)
	}
	metadata, _, err := s.cachedManuscriptPDF(context.Background(), loaded)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.objectStorage().Put(context.Background(), documentPrefix(d)+metadata.Name, "application/pdf", []byte("tampered")); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, request(t, h, "GET", "/api/v1/documents/"+d.ID+"/pdf", nil), 502)
	// Regeneration repairs a corrupted cached file; no stale or unverified bytes are returned.
	requireStatus(t, request(t, h, "POST", "/api/v1/documents/"+d.ID+"/pdf", nil), 201)
	requireStatus(t, request(t, h, "GET", "/api/v1/documents/"+d.ID+"/pdf", nil), 200)
}
