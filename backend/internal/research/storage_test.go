package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryObjects struct {
	mu         sync.Mutex
	files      map[string][]byte
	failPut    bool
	failDelete bool
}

func (m *memoryObjects) Put(ctx context.Context, key, kind string, raw []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failPut {
		return errors.New("simulated upload failure")
	}
	if m.files == nil {
		m.files = map[string][]byte{}
	}
	m.files[key] = append([]byte{}, raw...)
	return nil
}
func (m *memoryObjects) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[key]
	if !ok {
		return nil, 0, ErrNotFound
	}
	raw := append([]byte{}, b...)
	return io.NopCloser(bytes.NewReader(raw)), int64(len(raw)), nil
}
func (m *memoryObjects) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, key)
	return nil
}
func (m *memoryObjects) DeletePrefix(ctx context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failDelete {
		return errors.New("simulated deletion failure")
	}
	for key := range m.files {
		if strings.HasPrefix(key, prefix) {
			delete(m.files, key)
		}
	}
	return nil
}

func TestR2S3SignedRoundTrip(t *testing.T) {
	objects := &memoryObjects{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=test-access/") {
			t.Error("request is not signed with the configured R2 credentials")
		}
		if r.URL.Path == "/kairo" || r.URL.Path == "/kairo/" {
			var response struct {
				XMLName   xml.Name `xml:"ListBucketResult"`
				Truncated bool     `xml:"IsTruncated"`
				Contents  []struct {
					Key string `xml:"Key"`
				} `xml:"Contents"`
			}
			objects.mu.Lock()
			for key := range objects.files {
				if strings.HasPrefix(key, r.URL.Query().Get("prefix")) {
					response.Contents = append(response.Contents, struct {
						Key string `xml:"Key"`
					}{key})
				}
			}
			objects.mu.Unlock()
			w.Header().Set("Content-Type", "application/xml")
			_ = xml.NewEncoder(w).Encode(response)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/kairo/")
		switch r.Method {
		case "PUT":
			raw, _ := io.ReadAll(r.Body)
			_ = objects.Put(r.Context(), key, r.Header.Get("Content-Type"), raw)
			w.Header().Set("ETag", `"test-etag"`)
			w.WriteHeader(200)
		case "GET":
			body, size, err := objects.Get(r.Context(), key)
			if err != nil {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`<Error><Code>NoSuchKey</Code><Message>not found</Message></Error>`))
				return
			}
			defer body.Close()
			w.Header().Set("Content-Length", fmtInt(size))
			_, _ = io.Copy(w, body)
		case "DELETE":
			_ = objects.Delete(r.Context(), key)
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected S3 operation %s", r.Method)
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	r2 := newR2Storage(server.URL, "kairo", "test-access", "test-secret")
	ctx := context.Background()
	if err := CheckObjectStorage(ctx, r2); err != nil {
		t.Fatal(err)
	}
	if err := r2.Put(ctx, "experiments/exp_test/attempt-001/report.md", "text/markdown", []byte("computed evidence")); err != nil {
		t.Fatal(err)
	}
	raw, err := readObject(ctx, r2, "experiments/exp_test/attempt-001/report.md", 100)
	if err != nil || string(raw) != "computed evidence" {
		t.Fatal("S3 round-trip failed", err)
	}
	_ = r2.Put(ctx, "datasets/ds_keep.csv", "text/csv", []byte("x\n1\n"))
	if err = r2.DeletePrefix(ctx, "experiments/exp_test/"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = r2.Get(ctx, "experiments/exp_test/attempt-001/report.md"); !errors.Is(err, ErrNotFound) {
		t.Fatal("prefix deletion failed")
	}
	if _, err = readObject(ctx, r2, "datasets/ds_keep.csv", 100); err != nil {
		t.Fatal("deletion escaped its prefix")
	}
}
func fmtInt(n int64) string { b, _ := json.Marshal(n); return string(b) }
func TestStorageConfigurationAndPaths(t *testing.T) {
	if _, err := NewObjectStorage(Config{StorageBackend: "r2", R2Endpoint: "https://account.r2.cloudflarestorage.com", R2Bucket: "kairo"}); err == nil {
		t.Fatal("missing credentials silently fell back to disk")
	}
	if err := validateR2Config(Config{R2Endpoint: "https://account.r2.cloudflarestorage.com/kairo", R2Bucket: "kairo", R2AccessKey: "private-key", R2SecretKey: "private-secret"}); err == nil || strings.Contains(err.Error(), "private-") {
		t.Fatal("invalid config accepted or credentials exposed")
	}
	local := LocalStorage{Root: t.TempDir()}
	for _, key := range []string{"../secret", "/absolute", "datasets/../secret", "a\\b", "."} {
		if err := local.Put(context.Background(), key, "", []byte("x")); err == nil {
			t.Fatal("unsafe key accepted", key)
		}
	}
	if err := local.DeletePrefix(context.Background(), ""); err == nil {
		t.Fatal("root deletion accepted")
	}
}
func TestRemoteArtifactAndDocumentLifecycle(t *testing.T) {
	s, h := testServer(t)
	objects := &memoryObjects{}
	s.Objects = objects
	s.Jobs.Objects = objects
	e := completedDocumentExperiment(t, s)
	if err := s.Jobs.writeArtifacts(e, WorkerInput{Plan: e.Plan}, *e.Result); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Config.DataDir, "experiments", e.ID)); !os.IsNotExist(err) {
		t.Fatal("remote mode wrote local artifact files")
	}
	requireStatus(t, request(t, h, "GET", "/api/v1/experiments/"+e.ID+"/artifacts/attempt-001/results.json", nil), 200)
	requireStatus(t, request(t, h, "GET", "/api/v1/experiments/"+e.ID+"/bundle", nil), 200)
	w := request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/documents", map[string]string{"kind": "manuscript"})
	requireStatus(t, w, 201)
	var d ResearchDocument
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	metadata, err := s.Store.ResearchDocument(d.ID)
	if err != nil || metadata.Content != "" || len(metadata.Evidence) != 0 {
		t.Fatal("document bodies remained in metadata storage")
	}
	requireStatus(t, request(t, h, "GET", "/api/v1/documents/"+d.ID+"/download", nil), 200)
	requireStatus(t, request(t, h, "GET", "/api/v1/documents/"+d.ID+"/bundle", nil), 200)
	objects.failDelete = true
	requireStatus(t, request(t, h, "DELETE", "/api/v1/experiments/"+e.ID, nil), 500)
	if _, err = s.Store.Get(e.ID); err != nil {
		t.Fatal("failed cloud cleanup lost experiment metadata")
	}
	objects.failDelete = false
	requireStatus(t, request(t, h, "DELETE", "/api/v1/experiments/"+e.ID, nil), 204)
	if len(objects.files) != 0 {
		t.Fatal("experiment objects survived deletion")
	}
}
func TestStorageMigrationVerifiesBeforeDeleting(t *testing.T) {
	s, _ := testServer(t)
	e := completedDocumentExperiment(t, s)
	if err := s.Jobs.writeArtifacts(e, WorkerInput{Plan: e.Plan}, *e.Result); err != nil {
		t.Fatal(err)
	}
	target := &memoryObjects{}
	report, err := MigrateArtifacts(context.Background(), s.Store, s.Config.DataDir, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Files < 5 || report.Removed != report.Files {
		t.Fatalf("incomplete migration: %+v", report)
	}
	artifacts, _ := s.Store.Artifacts(e.ID)
	for _, a := range artifacts {
		if _, err := os.Stat(filepath.Join(s.Config.DataDir, "experiments", e.ID, a.Name)); !os.IsNotExist(err) {
			t.Fatal("verified local artifact was retained")
		}
	}
	if _, err = MigrateArtifacts(context.Background(), s.Store, s.Config.DataDir, target, true); err != nil {
		t.Fatal("migration was not resumable", err)
	}
}
func TestMigrationFailureKeepsLocalFiles(t *testing.T) {
	s, _ := testServer(t)
	e := completedDocumentExperiment(t, s)
	if err := s.Jobs.writeArtifacts(e, WorkerInput{Plan: e.Plan}, *e.Result); err != nil {
		t.Fatal(err)
	}
	target := &memoryObjects{failPut: true}
	if _, err := MigrateArtifacts(context.Background(), s.Store, s.Config.DataDir, target, true); err == nil {
		t.Fatal("migration ignored upload failure")
	}
	if _, err := os.Stat(filepath.Join(s.Config.DataDir, "experiments", e.ID, "attempt-001/results.json")); err != nil {
		t.Fatal("failed migration deleted local evidence")
	}
}

func TestLegacyDocumentsMigrateOutOfDatabase(t *testing.T) {
	s, _ := testServer(t)
	e := completedDocumentExperiment(t, s)
	evidence, _ := json.Marshal(e)
	sum := sha256.Sum256(evidence)
	d := ResearchDocument{ID: newID("doc"), ExperimentID: e.ID, Kind: "manuscript", Title: "Legacy draft", CreatedAt: e.CreatedAt, Attempt: e.Attempt, PlanVersion: e.PlanVersion, Source: "deterministic", EvidenceSHA256: hex.EncodeToString(sum[:]), Content: `\documentclass{article}`, Evidence: evidence}
	if err := s.Store.SaveResearchDocument(d); err != nil {
		t.Fatal(err)
	}
	objects := &memoryObjects{}
	report, err := MigrateArtifacts(context.Background(), s.Store, s.Config.DataDir, objects, true)
	if err != nil {
		t.Fatal(err)
	}
	metadata, _ := s.Store.ResearchDocument(d.ID)
	if metadata.Content != "" || len(metadata.Evidence) != 0 || report.Documents != 1 {
		t.Fatal("legacy content was not moved")
	}
	remote := &Server{Store: s.Store, Objects: objects}
	got, err := remote.loadDocument(context.Background(), d.ID)
	if err != nil || got.Content != d.Content || !bytes.Equal(got.Evidence, evidence) {
		t.Fatal("remote document lost source or evidence", err)
	}
}
func TestRemoteUploadFailureDoesNotWriteArtifactsToDisk(t *testing.T) {
	s, _ := testServer(t)
	s.Jobs.Objects = &memoryObjects{failPut: true}
	e := completedDocumentExperiment(t, s)
	if err := s.Jobs.writeArtifacts(e, WorkerInput{Plan: e.Plan}, *e.Result); err == nil {
		t.Fatal("upload failure ignored")
	}
	if _, err := os.Stat(filepath.Join(s.Config.DataDir, "experiments", e.ID)); !os.IsNotExist(err) {
		t.Fatal("failed remote upload fell back to disk")
	}
	artifacts, _ := s.Store.Artifacts(e.ID)
	if len(artifacts) != 0 {
		t.Fatal("failed upload published artifact metadata")
	}
}

func TestRemoteDatasetUploadAndWorkerInput(t *testing.T) {
	s, h := testServer(t)
	objects := &memoryObjects{}
	s.Objects = objects
	s.Jobs.Objects = objects
	w := upload(t, h, "measurements.csv", "group,value\nA,12\nB,19\n")
	requireStatus(t, w, 201)
	var dataset Dataset
	_ = json.Unmarshal(w.Body.Bytes(), &dataset)
	if _, err := os.Stat(filepath.Join(s.Config.DataDir, "datasets", dataset.ID+".csv")); !os.IsNotExist(err) {
		t.Fatal("remote dataset wrote to disk")
	}
	requireStatus(t, request(t, h, "GET", "/api/v1/datasets/"+dataset.ID+"/download", nil), 200)
	p := samplePlan(t, "custom")
	input, err := s.Jobs.Input(Experiment{Plan: &p, DatasetID: dataset.ID})
	if err != nil || len(input.DatasetRows) != 2 || input.DatasetRows[0]["value"] != "12" {
		t.Fatal("worker did not read selected rows from object storage", err)
	}
}

func TestLiveR2ResearchWorkflow(t *testing.T) {
	if os.Getenv("KAIRO_TEST_R2") != "1" {
		t.Skip("opt-in live R2 workflow test")
	}
	s, h := testServer(t)
	cfg := Config{StorageBackend: "r2", R2Endpoint: os.Getenv("R2_ENDPOINT"), R2Bucket: os.Getenv("R2_BUCKET"), R2AccessKey: os.Getenv("R2_ACCESS_KEY_ID"), R2SecretKey: os.Getenv("R2_SECRET_ACCESS_KEY")}
	objects, err := NewObjectStorage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.Objects = objects
	s.Jobs.Objects = objects
	w := upload(t, h, "r2-validation.csv", "id,value\n1,12\n2,19\n")
	requireStatus(t, w, 201)
	var dataset Dataset
	_ = json.Unmarshal(w.Body.Bytes(), &dataset)
	t.Cleanup(func() { _ = objects.Delete(context.Background(), "datasets/"+dataset.ID+".csv") })
	p := samplePlan(t, "custom")
	p.TimeoutSeconds = 120
	p.Code += "\noriginal_run=run_experiment\ndef run_experiment(inputs,dataset_rows):\n    import os\n    result=original_run(inputs,dataset_rows)\n    result['metrics']['dataset_rows_received']=len(dataset_rows)\n    result['metrics']['storage_secret_in_worker']='R2_SECRET_ACCESS_KEY' in os.environ\n    return result\n"
	w = request(t, h, "POST", "/api/v1/experiments", map[string]string{"question": p.ResearchQuestion, "dataset_id": dataset.ID})
	requireStatus(t, w, 201)
	var e Experiment
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	t.Cleanup(func() { _ = objects.DeletePrefix(context.Background(), "experiments/"+e.ID+"/") })
	s.Registry = NewRegistry("python3", "docker", true)
	s.Jobs.registry = s.Registry
	s.Jobs.executor = PythonExecutor{Mode: "docker", Image: "researchos-worker:local"}
	s.Jobs.mode = "docker"
	s.Jobs.image = "researchos-worker:local"
	requireStatus(t, request(t, h, "PUT", "/api/v1/experiments/"+e.ID+"/plan", p), 200)
	requireStatus(t, request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/run", nil), 202)
	s.Jobs.Start()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		e, err = s.Store.Get(e.ID)
		if err != nil {
			t.Fatal(err)
		}
		if e.Status == "completed" || e.Status == "failed" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if e.Status != "completed" {
		t.Fatal("live R2 execution failed:", e.Error)
	}
	if e.Result.Metrics["dataset_rows_received"] != float64(2) || e.Result.Metrics["storage_secret_in_worker"] != false {
		t.Fatal("worker input or credential isolation failed")
	}
	requireStatus(t, request(t, h, "GET", "/api/v1/datasets/"+dataset.ID+"/download", nil), 200)
	requireStatus(t, request(t, h, "GET", "/api/v1/experiments/"+e.ID+"/report", nil), 200)
	w = request(t, h, "GET", "/api/v1/experiments/"+e.ID+"/bundle", nil)
	requireStatus(t, w, 200)
	if w.Body.Len() < 1000 {
		t.Fatal("empty R2 reproduction bundle")
	}
	w = request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/documents", map[string]string{"kind": "manuscript"})
	requireStatus(t, w, 201)
	var document ResearchDocument
	_ = json.Unmarshal(w.Body.Bytes(), &document)
	requireStatus(t, request(t, h, "GET", "/api/v1/documents/"+document.ID+"/download", nil), 200)
	requireStatus(t, request(t, h, "GET", "/api/v1/documents/"+document.ID+"/bundle", nil), 200)
	for _, dir := range []string{"experiments", "datasets"} {
		if _, err = os.Stat(filepath.Join(s.Config.DataDir, dir)); !os.IsNotExist(err) {
			t.Fatal("live R2 workflow persisted research files locally")
		}
	}
	requireStatus(t, request(t, h, "DELETE", "/api/v1/experiments/"+e.ID, nil), 204)
	if _, _, err = objects.Get(context.Background(), documentPrefix(document)+documentSourceName(document)); !errors.Is(err, ErrNotFound) {
		t.Fatal("document was not deleted from R2")
	}
	t.Log("PASS actual R2 CSV upload → isolated computation → source/results/report objects → ZIP and LaTeX downloads → deletion; no local artifact files")
}

func TestIncompleteMigrationRecoversOnlyExactBytes(t *testing.T) {
	s, _ := testServer(t)
	e := completedDocumentExperiment(t, s)
	if err := s.Jobs.writeArtifacts(e, WorkerInput{Plan: e.Plan}, *e.Result); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"results.json", "provenance.json"} {
		if err := os.Remove(filepath.Join(s.Config.DataDir, "experiments", e.ID, "attempt-001", name)); err != nil {
			t.Fatal(err)
		}
	}
	objects := &memoryObjects{}
	report, err := MigrateArtifactsAllowMissing(context.Background(), s.Store, s.Config.DataDir, objects, true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Recovered != 1 || len(report.Missing) != 1 || !strings.HasSuffix(report.Missing[0], "provenance.json") || report.Records != 1 {
		t.Fatalf("incorrect recovery report: %+v", report)
	}
	raw, err := readObject(context.Background(), objects, "experiments/"+e.ID+"/attempt-001/results.json", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.MarshalIndent(e.Result, "", "  ")
	if !bytes.Equal(raw, want) {
		t.Fatal("recovery changed saved results")
	}
	artifacts, _ := s.Store.Artifacts(e.ID)
	if len(artifacts) < 10 {
		t.Fatal("missing artifact references were discarded")
	}
}
func TestExistingObjectMismatchNeverOverwritesOrDeletesSource(t *testing.T) {
	s, _ := testServer(t)
	e := completedDocumentExperiment(t, s)
	if err := s.Jobs.writeArtifacts(e, WorkerInput{Plan: e.Plan}, *e.Result); err != nil {
		t.Fatal(err)
	}
	key := "experiments/" + e.ID + "/attempt-001/results.json"
	objects := &memoryObjects{}
	_ = objects.Put(context.Background(), key, "application/json", []byte("different existing object"))
	if _, err := MigrateArtifacts(context.Background(), s.Store, s.Config.DataDir, objects, true); err == nil {
		t.Fatal("mismatched existing cloud object overwritten")
	}
	if _, err := os.Stat(filepath.Join(s.Config.DataDir, "experiments", e.ID, "attempt-001/results.json")); err != nil {
		t.Fatal("verification failure deleted local evidence")
	}
}
