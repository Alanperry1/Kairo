package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

//go:embed manuscript_pdf.py
var manuscriptPDFCode string
var pdfCompileSlot = make(chan struct{}, 1)
var compiledPDFPath = regexp.MustCompile(`^compiled/[a-f0-9]{64}\.pdf$`)

type manuscriptPDF struct {
	Name         string    `json:"name"`
	SHA256       string    `json:"sha256"`
	SourceSHA256 string    `json:"source_sha256"`
	Size         int       `json:"size"`
	CompiledAt   time.Time `json:"compiled_at"`
	Compiler     string    `json:"compiler"`
}

func manuscriptSourceHash(d ResearchDocument) string {
	raw, _ := json.Marshal(struct {
		Content string
		Figures []DocumentAsset
	}{d.Content, d.AssetFiles})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func (s *Server) cachedManuscriptPDF(ctx context.Context, d ResearchDocument) (manuscriptPDF, []byte, error) {
	var metadata manuscriptPDF
	raw, err := readObject(ctx, s.objectStorage(), documentPrefix(d)+"pdf.json", 4096)
	if err != nil {
		return metadata, nil, err
	}
	if strictJSON(raw, &metadata) != nil || !compiledPDFPath.MatchString(metadata.Name) || metadata.SourceSHA256 != manuscriptSourceHash(d) || metadata.Size < 5 || metadata.Size > 16<<20 {
		return metadata, nil, errors.New("PDF metadata does not match the manuscript")
	}
	raw, err = readObject(ctx, s.objectStorage(), documentPrefix(d)+metadata.Name, 16<<20)
	if err != nil {
		return metadata, nil, err
	}
	hash := sha256.Sum256(raw)
	if !bytes.HasPrefix(raw, []byte("%PDF-")) || len(raw) != metadata.Size || hex.EncodeToString(hash[:]) != metadata.SHA256 {
		return metadata, nil, errors.New("PDF integrity check failed")
	}
	return metadata, raw, nil
}
func (s *Server) compileManuscript(ctx context.Context, d ResearchDocument) ([]byte, error) {
	for _, binary := range []string{"bwrap", "/usr/bin/pdflatex", "/usr/bin/python3", "/usr/bin/prlimit"} {
		if _, err := exec.LookPath(binary); err != nil {
			return nil, errors.New("PDF compilation requires Bubblewrap, pdfLaTeX, Python 3, and prlimit on the API host")
		}
	}
	figures := map[string]string{}
	for _, asset := range d.AssetFiles {
		if !strings.HasSuffix(asset.Name, ".pdf") {
			continue
		}
		raw, err := s.readDocumentAsset(ctx, d, asset)
		if err != nil {
			return nil, err
		}
		figures[asset.Name] = base64.StdEncoding.EncodeToString(raw)
	}
	payload, _ := json.Marshal(map[string]any{"source": d.Content, "figures": figures})
	args := []string{"--unshare-all", "--die-with-parent", "--new-session", "--cap-drop", "ALL", "--clearenv", "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "HOME", "/work", "--ro-bind", "/usr", "/usr"}
	for _, dir := range []string{"/lib", "/lib64", "/etc/texmf", "/var/lib/texmf", "/etc/fonts", "/var/cache/fontconfig"} {
		if _, err := os.Stat(dir); err == nil {
			args = append(args, "--ro-bind", dir, dir)
		}
	}
	args = append(args, "--proc", "/proc", "--dev", "/dev", "--size", "16777216", "--tmpfs", "/tmp", "--size", "134217728", "--tmpfs", "/work", "--chdir", "/work", "/usr/bin/prlimit", "--as=1073741824", "--cpu=60", "--fsize=16777216", "--nproc=64", "--", "/usr/bin/python3", "-I", "-c", manuscriptPDFCode)
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bwrap", args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	cmd.Stdin = bytes.NewReader(payload)
	cmd.WaitDelay = 2 * time.Second
	stdout, stderr := &limitedBuffer{limit: 24 << 20}, &limitedBuffer{limit: 8 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("PDF compilation failed. Check the LaTeX source, packages, and host sandbox support. The saved source is unchanged.")
	}
	var result struct {
		PDF string `json:"pdf"`
	}
	if json.Unmarshal(stdout.Bytes(), &result) != nil {
		return nil, errors.New("PDF compiler returned an invalid response")
	}
	raw, err := base64.StdEncoding.DecodeString(result.PDF)
	if err != nil || !bytes.HasPrefix(raw, []byte("%PDF-")) || len(raw) > 16<<20 {
		return nil, errors.New("PDF compiler returned invalid output")
	}
	return raw, nil
}
func (s *Server) generateManuscriptPDF(w http.ResponseWriter, r *http.Request) {
	d, err := s.loadDocument(r.Context(), r.PathValue("documentID"))
	if err != nil {
		handleError(w, err)
		return
	}
	if d.Kind != "manuscript" {
		apiError(w, 422, "manuscript_required", "PDF compilation is available for LaTeX manuscripts.")
		return
	}
	taskStatus(r.Context(), "Waiting for the PDF compiler")
	select {
	case pdfCompileSlot <- struct{}{}:
		defer func() { <-pdfCompileSlot }()
	case <-r.Context().Done():
		return
	}
	if metadata, _, err := s.cachedManuscriptPDF(r.Context(), d); err == nil {
		respond(w, 200, metadata)
		return
	}
	taskStatus(r.Context(), "Compiling the saved LaTeX and figures into PDF")
	raw, err := s.compileManuscript(r.Context(), d)
	if err != nil {
		apiError(w, 502, "pdf_compilation_failed", err.Error())
		return
	}
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	metadata := manuscriptPDF{Name: "compiled/" + digest + ".pdf", SHA256: digest, SourceSHA256: manuscriptSourceHash(d), Size: len(raw), CompiledAt: time.Now().UTC(), Compiler: "pdfLaTeX (two passes, no shell escape)"}
	taskStatus(r.Context(), "Saving the compiled PDF to object storage")
	if err = s.objectStorage().Put(r.Context(), documentPrefix(d)+metadata.Name, "application/pdf", raw); err == nil {
		manifest, _ := json.Marshal(metadata)
		err = s.objectStorage().Put(r.Context(), documentPrefix(d)+"pdf.json", "application/json", manifest)
	}
	if err != nil {
		handleError(w, err)
		return
	}
	respond(w, 201, metadata)
}
func (s *Server) downloadManuscriptPDF(w http.ResponseWriter, r *http.Request) {
	d, err := s.loadDocument(r.Context(), r.PathValue("documentID"))
	if err != nil {
		handleError(w, err)
		return
	}
	if d.Kind != "manuscript" {
		apiError(w, 422, "manuscript_required", "Choose a LaTeX manuscript.")
		return
	}
	_, raw, err := s.cachedManuscriptPDF(r.Context(), d)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiError(w, 404, "pdf_not_generated", "Generate the manuscript PDF first.")
		} else {
			apiError(w, 502, "pdf_unavailable", "The saved PDF could not be verified. Generate it again.")
		}
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": d.ID + ".pdf"}))
	w.Header().Set("Content-Length", fmt.Sprint(len(raw)))
	_, _ = w.Write(raw)
}
