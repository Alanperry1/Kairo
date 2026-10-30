package research

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type taskStreamKey struct{}
type taskEmitter func(string, any)

func taskEmit(ctx context.Context, event string, data any) {
	if emit, ok := ctx.Value(taskStreamKey{}).(taskEmitter); ok {
		emit(event, data)
	}
}
func taskStatus(ctx context.Context, message string) {
	taskEmit(ctx, "status", map[string]string{"message": message})
}
func hasTaskStream(ctx context.Context) bool {
	_, ok := ctx.Value(taskStreamKey{}).(taskEmitter)
	return ok
}

type taskResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *taskResponse) Header() http.Header { return r.header }
func (r *taskResponse) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
}
func (r *taskResponse) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	if r.body.Len()+len(b) > 8<<20 {
		return 0, fmt.Errorf("task response too large")
	}
	return r.body.Write(b)
}
func withTaskStream(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		controller := http.NewResponseController(w)
		// Keep the request body readable after the initial SSE flush on HTTP/1.
		// HTTP/2 already permits concurrent request reads and response writes.
		_ = controller.EnableFullDuplex()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		var mutex sync.Mutex
		closed := false
		emit := taskEmitter(func(event string, value any) {
			mutex.Lock()
			defer mutex.Unlock()
			if closed || ctx.Err() != nil {
				return
			}
			raw, err := json.Marshal(value)
			if err != nil {
				return
			}
			_ = controller.SetWriteDeadline(time.Now().Add(20 * time.Second))
			if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err == nil {
				err = controller.Flush()
			}
			if err != nil {
				cancel()
			}
		})
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			ticker := time.NewTicker(8 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					emit("heartbeat", map[string]any{})
				}
			}
		}()
		defer func() { mutex.Lock(); closed = true; mutex.Unlock() }()
		defer func() {
			if recovered := recover(); recovered != nil {
				emit("error", map[string]any{"status": 500, "error": map[string]string{"code": "task_failed", "message": "The task stopped unexpectedly. Refresh the workspace to check its saved state."}})
			}
		}()
		emit("status", map[string]string{"message": "Connected to Kairo"})
		recorder := &taskResponse{header: make(http.Header)}
		handler(recorder, r.WithContext(context.WithValue(ctx, taskStreamKey{}, emit)))
		if ctx.Err() != nil {
			return
		}
		if recorder.status >= 400 {
			var body map[string]any
			_ = json.Unmarshal(recorder.body.Bytes(), &body)
			if body == nil {
				body = map[string]any{"error": map[string]string{"message": "The task failed."}}
			}
			body["status"] = recorder.status
			emit("error", body)
			return
		}
		if !json.Valid(recorder.body.Bytes()) {
			emit("error", map[string]any{"status": 502, "error": map[string]string{"code": "invalid_task_result", "message": "The task ended without a complete result. Refresh to check its state."}})
			return
		}
		emit("done", map[string]any{"status": recorder.status, "result": json.RawMessage(recorder.body.Bytes())})
	}
}
