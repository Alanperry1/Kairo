package research

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWorkspaceKnowledgeRetrievesSavedEvidenceAndStreamsSources(t *testing.T) {
	s, h := testServer(t)
	e := completedDocumentExperiment(t, s)
	objects := &memoryObjects{}
	s.Objects = objects
	w := request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/documents", map[string]any{"kind": "manuscript", "include_figures": false})
	requireStatus(t, w, 201)
	var d ResearchDocument
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	// The manuscript body is in object storage, not duplicated into Postgres.
	stored, _ := s.Store.ResearchDocument(d.ID)
	if stored.Content != "" {
		t.Fatal("document was copied into database")
	}
	raw := []byte("# Recorded source\nprint(99.4)\n")
	hash := sha256.Sum256(raw)
	artifact := Artifact{Name: "attempt-001/experiment.py", Size: int64(len(raw)), ContentType: "text/plain", SHA256: hex.EncodeToString(hash[:])}
	if err := s.objectStorage().Put(context.Background(), "experiments/"+e.ID+"/"+artifact.Name, "text/plain", raw); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.SaveArtifacts(e.ID, []Artifact{artifact}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	catalog, err := s.Store.searchWorkspace(ctx, e.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, v := range catalog.Sources {
		kinds[v.Kind] = true
	}
	for _, kind := range []string{"experiment", "manuscript", "artifact", "activity"} {
		if !kinds[kind] {
			t.Fatal("missing registered resource", kind)
		}
	}
	overview, err := s.Store.workspaceOverview(ctx)
	if err != nil || !strings.Contains(string(overview), `"completed": 1`) {
		t.Fatal("incorrect workspace summary", string(overview), err)
	}
	source, body, err := s.readWorkspace(ctx, WorkspaceRead{Kind: "manuscript", ID: d.ID}, "population")
	b, _ := json.Marshal(body)
	if err != nil || source.ExperimentID != e.ID || !strings.Contains(string(b), "99.4") {
		t.Fatal("object-storage manuscript not retrieved", err)
	}
	_, body, err = s.readWorkspace(ctx, WorkspaceRead{Kind: "artifact", ID: e.ID + "/" + artifact.Name}, "code")
	b, _ = json.Marshal(body)
	if err != nil || !strings.Contains(string(b), "print(99.4)") {
		t.Fatal("registered code artifact not retrieved", err)
	}
	if _, _, err = s.readWorkspace(ctx, WorkspaceRead{Kind: "artifact", ID: e.ID + "/../../.env"}, ""); err == nil {
		t.Fatal("arbitrary path read")
	}
	calls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		prompt := req["prompt"].(string)
		reply := AssistantReply{Message: "Reading the saved study and manuscript.", WorkspaceRead: []WorkspaceRead{{Kind: "experiment", ID: e.ID}, {Kind: "manuscript", ID: d.ID}}}
		if calls == 2 {
			if !strings.Contains(prompt, "99.4") || !strings.Contains(prompt, "Author-review draft") || !strings.Contains(prompt, "workspace_summary") {
				t.Error("retrieved evidence missing")
			}
			reply = AssistantReply{Message: "The recorded final population is 99.4. The study uses an illustrative model; this is not observed biological evidence.", Sources: []WorkspaceSource{{Key: "fake", ID: "made-up", Title: "Invented source"}}}
		}
		data, _ := json.Marshal(reply)
		for i, chunk := range []string{string(data[:len(data)/2]), string(data[len(data)/2:])} {
			fmt.Fprintf(w, "{\"response\":%q,\"done\":%t}\n", chunk, i == 1)
			w.(http.Flusher).Flush()
		}
	}))
	defer model.Close()
	s.Model = NewModelClient("ollama", model.URL, "test-open-weight", "")
	w = request(t, h, "POST", "/api/v1/assistant/chat/stream", map[string]any{"messages": []AssistantMessage{{Role: "user", Content: "Explain my population study and its saved manuscript."}}})
	requireStatus(t, w, 200)
	response := w.Body.String()
	if calls != 2 || !strings.Contains(response, "event: delta") || !strings.Contains(response, "event: done") {
		t.Fatal("retrieval did not stream a final response", response)
	}
	for _, line := range strings.Split(response, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event struct {
			Result *AssistantReply `json:"result"`
		}
		_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event)
		if event.Result != nil {
			ids := map[string]bool{}
			for _, source := range event.Result.Sources {
				ids[source.ID] = true
			}
			if len(event.Result.Sources) != 2 || !ids[e.ID] || !ids[d.ID] {
				t.Fatal("sources were fabricated or did not reflect reads", event.Result.Sources)
			}
		}
	}
	// Integrity failures cannot be turned into model evidence.
	if err = s.objectStorage().Put(ctx, "experiments/"+e.ID+"/"+artifact.Name, "text/plain", []byte("changed")); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.readWorkspace(ctx, WorkspaceRead{Kind: "artifact", ID: e.ID + "/" + artifact.Name}, ""); err == nil {
		t.Fatal("tampered artifact accepted")
	}
	// All matches can be reached, including records beyond the initial page.
	for i := 0; i < 24; i++ {
		copy := e
		copy.ID = fmt.Sprintf("exp_catalog_%02d", i)
		copy.Title = fmt.Sprintf("Rare glacier study %02d", i)
		if err = s.Store.Create(copy); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.Store.searchWorkspace(ctx, "Rare glacier", 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Store.searchWorkspace(ctx, "Rare glacier", 20)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 24 || len(first.Sources) != 20 || len(second.Sources) != 4 {
		t.Fatal("workspace catalog was silently truncated", first.Total, len(first.Sources), len(second.Sources))
	}
}

func TestWorkspaceSnippetsBoundAndDisclosePartialEvidence(t *testing.T) {
	text := strings.Repeat("ⱥ研究", 1000) + " unique-signal " + strings.Repeat("more text ", 1000)
	snippet := workspaceSnippet(text, "unique-signal", 1000)
	value := snippet["text"].(string)
	if snippet["truncated"] != true || len(value) > 1000 || !utf8.ValidString(value) || !strings.Contains(value, "unique-signal") {
		t.Fatal("bad bounded evidence excerpt")
	}
	if workspaceTerms("'; DROP TABLE experiments; --") != "drop OR table OR experiments" {
		t.Fatal("query tokenization failed")
	}
}
