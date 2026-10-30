package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDatasetFiveGBLimitAndCSVValidation(t *testing.T) {
	if MaxDatasetBytes != 5_000_000_000 {
		t.Fatal("incorrect 5 GB limit")
	}
	header, preview, rows, err := validateCSV(strings.NewReader("\ufeffa,b\n1,2\n3,4\n"))
	if err != nil || header[0] != "a" || rows != 2 || len(preview) != 2 {
		t.Fatal("CSV validation lost data", err)
	}
	for _, raw := range []string{"a,a\n1,2\n", "a,b\n1\n", "a,b\n1," + strings.Repeat("x", 10001) + "\n", "a,b\n\"" + strings.Repeat("x", 1<<20) + "\",2\n"} {
		if _, _, _, err = validateCSV(strings.NewReader(raw)); err == nil {
			t.Fatal("invalid or unbounded CSV accepted")
		}
	}
	raw := "a,b\n" + strings.Repeat("1,2\n", 60000)
	_, preview, rows, err = validateCSV(strings.NewReader(raw))
	if err != nil || rows != 60000 || len(preview) != 5 {
		t.Fatal("old row cap still applied", err)
	}
	for _, raw := range []string{"http://example.com/data.csv", "https://127.0.0.1/data.csv", "https://169.254.169.254/latest/meta-data", "https://user:password@example.com/file.csv", "https://example.com:444/data.csv"} {
		if _, err := validDatasetURL(raw); err == nil {
			t.Fatal("unsafe remote dataset URL accepted", raw)
		}
	}
	for _, ip := range []string{"127.0.0.1", "::1", "10.0.0.1", "100.64.0.1", "169.254.1.1", "fc00::1"} {
		if publicIP(net.ParseIP(ip)) {
			t.Fatal("private dataset address accepted", ip)
		}
	}
	if _, err := validDatasetURL("https://example.com/dataset.csv"); err != nil {
		t.Fatal(err)
	}
}
func TestStreamingDatasetUploadAndLimitCleanup(t *testing.T) {
	s, _ := testServer(t)
	objects := &memoryObjects{}
	s.Objects = objects
	data := "x,y\n" + strings.Repeat("1,2\n", 60000)
	dataset, err := s.ingestDataset(context.Background(), "data.csv", strings.NewReader(data), "https://example.com/data.csv")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(data))
	if dataset.Rows != 60000 || dataset.Size != int64(len(data)) || dataset.SHA256 != hex.EncodeToString(sum[:]) || dataset.SourceURL == "" {
		t.Fatal("streaming metadata incorrect")
	}
	if _, err = os.Stat(filepath.Join(s.Config.DataDir, "datasets")); !os.IsNotExist(err) {
		t.Fatal("R2 mode dataset wrote local files")
	}
	count := len(objects.files)
	if _, err = s.ingestDatasetLimit(context.Background(), "too-large.csv", strings.NewReader(data), "", 100); !errors.Is(err, errDatasetTooLarge) {
		t.Fatal("over-limit upload accepted", err)
	}
	if len(objects.files) != count {
		t.Fatal("over-limit upload left a published object")
	}
	if _, err = s.ingestDataset(context.Background(), "bad.csv", strings.NewReader("a,a\n1,2\n"), ""); err == nil {
		t.Fatal("bad CSV accepted")
	}
	if len(objects.files) != count {
		t.Fatal("invalid upload left an object")
	}
}
func TestLargeDatasetRequiresStreamingPlan(t *testing.T) {
	s, _ := testServer(t)
	objects := &memoryObjects{}
	s.Jobs.Objects = objects
	d := Dataset{ID: newID("ds"), Name: "large.csv", Rows: 200000, Size: 30 << 20}
	if err := s.Store.SaveDataset(d); err != nil {
		t.Fatal(err)
	}
	p := samplePlan(t, "custom")
	e := Experiment{Plan: &p, DatasetID: d.ID}
	if _, err := s.Jobs.Input(e); err == nil || !strings.Contains(err.Error(), "dataset_mode=stream") {
		t.Fatal("large dataset loaded inline", err)
	}
	p.Parameters = json.RawMessage(`{"inputs":{},"seed":42,"required_modules":[],"dataset_mode":"stream"}`)
	input, err := s.Jobs.Input(e)
	if err != nil || !input.DatasetStream || input.WriteDatasetRows == nil {
		t.Fatal("streaming plan not prepared", err)
	}
}
func TestLiveR2MultipartDataset(t *testing.T) {
	if os.Getenv("KAIRO_TEST_R2") != "1" {
		t.Skip("opt-in live R2 multipart upload")
	}
	s, h := testServer(t)
	objects, err := NewObjectStorage(Config{StorageBackend: "r2", R2Endpoint: os.Getenv("R2_ENDPOINT"), R2Bucket: os.Getenv("R2_BUCKET"), R2AccessKey: os.Getenv("R2_ACCESS_KEY_ID"), R2SecretKey: os.Getenv("R2_SECRET_ACCESS_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	s.Objects = objects
	s.Jobs.Objects = objects
	line := "2," + strings.Repeat("a", 180) + "\n"
	const rows = 100001
	reader := io.MultiReader(strings.NewReader("value,label\n"), strings.NewReader(strings.Repeat(line, rows)))
	d, err := s.ingestDataset(context.Background(), "multipart-test.csv", reader, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = objects.Delete(context.Background(), "datasets/"+d.ID+".csv") })
	if d.Size <= multipartChunkSize || d.Rows != rows {
		t.Fatal("test did not cross multipart threshold")
	}
	body, _, err := objects.Get(context.Background(), "datasets/"+d.ID+".csv")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	_, err = io.Copy(hash, body)
	body.Close()
	if err != nil || hex.EncodeToString(hash.Sum(nil)) != d.SHA256 {
		t.Fatal("multipart hash mismatch")
	}
	p := samplePlan(t, "custom")
	p.TimeoutSeconds = 120
	p.Parameters = json.RawMessage(`{"inputs":{},"seed":42,"required_modules":[],"dataset_mode":"stream"}`)
	p.Code = "def run_experiment(inputs,dataset_rows):\n    count=0\n    total=0.0\n    for row in dataset_rows:\n        count+=1\n        total+=float(row['value'])\n    return {'kind':'observational_analysis','metrics':{'count':count,'sum':total},'findings':[f'Processed {count} rows'],'limitations':['Test fixture only']}\n"
	input, err := s.Jobs.Input(Experiment{Plan: &p, DatasetID: d.ID})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	result, err := (PythonExecutor{Mode: "docker", Image: "researchos-worker:local"}).Execute(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Metrics["count"] != float64(rows) || result.Metrics["sum"] != float64(rows*2) {
		t.Fatal("streaming computation skipped data")
	}
	w := request(t, h, "GET", "/api/v1/datasets/"+d.ID+"/download", nil)
	requireStatus(t, w, http.StatusOK)
	if w.Body.Len() != int(d.Size) {
		t.Fatal("dataset download truncated")
	}
	if _, err = os.Stat(filepath.Join(s.Config.DataDir, "datasets")); !os.IsNotExist(err) {
		t.Fatal("multipart upload wrote local data")
	}
	t.Logf("PASS actual R2 multipart upload: %d bytes, %d rows; checksum verified; all rows streamed into isolated Python, no local dataset files", d.Size, d.Rows)
}
func TestStreamDatasetIntegrity(t *testing.T) {
	s, _ := testServer(t)
	objects := &memoryObjects{}
	s.Jobs.Objects = objects
	raw := []byte("x,y\n1,2\n3,4\n")
	sum := sha256.Sum256(raw)
	d := Dataset{ID: "ds_test", Size: int64(len(raw)), Rows: 2, SHA256: hex.EncodeToString(sum[:])}
	_ = objects.Put(context.Background(), "datasets/ds_test.csv", "text/csv", raw)
	var output bytes.Buffer
	if err := s.Jobs.streamDatasetRows(context.Background(), d, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Count(output.String(), "\n") != 2 {
		t.Fatal("stream rows missing")
	}
	d.SHA256 = strings.Repeat("0", 64)
	if err := s.Jobs.streamDatasetRows(context.Background(), d, io.Discard); err == nil {
		t.Fatal("stream checksum mismatch ignored")
	}
}

func TestLivePublicDatasetDiscovery(t *testing.T) {
	if os.Getenv("KAIRO_TEST_PUBLIC_DATASETS") != "1" {
		t.Skip("opt-in live public dataset catalogs")
	}
	s := &Server{}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	result, err := s.discoverDatasets(ctx, "air quality")
	if err != nil {
		t.Fatal(err)
	}
	for _, warning := range result.Warnings {
		t.Log(warning)
	}
	if len(result.Public) == 0 {
		t.Fatal("no public CSV candidates returned")
	}
	for _, item := range result.Public {
		if _, err := validDatasetURL(item.URL); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("PASS actual public catalog search returned", len(result.Public), "CSV resources")
}

func TestAssistantHonorsWorkspaceDataset(t *testing.T) {
	s, _ := testServer(t)
	d := Dataset{ID: newID("ds"), Name: "growth.csv", Rows: 2, Columns: []string{"time", "value"}}
	if err := s.Store.SaveDataset(d); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		prompt := body["prompt"].(string)
		if !strings.Contains(prompt, "growth.csv") || !strings.Contains(prompt, d.ID) {
			t.Error("workspace dataset context missing")
		}
		reply := AssistantReply{Message: "We can use growth.csv to analyze the saved observations.", Draft: &ResearchDraft{Question: "How does the recorded growth change with time?", Language: "python", DatasetID: d.ID}}
		raw, _ := json.Marshal(reply)
		respond(w, 200, map[string]any{"response": string(raw), "done": true})
	}))
	defer upstream.Close()
	s.Model = NewModelClient("ollama", upstream.URL, "open-weight-test", "")
	h := s.Handler()
	w := request(t, h, "POST", "/api/v1/assistant/chat", map[string]any{"messages": []AssistantMessage{{Role: "user", Content: "Use the growth file in my datasets"}}, "dataset_id": d.ID})
	requireStatus(t, w, 200)
	var reply AssistantReply
	_ = json.Unmarshal(w.Body.Bytes(), &reply)
	if reply.Draft.DatasetID != d.ID {
		t.Fatal("dataset preference ignored")
	}
	requireStatus(t, request(t, h, "POST", "/api/v1/assistant/chat", map[string]any{"messages": []AssistantMessage{{Role: "user", Content: "Use this"}}, "dataset_id": "ds_unknown"}), 422)
}
func TestLivePublicCSVImportToR2(t *testing.T) {
	if os.Getenv("KAIRO_TEST_R2") != "1" || os.Getenv("KAIRO_TEST_PUBLIC_DATASETS") != "1" {
		t.Skip("opt-in public CSV import to real R2")
	}
	s, h := testServer(t)
	objects, err := NewObjectStorage(Config{StorageBackend: "r2", R2Endpoint: os.Getenv("R2_ENDPOINT"), R2Bucket: os.Getenv("R2_BUCKET"), R2AccessKey: os.Getenv("R2_ACCESS_KEY_ID"), R2SecretKey: os.Getenv("R2_SECRET_ACCESS_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	s.Objects = objects
	source := "https://raw.githubusercontent.com/mwaskom/seaborn-data/master/iris.csv"
	w := request(t, h, "POST", "/api/v1/datasets/import", map[string]string{"url": source, "name": "iris-import-validation.csv"})
	requireStatus(t, w, 201)
	var dataset Dataset
	_ = json.Unmarshal(w.Body.Bytes(), &dataset)
	t.Cleanup(func() { _ = objects.Delete(context.Background(), "datasets/"+dataset.ID+".csv") })
	if dataset.Rows != 150 || dataset.SourceURL != source {
		t.Fatal("public source import metadata incorrect")
	}
	raw, err := readObject(context.Background(), objects, "datasets/"+dataset.ID+".csv", 1<<20)
	if err != nil || !bytes.Contains(raw, []byte("sepal_length")) {
		t.Fatal("import did not store the source CSV in R2", err)
	}
	if _, err = os.Stat(filepath.Join(s.Config.DataDir, "datasets")); !os.IsNotExist(err) {
		t.Fatal("public import spooled CSV to disk")
	}
	t.Log("PASS actual HTTPS source → CSV validation → R2 multipart storage → workspace dataset metadata")
}
