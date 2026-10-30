package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed figures.py
var figureRendererCode string

type PlotSpec struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Title   string    `json:"title"`
	XLabel  string    `json:"x_label"`
	YLabel  string    `json:"y_label"`
	Caption string    `json:"caption"`
	Source  string    `json:"source"`
	Series  []Series  `json:"series,omitempty"`
	Labels  []string  `json:"labels,omitempty"`
	Values  []float64 `json:"values,omitempty"`
}
type DocumentFigure struct {
	Name     string `json:"name"`
	PDF      string `json:"pdf"`
	Caption  string `json:"caption"`
	Source   string `json:"source"`
	Kind     string `json:"kind"`
	Renderer string `json:"renderer"`
}
type DocumentAsset struct {
	Name        string `json:"name"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}
type documentFigureManifest struct {
	Figures []DocumentFigure `json:"figures"`
	Assets  []DocumentAsset  `json:"assets"`
}

func finiteNumber(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, finite(n)
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		v, err := n.Float64()
		return v, err == nil && finite(v)
	}
	return 0, false
}
func figureCatalog(result *Result) []PlotSpec {
	if result == nil {
		return nil
	}
	out := []PlotSpec{}
	if len(result.Series) > 0 && len(result.Series) <= 8 {
		lines := []Series{}
		valid := true
		points := 0
		for _, line := range result.Series {
			if len(line.Points) == 0 || len(line.Points) > 2000 || len(line.Name) > 200 {
				valid = false
				break
			}
			for _, p := range line.Points {
				if !finite(p.X) || !finite(p.Y) {
					valid = false
				}
			}
			points += len(line.Points)
			lines = append(lines, line)
		}
		if valid && points >= 2 {
			x, y := result.XAxisLabel, result.YAxisLabel
			if x == "" {
				x = "x"
			}
			if y == "" {
				y = "y"
			}
			out = append(out, PlotSpec{ID: "recorded-series", Kind: "line", Title: "Recorded experiment results", XLabel: x, YLabel: y, Caption: fmt.Sprintf("Recorded experiment output (%d series, %d points). Values are plotted in their recorded order; no fitted trend or uncertainty interval has been added.", len(lines), points), Source: "results.json: series", Series: lines})
		}
	}
	keys := []string{}
	for key := range result.Metrics {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	groups := []string{}
	for _, key := range keys {
		if _, ok := result.Metrics[key].(map[string]any); ok {
			groups = append(groups, key)
		}
	}
	if len(groups) >= 2 && len(groups) <= 20 {
		first := result.Metrics[groups[0]].(map[string]any)
		metrics := []string{}
		for key := range first {
			if _, ok := finiteNumber(first[key]); ok {
				metrics = append(metrics, key)
			}
		}
		sort.Strings(metrics)
		for _, metric := range metrics {
			if len(out) >= 4 {
				break
			}
			values := []float64{}
			for _, group := range groups {
				v, ok := finiteNumber(result.Metrics[group].(map[string]any)[metric])
				if !ok {
					values = nil
					break
				}
				values = append(values, v)
			}
			if len(values) != len(groups) {
				continue
			}
			out = append(out, PlotSpec{ID: fmt.Sprintf("comparison-%d", len(out)+1), Kind: "bar", Title: metric, XLabel: "Recorded condition", YLabel: metric, Labels: groups, Values: values, Caption: "Recorded " + metric + " for each condition. Bars show the saved values; no uncertainty intervals or significance tests have been added.", Source: "results.json: metrics/*/" + metric})
		}
	}
	if result.CSV != "" && len(result.CSV) <= 8<<20 {
		reader := csv.NewReader(strings.NewReader(result.CSV))
		header, err := reader.Read()
		rows := [][]string{}
		valid := err == nil && len(header) >= 2 && len(header) <= 100
		if valid {
			for {
				row, err := reader.Read()
				if err == io.EOF {
					break
				}
				if err != nil || len(rows) >= 5000 {
					valid = false
					break
				}
				rows = append(rows, row)
			}
		}
		if valid && len(rows) >= 2 {
			columns := map[int][]float64{}
			numeric := []int{}
			for i, name := range header {
				if len(name) > 150 {
					continue
				}
				values := []float64{}
				for _, row := range rows {
					v, err := strconv.ParseFloat(strings.TrimSpace(row[i]), 64)
					if err != nil || !finite(v) {
						values = nil
						break
					}
					values = append(values, v)
				}
				if len(values) == len(rows) {
					numeric = append(numeric, i)
					columns[i] = values
				}
			}
			if len(numeric) >= 2 {
				x := numeric[0]
				for _, y := range numeric[1:] {
					if len(out) >= 6 {
						break
					}
					points := []Point{}
					ordered := true
					for i, v := range columns[y] {
						points = append(points, Point{X: columns[x][i], Y: v})
						if i > 0 && columns[x][i] < columns[x][i-1] {
							ordered = false
						}
					}
					kind := "scatter"
					if ordered {
						kind = "line"
					}
					out = append(out, PlotSpec{ID: fmt.Sprintf("csv-%d-%d", x, y), Kind: kind, Title: header[y] + " by " + header[x], XLabel: header[x], YLabel: header[y], Series: []Series{{Name: header[y], Points: points}}, Caption: fmt.Sprintf("%s against %s using all %d recorded CSV rows. No observations were dropped and no fitted model has been added.", header[y], header[x], len(rows)), Source: "data.csv: " + header[x] + ", " + header[y]})
				}
			}
			if len(numeric) > 0 && len(rows) >= 10 && len(out) < 7 {
				i := numeric[0]
				out = append(out, PlotSpec{ID: "csv-distribution", Kind: "histogram", Title: "Distribution of " + header[i], XLabel: header[i], YLabel: "Recorded observations", Values: columns[i], Caption: fmt.Sprintf("Distribution of all %d recorded values in CSV column %s. All rows and groups are included; no fitted distribution has been added.", len(rows), header[i]), Source: "data.csv: " + header[i]})
			}
		}
	}
	return out
}
func figureOptions(result *Result) []map[string]any {
	out := []map[string]any{}
	for _, p := range figureCatalog(result) {
		out = append(out, map[string]any{"id": p.ID, "kind": p.Kind, "title": p.Title, "caption": p.Caption, "source": p.Source, "x_label": p.XLabel, "y_label": p.YLabel})
	}
	return out
}
func selectFigureSpecs(result *Result, ids []string) ([]PlotSpec, error) {
	if len(ids) > 3 {
		return nil, errors.New("at most three evidence figures are allowed")
	}
	catalog := map[string]PlotSpec{}
	for _, p := range figureCatalog(result) {
		catalog[p.ID] = p
	}
	selected := []PlotSpec{}
	seen := map[string]bool{}
	for _, id := range ids {
		p, ok := catalog[id]
		if !ok || seen[id] {
			return nil, errors.New("writing agent selected an unavailable or duplicate figure")
		}
		seen[id] = true
		selected = append(selected, p)
	}
	return selected, nil
}
func (s *Server) renderDocumentFigures(ctx context.Context, plots []PlotSpec) ([]DocumentFigure, map[string][]byte, error) {
	if len(plots) == 0 {
		return nil, nil, nil
	}
	if len(plots) > 3 {
		return nil, nil, errors.New("too many figures")
	}
	payload, _ := json.Marshal(map[string]any{"figures": plots})
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if s.Config.WorkerMode == "docker" {
		name := newID("kairo-figures")
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "--pull=never", "-i", "--name", name, "--network", "none", "--read-only", "--memory", "768m", "--cpus", "1", "--pids-limit", "64", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", "65534:65534", "--tmpfs", "/tmp:size=64m,noexec,nosuid,nodev", "-e", "MPLCONFIGDIR=/tmp/matplotlib", "-e", "HOME=/tmp", "-e", "OPENBLAS_NUM_THREADS=1", "-e", "OMP_NUM_THREADS=1", s.Config.WorkerImage, "python3", "-I", "-c", figureRendererCode)
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = exec.CommandContext(cleanup, "docker", "rm", "-f", name).Run()
		}()
	} else {
		python := s.Config.Python
		if python == "" {
			python = "python3"
		}
		cache := filepath.Join(s.Config.DataDir, ".build-cache", "matplotlib")
		if err := os.MkdirAll(cache, 0700); err != nil {
			return nil, nil, err
		}
		cmd = exec.CommandContext(ctx, python, "-I", "-c", figureRendererCode)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + cache, "MPLCONFIGDIR=" + cache, "OPENBLAS_NUM_THREADS=1", "OMP_NUM_THREADS=1", "PYTHONDONTWRITEBYTECODE=1"}
	}
	stdout, stderr := &limitedBuffer{limit: 32 << 20}, &limitedBuffer{limit: 16 << 10}
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, errors.New("Matplotlib figure rendering failed; check the worker image and installed libraries")
	}
	var output struct {
		Version string `json:"version"`
		Python  string `json:"python"`
		Figures []struct {
			ID  string `json:"id"`
			PNG string `json:"png"`
			PDF string `json:"pdf"`
		} `json:"figures"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil || len(output.Figures) != len(plots) {
		return nil, nil, errors.New("invalid figure renderer output")
	}
	figures := []DocumentFigure{}
	assets := map[string][]byte{"figures.py": []byte(figureRendererCode), "figure-data.json": payload, "requirements-figures.txt": []byte("# Python " + output.Python + "\nmatplotlib==" + output.Version + "\n")}
	for i, item := range output.Figures {
		raw, err := base64.StdEncoding.DecodeString(item.PNG)
		if err != nil || len(raw) > 4<<20 {
			return nil, nil, errors.New("invalid figure image")
		}
		info, err := png.DecodeConfig(bytes.NewReader(raw))
		if err != nil || info.Width > 2000 || info.Height > 2000 {
			return nil, nil, errors.New("invalid figure dimensions")
		}
		pdf, err := base64.StdEncoding.DecodeString(item.PDF)
		if err != nil || len(pdf) > 4<<20 || !bytes.HasPrefix(pdf, []byte("%PDF-")) || item.ID != plots[i].ID {
			return nil, nil, errors.New("invalid figure PDF or source")
		}
		figure := DocumentFigure{Name: fmt.Sprintf("figures/figure-%02d.png", i+1), PDF: fmt.Sprintf("figures/figure-%02d.pdf", i+1), Caption: plots[i].Caption, Source: plots[i].Source, Kind: plots[i].Kind, Renderer: "Matplotlib " + output.Version}
		assets[figure.Name] = raw
		assets[figure.PDF] = pdf
		figures = append(figures, figure)
	}
	return figures, assets, nil
}

