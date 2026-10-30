package research

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	Config   Config
	Store    *Store
	Registry *Registry
	Model    *ModelClient
	Jobs     *Jobs
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"status": "ok", "service": "kairo", "version": "1.0.0"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := s.Store.db.PingContext(ctx); err != nil {
			apiError(w, 503, "database_unavailable", "Database is unavailable")
			return
		}
		respond(w, 200, map[string]any{"database": "ready", "worker_mode": s.Config.WorkerMode, "model_provider": s.Config.ModelProvider, "model_connectivity": "not_checked"})
	})
	mux.HandleFunc("GET /api/v1/experiments", s.listExperiments)
	mux.HandleFunc("POST /api/v1/experiments", s.createExperiment)
	mux.HandleFunc("GET /api/v1/experiments/{id}", s.getExperiment)
	mux.HandleFunc("PATCH /api/v1/experiments/{id}", s.updateExperiment)
	mux.HandleFunc("DELETE /api/v1/experiments/{id}", s.deleteExperiment)
	mux.HandleFunc("POST /api/v1/experiments/{id}/plan", s.planExperiment)
	mux.HandleFunc("PUT /api/v1/experiments/{id}/plan", s.putPlan)
	mux.HandleFunc("GET /api/v1/experiments/{id}/plans", s.planHistory)
	mux.HandleFunc("POST /api/v1/experiments/{id}/run", s.runExperiment)
	mux.HandleFunc("POST /api/v1/experiments/{id}/cancel", s.cancelExperiment)
	mux.HandleFunc("GET /api/v1/experiments/{id}/events", s.events)
	mux.HandleFunc("GET /api/v1/experiments/{id}/events/stream", s.streamEvents)
	mux.HandleFunc("GET /api/v1/experiments/{id}/artifacts", s.listArtifacts)
	mux.HandleFunc("GET /api/v1/experiments/{id}/artifacts/{name...}", s.downloadArtifact)
	mux.HandleFunc("GET /api/v1/experiments/{id}/bundle", s.bundle)
	mux.HandleFunc("GET /api/v1/experiments/{id}/report", s.report)
	mux.HandleFunc("GET /api/v1/tools", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"data": s.Registry.Tools(), "execution_mode": s.Config.WorkerMode})
	})
	mux.HandleFunc("GET /api/v1/projects", s.projects)
	mux.HandleFunc("POST /api/v1/projects", s.createProject)
	mux.HandleFunc("GET /api/v1/datasets", s.datasets)
	mux.HandleFunc("POST /api/v1/datasets", s.uploadDataset)
	mux.HandleFunc("GET /api/v1/datasets/{id}", s.getDataset)
	mux.HandleFunc("GET /api/v1/datasets/{id}/download", s.downloadDataset)
	mux.HandleFunc("GET /api/v1/reports", s.reports)
	mux.HandleFunc("GET /api/v1/dashboard", s.dashboard)
	mux.HandleFunc("GET /api/v1/activity", s.activity)
	mux.HandleFunc("GET /api/v1/settings", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"model_provider": s.Config.ModelProvider, "model_name": s.Config.ModelName, "model_connectivity": "not_checked", "worker_mode": s.Config.WorkerMode, "workers": s.Config.Workers, "api_auth_enabled": s.Config.Token != "", "supported_adapters": s.Registry.Tools()})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { apiError(w, 404, "not_found", "API route not found") })
	return s.middleware(mux)
}
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Request-ID", newID("req"))
		w.Header().Set("Referrer-Policy", "no-referrer")
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("request panic", "error", recovered)
				apiError(w, 500, "internal_error", "Internal server error")
			}
		}()
		if origin := r.Header.Get("Origin"); origin != "" {
			allowed := false
			for _, o := range s.Config.Origins {
				if origin == o {
					allowed = true
					break
				}
			}
			if !allowed {
				apiError(w, 403, "origin_denied", "Origin is not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,Last-Event-ID")
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID,Content-Disposition")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		if r.URL.Path != "/healthz" && s.Config.Token != "" {
			header := r.Header.Get("Authorization")
			actual := strings.TrimPrefix(header, "Bearer ")
			if !strings.HasPrefix(header, "Bearer ") || subtle.ConstantTimeCompare([]byte(actual), []byte(s.Config.Token)) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				apiError(w, 401, "unauthorized", "A valid bearer token is required")
				return
			}
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
		next.ServeHTTP(w, r)
	})
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("write response", "error", err)
	}
}
func apiError(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func handleError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		apiError(w, 404, "not_found", "Resource not found")
	} else if errors.Is(err, ErrConflict) {
		apiError(w, 409, "state_conflict", err.Error())
	} else {
		slog.Error("API operation failed", "error", err)
		apiError(w, 500, "internal_error", "Operation failed; check server logs")
	}
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		apiError(w, 415, "unsupported_media_type", "Use Content-Type: application/json")
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err == nil {
		err = strictJSON(raw, v)
	}
	if err != nil {
		apiError(w, 400, "invalid_json", err.Error())
		return false
	}
	return true
}
func (s *Server) experiment(w http.ResponseWriter, r *http.Request) (Experiment, bool) {
	e, err := s.Store.Get(r.PathValue("id"))
	if err != nil {
		handleError(w, err)
		return e, false
	}
	return e, true
}
func pageParams(r *http.Request) (int, int, error) {
	limit, offset := 50, 0
	var err error
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, err
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		offset, err = strconv.Atoi(v)
		if err != nil {
			return 0, 0, err
		}
	}
	if limit < 1 || limit > 200 || offset < 0 || offset > 1000000 {
		return 0, 0, errors.New("limit must be 1–200 and offset 0–1000000")
	}
	return limit, offset, nil
}
func (s *Server) listExperiments(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := pageParams(r)
	if err != nil {
		apiError(w, 400, "invalid_pagination", err.Error())
		return
	}
	data, total, err := s.Store.List(r.URL.Query().Get("status"), r.URL.Query().Get("q"), r.URL.Query().Get("project_id"), limit, offset)
	if err != nil {
		handleError(w, err)
		return
	}
	respond(w, 200, map[string]any{"data": data, "total": total, "limit": limit, "offset": offset})
}
func (s *Server) createExperiment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title     string `json:"title"`
		Question  string `json:"question"`
		ProjectID string `json:"project_id"`
		DatasetID string `json:"dataset_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Question = strings.TrimSpace(req.Question)
	req.Title = strings.TrimSpace(req.Title)
	if len(req.Question) < 5 || len(req.Question) > 4000 || len(req.Title) > 200 {
		apiError(w, 422, "invalid_experiment", "question must be 5–4000 characters; title at most 200")
		return
	}
	if req.Title == "" {
		req.Title = req.Question
		if len(req.Title) > 160 {
			req.Title = string([]rune(req.Title)[:min(160, len([]rune(req.Title)))])
		}
	}
	if req.ProjectID != "" && !s.Store.ProjectExists(req.ProjectID) {
		apiError(w, 422, "invalid_project", "Project does not exist")
		return
	}
	if req.DatasetID != "" {
		if _, err := s.Store.Dataset(req.DatasetID); err != nil {
			apiError(w, 422, "invalid_dataset", "Dataset does not exist")
			return
		}
	}
	now := time.Now().UTC()
	e := Experiment{ID: newID("exp"), Title: req.Title, Question: req.Question, ProjectID: req.ProjectID, DatasetID: req.DatasetID, Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := s.Store.Create(e); err != nil {
		handleError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/experiments/"+e.ID)
	respond(w, 201, e)
}
func (s *Server) getExperiment(w http.ResponseWriter, r *http.Request) {
	if e, ok := s.experiment(w, r); ok {
		respond(w, 200, e)
	}
}
func (s *Server) updateExperiment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title     *string `json:"title"`
		Question  *string `json:"question"`
		ProjectID *string `json:"project_id"`
		DatasetID *string `json:"dataset_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Title != nil && (strings.TrimSpace(*req.Title) == "" || len(*req.Title) > 200) || req.Question != nil && (len(strings.TrimSpace(*req.Question)) < 5 || len(*req.Question) > 4000) {
		apiError(w, 422, "invalid_experiment", "Invalid title or research question")
		return
	}
	if req.ProjectID != nil && *req.ProjectID != "" && !s.Store.ProjectExists(*req.ProjectID) {
		apiError(w, 422, "invalid_project", "Project does not exist")
		return
	}
	if req.DatasetID != nil && *req.DatasetID != "" {
		if _, err := s.Store.Dataset(*req.DatasetID); err != nil {
			apiError(w, 422, "invalid_dataset", "Dataset does not exist")
			return
		}
	}
	e, err := s.Store.Mutate(r.PathValue("id"), "updated", "Experiment updated", func(e *Experiment) error {
		if e.Status == "running" || e.Status == "queued" || e.Status == "planning" {
			return ErrConflict
		}
		if (req.Question != nil || req.DatasetID != nil) && e.Attempt > 0 {
			return stateError("create a new experiment to change the question or dataset after execution")
		}
		if req.Title != nil {
			e.Title = strings.TrimSpace(*req.Title)
		}
		if req.ProjectID != nil {
			e.ProjectID = *req.ProjectID
		}
		if req.Question != nil || req.DatasetID != nil {
			if req.Question != nil {
				e.Question = strings.TrimSpace(*req.Question)
			}
			if req.DatasetID != nil {
				e.DatasetID = *req.DatasetID
			}
			e.Plan = nil
			e.Status = "draft"
			e.Error = ""
			e.Progress = 0
		}
		return nil
	})
	if err != nil {
		handleError(w, err)
		return
	}
	respond(w, 200, e)
}
func (s *Server) deleteExperiment(w http.ResponseWriter, r *http.Request) {
	e, ok := s.experiment(w, r)
	if !ok {
		return
	}
	if err := s.Store.Delete(e.ID); err != nil {
		handleError(w, err)
		return
	}
	if err := os.RemoveAll(filepath.Join(s.Config.DataDir, "experiments", e.ID)); err != nil {
		slog.Warn("artifact cleanup failed", "error", err)
	}
	w.WriteHeader(204)
}
func canPlan(e *Experiment) error {
	if e.Status == "running" || e.Status == "queued" || e.Status == "planning" || e.Status == "completed" {
		return stateError("planning requires a draft, planned, failed, cancelled, or needs_review experiment")
	}
	return nil
}
func (s *Server) planExperiment(w http.ResponseWriter, r *http.Request) {
	if s.Config.ModelProvider == "disabled" {
		apiError(w, 503, "model_unavailable", "AI planning is disabled; configure a model or PUT a manual plan")
		return
	}
	e, err := s.Store.Mutate(r.PathValue("id"), "planning", "Requesting a structured research plan from the configured model", func(e *Experiment) error {
		if err := canPlan(e); err != nil {
			return err
		}
		e.Status = "planning"
		e.Error = ""
		e.Progress = 5
		return nil
	})
	if err != nil {
		handleError(w, err)
		return
	}
	planningStarted := e.UpdatedAt
	var dataset *Dataset
	if e.DatasetID != "" {
		d, err := s.Store.Dataset(e.DatasetID)
		if err != nil {
			handleError(w, err)
			return
		}
		dataset = &d
	}
	p, err := s.Model.Plan(r.Context(), e, s.Registry, dataset)
	if err != nil {
		_, saveErr := s.Store.Mutate(e.ID, "planning_failed", "AI plan unavailable or invalid", func(e *Experiment) error {
			if e.Status != "planning" || !e.UpdatedAt.Equal(planningStarted) {
				return ErrConflict
			}
			e.Status = "needs_review"
			e.Error = err.Error()
			return nil
		})
		if errors.Is(saveErr, ErrConflict) {
			handleError(w, saveErr)
			return
		}
		if saveErr != nil {
			handleError(w, saveErr)
			return
		}
		apiError(w, 422, "planning_failed", err.Error())
		return
	}
	e, err = s.Store.Mutate(e.ID, "planned", "Validated research plan saved; explicit run required", func(e *Experiment) error {
		if e.Status != "planning" || !e.UpdatedAt.Equal(planningStarted) {
			return ErrConflict
		}
		e.Plan = &p
		e.PlanVersion++
		e.Status = "planned"
		e.Progress = 10
		return nil
	})
	if err != nil {
		handleError(w, err)
		return
	}
	respond(w, 200, e)
}
func (s *Server) putPlan(w http.ResponseWriter, r *http.Request) {
	var p Plan
	if !decode(w, r, &p) {
		return
	}
	e, ok := s.experiment(w, r)
	if !ok {
		return
	}
	if p.ResearchQuestion != e.Question {
		apiError(w, 422, "invalid_plan", "research_question must match the experiment question")
		return
	}
	p.Source = "manual"
	p.Model = ""
	if err := s.Registry.Validate(p, false); err != nil {
		apiError(w, 422, "invalid_plan", err.Error())
		return
	}
	if p.Adapter == "statistics.welch" && e.DatasetID == "" {
		apiError(w, 422, "dataset_required", "Attach a dataset before planning a statistical comparison")
		return
	}
	datasetID := e.DatasetID
	e, err := s.Store.Mutate(e.ID, "planned", "Manual plan validated and versioned; explicit run required", func(e *Experiment) error {
		if err := canPlan(e); err != nil {
			return err
		}
		if e.Question != p.ResearchQuestion || e.DatasetID != datasetID {
			return ErrConflict
		}
		e.Plan = &p
		e.PlanVersion++
		e.Status = "planned"
		e.Progress = 10
		e.Error = ""
		return nil
	})
	if err != nil {
		handleError(w, err)
		return
	}
	respond(w, 200, e)
}
func (s *Server) planHistory(w http.ResponseWriter, r *http.Request) {
	e, ok := s.experiment(w, r)
	if !ok {
		return
	}
	data, err := s.Store.PlanHistory(e.ID)
	if err != nil {
		handleError(w, err)
		return
	}
	respond(w, 200, map[string]any{"data": data})
}
func (s *Server) runExperiment(w http.ResponseWriter, r *http.Request) {
	e, ok := s.experiment(w, r)
	if !ok {
		return
	}
	if e.Plan == nil {
		apiError(w, 409, "plan_required", "Create a validated plan before execution")
		return
	}
	if err := s.Registry.Validate(*e.Plan, true); err != nil {
		apiError(w, 422, "invalid_plan", err.Error())
		return
	}
	if _, err := s.Jobs.Input(e); err != nil {
		apiError(w, 422, "invalid_dataset", err.Error())
		return
	}
	version := e.PlanVersion
	datasetID := e.DatasetID
	e, err := s.Store.Mutate(e.ID, "queued", "Experiment added to durable execution queue", func(e *Experiment) error {
		if e.Status != "planned" && e.Status != "failed" && e.Status != "cancelled" {
			return stateError("only planned, failed, or cancelled experiments can be queued")
		}
		if e.PlanVersion != version || e.Plan == nil || e.DatasetID != datasetID {
			return ErrConflict
		}
		e.Status = "queued"
		e.Progress = 10
		e.Error = ""
		e.Result = nil
		e.Attempt++
		return nil
	})
	if err != nil {
		handleError(w, err)
		return
	}
	s.Jobs.Wake()
	respond(w, 202, e)
}
func (s *Server) cancelExperiment(w http.ResponseWriter, r *http.Request) {
	e, err := s.Store.Mutate(r.PathValue("id"), "cancelled", "Cancellation requested", func(e *Experiment) error {
		if e.Status != "queued" && e.Status != "running" && e.Status != "planning" {
			return ErrConflict
		}
		e.Status = "cancelled"
		e.Error = "Cancelled by user"
		return nil
	})
	if err != nil {
		handleError(w, err)
		return
	}
	s.Jobs.Cancel(e.ID, e.Attempt)
	respond(w, 200, e)
}
func eventAfter(r *http.Request) (int64, error) {
	v := r.URL.Query().Get("after")
	if v == "" {
		v = r.Header.Get("Last-Event-ID")
	}
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, errors.New("event cursor must be a nonnegative integer")
	}
	return n, nil
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	e, ok := s.experiment(w, r)
	if !ok {
		return
	}
	after, err := eventAfter(r)
	if err != nil {
		apiError(w, 400, "invalid_cursor", err.Error())
		return
	}
	data, err := s.Store.Events(e.ID, after, 200)
	if err != nil {
		handleError(w, err)
		return
	}
	respond(w, 200, map[string]any{"data": data})
}
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	e, ok := s.experiment(w, r)
	if !ok {
		return
	}
	after, err := eventAfter(r)
	if err != nil {
		apiError(w, 400, "invalid_cursor", err.Error())
		return
	}
	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	for {
		events, err := s.Store.Events(e.ID, after, 200)
		if err != nil {
			return
		}
		_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
		for _, event := range events {
			b, _ := json.Marshal(event)
			if _, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Kind, b); err != nil {
				return
			}
			after = event.ID
		}
		if err = controller.Flush(); err != nil {
			return
		}
		current, err := s.Store.Get(e.ID)
		if err != nil {
			return
		}
		if terminal(current.Status) && len(events) < 200 {
			last, err := s.Store.Events(e.ID, after, 1)
			if err != nil {
				return
			}
			if len(last) == 0 {
				_, _ = fmt.Fprint(w, "event: end\ndata: {}\n\n")
				_ = controller.Flush()
				return
			}
			continue
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		case <-heartbeat.C:
			_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if _, err = fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if err = controller.Flush(); err != nil {
				return
			}
		}
	}
}
func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	e, ok := s.experiment(w, r)
	if !ok {
		return
	}
	data, err := s.Store.Artifacts(e.ID)
	if err != nil {
		handleError(w, err)
		return
	}
	respond(w, 200, map[string]any{"data": data})
}
func (s *Server) artifact(w http.ResponseWriter, r *http.Request, e Experiment, name string) bool {
	as, err := s.Store.Artifacts(e.ID)
	if err != nil {
		handleError(w, err)
		return false
	}
	for _, a := range as {
		if a.Name == name {
			w.Header().Set("Content-Type", a.ContentType)
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(name)}))
			http.ServeFile(w, r, filepath.Join(s.Config.DataDir, "experiments", e.ID, a.Name))
			return true
		}
	}
	apiError(w, 404, "artifact_not_found", "Artifact is not available")
	return false
}
func (s *Server) downloadArtifact(w http.ResponseWriter, r *http.Request) {
	e, ok := s.experiment(w, r)
	if ok {
		s.artifact(w, r, e, r.PathValue("name"))
	}
}
func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	e, ok := s.experiment(w, r)
	if !ok {
		return
	}
	if e.Status != "completed" {
		apiError(w, 409, "report_unavailable", "Report is available after successful execution")
		return
	}
	s.artifact(w, r, e, fmt.Sprintf("attempt-%03d/report.md", e.Attempt))
}
func (s *Server) bundle(w http.ResponseWriter, r *http.Request) {
	e, ok := s.experiment(w, r)
	if !ok {
		return
	}
	as, err := s.Store.Artifacts(e.ID)
	if err != nil {
		handleError(w, err)
		return
	}
	if len(as) == 0 {
		apiError(w, 404, "artifacts_unavailable", "No artifacts are available yet")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": e.ID + "-reproducibility.zip"}))
	zw := zip.NewWriter(w)
	defer zw.Close()
	for _, a := range as {
		f, err := os.Open(filepath.Join(s.Config.DataDir, "experiments", e.ID, a.Name))
		if err != nil {
			slog.Error("bundle file", "error", err)
			return
		}
		entry, err := zw.Create(a.Name)
		if err == nil {
			_, err = io.Copy(entry, f)
		}
		f.Close()
		if err != nil {
			return
		}
	}
}
func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	data, err := s.Store.Projects()
	if err != nil {
		handleError(w, err)
		return
	}
	respond(w, 200, map[string]any{"data": data})
}
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 120 || len(req.Description) > 2000 {
		apiError(w, 422, "invalid_project", "name is required (max 120); description max 2000")
		return
	}
	p := Project{newID("prj"), req.Name, req.Description, time.Now().UTC()}
	if err := s.Store.SaveProject(p); err != nil {
		handleError(w, err)
		return
	}
	respond(w, 201, p)
}
func (s *Server) datasets(w http.ResponseWriter, r *http.Request) {
	data, err := s.Store.Datasets()
	if err != nil {
		handleError(w, err)
		return
	}
	respond(w, 200, map[string]any{"data": data})
}
func (s *Server) getDataset(w http.ResponseWriter, r *http.Request) {
	d, err := s.Store.Dataset(r.PathValue("id"))
	if err != nil {
		handleError(w, err)
		return
	}
	respond(w, 200, d)
}
func (s *Server) downloadDataset(w http.ResponseWriter, r *http.Request) {
	d, err := s.Store.Dataset(r.PathValue("id"))
	if err != nil {
		handleError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": d.Name}))
	http.ServeFile(w, r, filepath.Join(s.Config.DataDir, "datasets", d.ID+".csv"))
}
func (s *Server) uploadDataset(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 11<<20)
	reader, err := r.MultipartReader()
	if err != nil {
		apiError(w, 400, "invalid_upload", "Use multipart/form-data with one CSV file field named file")
		return
	}
	var raw []byte
	name := ""
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			apiError(w, 400, "invalid_upload", err.Error())
			return
		}
		if part.FormName() != "file" || name != "" {
			part.Close()
			apiError(w, 400, "invalid_upload", "Exactly one file field is required")
			return
		}
		name = filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
		if len(name) > 180 || strings.ToLower(filepath.Ext(name)) != ".csv" {
			part.Close()
			apiError(w, 422, "invalid_dataset", "File must have a .csv extension (name max 180 characters)")
			return
		}
		raw, err = io.ReadAll(io.LimitReader(part, 10<<20+1))
		part.Close()
		if err != nil || len(raw) > 10<<20 {
			apiError(w, 413, "dataset_too_large", "CSV must be at most 10 MiB")
			return
		}
	}
	if name == "" {
		apiError(w, 400, "missing_file", "A CSV file is required")
		return
	}
	raw = []byte(strings.TrimPrefix(string(raw), "\ufeff"))
	csvReader := csv.NewReader(strings.NewReader(string(raw)))
	header, err := csvReader.Read()
	if err != nil || len(header) < 2 || len(header) > 100 {
		apiError(w, 422, "invalid_csv", "CSV requires a header with 2–100 columns")
		return
	}
	seen := map[string]bool{}
	for _, h := range header {
		if h == "" || strings.TrimSpace(h) != h || len(h) > 100 || seen[h] {
			apiError(w, 422, "invalid_csv", "Column names must be nonempty, unique, trimmed and at most 100 characters")
			return
		}
		seen[h] = true
	}
	preview := [][]string{}
	rows := 0
	for {
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			apiError(w, 422, "invalid_csv", err.Error())
			return
		}
		rows++
		if rows > 50000 {
			apiError(w, 422, "dataset_too_large", "CSV must have at most 50000 rows")
			return
		}
		for _, v := range row {
			if len(v) > 10000 {
				apiError(w, 422, "invalid_csv", "CSV cells must be at most 10000 characters")
				return
			}
		}
		if len(preview) < 5 {
			preview = append(preview, row)
		}
	}
	if rows == 0 {
		apiError(w, 422, "invalid_csv", "CSV must contain data rows")
		return
	}
	sum := sha256.Sum256(raw)
	d := Dataset{newID("ds"), name, rows, header, preview, hex.EncodeToString(sum[:]), int64(len(raw)), time.Now().UTC()}
	dir := filepath.Join(s.Config.DataDir, "datasets")
	if err = os.MkdirAll(dir, 0700); err != nil {
		handleError(w, err)
		return
	}
	path := filepath.Join(dir, d.ID+".csv")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		handleError(w, err)
		return
	}
	if err = s.Store.SaveDataset(d); err != nil {
		_ = os.Remove(path)
		handleError(w, err)
		return
	}
	respond(w, 201, d)
}
func (s *Server) reports(w http.ResponseWriter, r *http.Request) {
	runs, _, err := s.Store.List("completed", "", "", 1000000, 0)
	if err != nil {
		handleError(w, err)
		return
	}
	reports := []map[string]any{}
	for _, e := range runs {
		reports = append(reports, map[string]any{"experiment_id": e.ID, "title": e.Title, "updated_at": e.UpdatedAt, "report_url": "/api/v1/experiments/" + e.ID + "/report", "bundle_url": "/api/v1/experiments/" + e.ID + "/bundle"})
	}
	respond(w, 200, map[string]any{"data": reports})
}
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	runs, total, err := s.Store.List("", "", "", 1000000, 0)
	if err != nil {
		handleError(w, err)
		return
	}
	counts := map[string]int{"draft": 0, "planning": 0, "planned": 0, "queued": 0, "running": 0, "completed": 0, "failed": 0, "cancelled": 0, "needs_review": 0}
	now := time.Now().UTC()
	days := []map[string]any{}
	for i := 6; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		n := 0
		for _, e := range runs {
			if e.CreatedAt.Format("2006-01-02") == day {
				n++
			}
		}
		days = append(days, map[string]any{"date": day, "experiments_created": n})
	}
	for _, e := range runs {
		counts[e.Status]++
	}
	respond(w, 200, map[string]any{"total_experiments": total, "status_counts": counts, "reports": counts["completed"], "activity": days, "timezone": "UTC", "sample_data": false})
}
func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.db.Query("SELECT id,experiment_id,kind,message,created_at FROM events ORDER BY id DESC LIMIT 50")
	if err != nil {
		handleError(w, err)
		return
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var ts string
		if err = rows.Scan(&e.ID, &e.ExperimentID, &e.Kind, &e.Message, &ts); err != nil {
			handleError(w, err)
			return
		}
		e.CreatedAt, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			handleError(w, err)
			return
		}
		out = append(out, e)
	}
	if err = rows.Err(); err != nil {
		handleError(w, err)
		return
	}
	respond(w, 200, map[string]any{"data": out})
}
