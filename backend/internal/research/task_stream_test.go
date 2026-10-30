package research

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestModelStreamsOutputAcrossProviders(t *testing.T) {
	for _, provider := range []string{"google", "ollama", "compatible"} {
		t.Run(provider, func(t *testing.T) {
			text := `{"code":"print(1)","message":"An answer"}`
			split := len(text) / 2
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if provider == "google" {
					if !strings.HasSuffix(r.URL.Path, ":streamGenerateContent") || r.URL.Query().Get("alt") != "sse" {
						t.Error("Google SSE endpoint missing")
					}
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"private thought\",\"thought\":true}]}}]}\n\n")
				} else {
					if body["stream"] != true {
						t.Error("streaming flag absent")
					}
					w.Header().Set("Content-Type", "text/event-stream")
				}
				for i, chunk := range []string{text[:split], text[split:]} {
					var payload any
					if provider == "google" {
						candidate := map[string]any{"index": 0, "content": map[string]any{"parts": []any{map[string]string{"text": chunk}}}}
						if i == 1 {
							candidate["finishReason"] = "STOP"
						}
						payload = map[string]any{"candidates": []any{candidate}}
					} else if provider == "ollama" {
						payload = map[string]any{"response": chunk, "done": i == 1}
					} else {
						choice := map[string]any{"index": 0, "delta": map[string]string{"content": chunk}}
						if i == 1 {
							choice["finish_reason"] = "stop"
						}
						payload = map[string]any{"choices": []any{choice}}
					}
					raw, _ := json.Marshal(payload)
					if provider == "ollama" {
						fmt.Fprintf(w, "%s\n", raw)
					} else {
						fmt.Fprintf(w, "data: %s\n\n", raw)
					}
					w.(http.Flusher).Flush()
					time.Sleep(10 * time.Millisecond)
				}
			}))
			defer server.Close()
			chunks := []string{}
			ctx := context.WithValue(context.Background(), taskStreamKey{}, taskEmitter(func(event string, data any) {
				if event == "delta" {
					chunks = append(chunks, data.(map[string]string)["text"])
				}
			}))
			model := NewModelClient(provider, server.URL, "gemma-4-test", "private-api-key")
			got, err := model.Generate(ctx, "Write code", "A synthetic test", true)
			if err != nil || got != text {
				t.Fatalf("incorrect streamed model response: %q %v", got, err)
			}
			if len(chunks) != 2 || strings.Join(chunks, "") != text {
				t.Fatal("output was buffered or contained non-output content")
			}
		})
	}
}
func TestModelRejectsIncompleteOrBlockedStream(t *testing.T) {
	cases := []struct{ provider, body string }{{"ollama", `{"response":"unfinished","done":false}` + "\n"}, {"google", "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"unfinished\"}]},\"finishReason\":\"MAX_TOKENS\"}]}\n\n"}, {"compatible", "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"length\"}]}\n\n"}}
	for _, item := range cases {
		model := NewModelClient(item.provider, "", "", "")
		ctx := context.WithValue(context.Background(), taskStreamKey{}, taskEmitter(func(string, any) {}))
		if _, err := model.readModelStream(ctx, strings.NewReader(item.body)); err == nil {
			t.Fatal("incomplete stream accepted", item.provider)
		}
	}
}
func TestTaskStreamFlushesBeforeCompletion(t *testing.T) {
	release := make(chan struct{})
	var finished atomic.Bool
	server := httptest.NewServer(withTaskStream(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Question string `json:"question"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Question != "Test input" {
			apiError(w, 400, "invalid_body", "The initial stream flush consumed the request body")
			return
		}
		taskStatus(r.Context(), "Writing code")
		taskEmit(r.Context(), "delta", map[string]string{"text": "print("})
		<-release
		finished.Store(true)
		respond(w, 201, map[string]bool{"saved": true})
	}))
	defer server.Close()
	response, err := http.Post(server.URL, "application/json", strings.NewReader(`{"question":"Test input"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(line, "print(") {
			break
		}
	}
	if finished.Load() {
		t.Fatal("client received output only after task completion")
	}
	close(release)
	rest, _ := io.ReadAll(reader)
	if !strings.Contains(string(rest), "event: done") || !strings.Contains(string(rest), `"saved":true`) {
		t.Fatal("missing final result")
	}
	recorder := httptest.NewRecorder()
	withTaskStream(func(w http.ResponseWriter, r *http.Request) {
		apiError(w, 422, "invalid_plan", "Missing required method")
	})(recorder, httptest.NewRequest("POST", "/", nil))
	if !strings.Contains(recorder.Body.String(), "event: error") || strings.Contains(recorder.Body.String(), "event: done") {
		t.Fatal("failed task was announced as complete")
	}
}
func TestModelStreamCancellation(t *testing.T) {
	upstreamCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "{\"response\":\"partial\",\"done\":false}\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(upstreamCanceled)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = context.WithValue(ctx, taskStreamKey{}, taskEmitter(func(event string, data any) {
		if event == "delta" {
			cancel()
		}
	}))
	_, err := NewModelClient("ollama", server.URL, "test", "").Generate(ctx, "test", "test", true)
	if err == nil {
		t.Fatal("canceled stream succeeded")
	}
	select {
	case <-upstreamCanceled:
	case <-time.After(time.Second):
		t.Fatal("provider request continued after disconnect")
	}
}
func TestStreamingPlanAndDocumentOnlySaveFinalValidatedOutput(t *testing.T) {
	s, h := testServer(t)
	p := samplePlan(t, "custom")
	completed := completedDocumentExperiment(t, s)
	objects := &memoryObjects{}
	s.Objects = objects
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		var output any = p
		if strings.Contains(body["system"].(string), "writing agent") {
			output = documentSections{Title: "Recorded study", Abstract: "Computed evidence.", Introduction: "The original question.", Methods: "Recorded method.", Results: "Recorded final value is 99.4.", Discussion: "Model limitations apply.", Conclusion: "Review assumptions."}
		}
		raw, _ := json.Marshal(output)
		middle := len(raw) / 2
		for i, chunk := range []string{string(raw[:middle]), string(raw[middle:])} {
			_ = json.NewEncoder(w).Encode(map[string]any{"response": chunk, "done": i == 1})
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()
	s.Model = NewModelClient("ollama", server.URL, "test-open-weight", "")
	s.Config.ModelProvider = "ollama"
	w := request(t, h, "POST", "/api/v1/experiments", map[string]string{"question": p.ResearchQuestion})
	requireStatus(t, w, 201)
	var e Experiment
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	w = request(t, h, "POST", "/api/v1/experiments/"+e.ID+"/plan/stream", nil)
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "event: delta") || !strings.Contains(w.Body.String(), "event: done") {
		t.Fatal("plan output was not streamed")
	}
	saved, _ := s.Store.Get(e.ID)
	if saved.Status != "planned" || saved.PlanVersion != 1 {
		t.Fatal("final plan not validated and saved")
	}
	w = request(t, h, "POST", "/api/v1/experiments/"+completed.ID+"/documents/stream", map[string]any{"kind": "manuscript", "include_figures": false})
	requireStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "event: delta") || !strings.Contains(w.Body.String(), "event: done") {
		t.Fatal("manuscript was not streamed")
	}
	docs, err := s.Store.ResearchDocuments("", completed.ID, 10, 0)
	if err != nil || len(docs) != 1 {
		t.Fatal("validated manuscript was not saved", err)
	}
	if len(objects.files) == 0 {
		t.Fatal("document was not uploaded to object storage")
	}
}