var figurePath = regexp.MustCompile(`^figures/figure-0[1-3]\.(png|pdf)$`)

func validDocumentAsset(name string) bool {
	return figurePath.MatchString(name) || name == "figures.py" || name == "figure-data.json" || name == "requirements-figures.txt"
}
func figureAssetManifest(figures []DocumentFigure, assets map[string][]byte) documentFigureManifest {
	manifest := documentFigureManifest{Figures: figures}
	for name, raw := range assets {
		sum := sha256.Sum256(raw)
		kind := "text/plain; charset=utf-8"
		if strings.HasSuffix(name, ".png") {
			kind = "image/png"
		} else if strings.HasSuffix(name, ".pdf") {
			kind = "application/pdf"
		} else if strings.HasSuffix(name, ".json") {
			kind = "application/json"
		}
		manifest.Assets = append(manifest.Assets, DocumentAsset{Name: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(raw)), ContentType: kind})
	}
	sort.Slice(manifest.Assets, func(i, j int) bool { return manifest.Assets[i].Name < manifest.Assets[j].Name })
	return manifest
}

func (s *Server) hasMatplotlib() bool {
	if s.Registry == nil {
		return false
	}
	tool, ok := s.Registry.Lookup("python.experiment")
	if !ok {
		return false
	}
	for _, name := range tool.AvailableModules {
		if name == "matplotlib" {
			return true
		}
	}
	return false
}
func (s *Server) loadFigureManifest(ctx context.Context, d ResearchDocument) (documentFigureManifest, error) {
	var manifest documentFigureManifest
	raw, err := readObject(ctx, s.objectStorage(), documentPrefix(d)+"figures.json", 64<<10)
	if errors.Is(err, ErrNotFound) {
		return manifest, nil
	}
	if err != nil {
		return manifest, err
	}
	if strictJSON(raw, &manifest) != nil || len(manifest.Figures) > 3 || len(manifest.Assets) > 9 {
		return manifest, errors.New("invalid figure manifest")
	}
	names := map[string]bool{}
	for _, asset := range manifest.Assets {
		if !validDocumentAsset(asset.Name) || asset.Size < 0 || asset.Size > 8<<20 || len(asset.SHA256) != 64 || names[asset.Name] {
			return manifest, errors.New("invalid figure asset")
		}
		names[asset.Name] = true
	}
	for _, figure := range manifest.Figures {
		if !figurePath.MatchString(figure.Name) || !strings.HasSuffix(figure.Name, ".png") || !figurePath.MatchString(figure.PDF) || !strings.HasSuffix(figure.PDF, ".pdf") || !names[figure.Name] || !names[figure.PDF] {
			return manifest, errors.New("invalid figure paths")
		}
	}
	return manifest, nil
}
func (s *Server) readDocumentAsset(ctx context.Context, d ResearchDocument, asset DocumentAsset) ([]byte, error) {
	if !validDocumentAsset(asset.Name) {
		return nil, errors.New("invalid document asset")
	}
	raw, err := readObject(ctx, s.objectStorage(), documentPrefix(d)+asset.Name, 8<<20)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != asset.Size || hex.EncodeToString(sum[:]) != asset.SHA256 {
		return nil, errors.New("figure asset integrity check failed")
	}
	return raw, nil
}
