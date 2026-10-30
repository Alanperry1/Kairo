package research

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("operation conflicts with experiment state")

type Experiment struct {
	Language    string    `json:"language,omitempty"`
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Question    string    `json:"question"`
	ProjectID   string    `json:"project_id,omitempty"`
	DatasetID   string    `json:"dataset_id,omitempty"`
	Status      string    `json:"status"`
	Attempt     int       `json:"attempt"`
	Progress    int       `json:"progress"`
	Plan        *Plan     `json:"plan,omitempty"`
	PlanVersion int       `json:"plan_version"`
	Result      *Result   `json:"result,omitempty"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type Plan struct {
	Language             string          `json:"language,omitempty"`
	LanguageReason       string          `json:"language_reason,omitempty"`
	Code                 string          `json:"code,omitempty"`
	ResearchQuestion     string          `json:"research_question"`
	Domain               string          `json:"domain"`
	Hypothesis           string          `json:"hypothesis"`
	Adapter              string          `json:"adapter"`
	Methodology          string          `json:"methodology"`
	IndependentVariables []string        `json:"independent_variables"`
	DependentVariables   []string        `json:"dependent_variables"`
	ControlledVariables  []string        `json:"controlled_variables"`
	Assumptions          []string        `json:"assumptions"`
	Limitations          []string        `json:"limitations"`
	Parameters           json.RawMessage `json:"parameters"`
	TimeoutSeconds       int             `json:"timeout_seconds"`
	Model                string          `json:"model,omitempty"`
	Source               string          `json:"source"`
}
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Series struct {
	Name   string  `json:"name"`
	Points []Point `json:"points"`
}
type Result struct {
	Language        string            `json:"language,omitempty"`
	RuntimeVersion  string            `json:"runtime_version,omitempty"`
	XAxisLabel      string            `json:"x_axis_label,omitempty"`
	YAxisLabel      string            `json:"y_axis_label,omitempty"`
	Kind            string            `json:"kind"`
	Metrics         map[string]any    `json:"metrics"`
	Findings        []string          `json:"findings"`
	Limitations     []string          `json:"limitations"`
	Series          []Series          `json:"series,omitempty"`
	CSV             string            `json:"csv,omitempty"`
	PythonVersion   string            `json:"python_version"`
	Dependencies    map[string]string `json:"dependencies"`
	Narrative       string            `json:"narrative,omitempty"`
	NarrativeSource string            `json:"narrative_source"`
	Warnings        []string          `json:"warnings"`
}
type Event struct {
	ID           int64     `json:"id"`
	ExperimentID string    `json:"experiment_id"`
	Kind         string    `json:"kind"`
	Message      string    `json:"message"`
	CreatedAt    time.Time `json:"created_at"`
}
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}
type Dataset struct {
	SourceURL string     `json:"source_url,omitempty"`
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Rows      int        `json:"rows"`
	Columns   []string   `json:"columns"`
	Preview   [][]string `json:"preview"`
	SHA256    string     `json:"sha256"`
	Size      int64      `json:"size"`
	CreatedAt time.Time  `json:"created_at"`
}
type Artifact struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

func newID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b)
}
func terminal(s string) bool {
	return s == "completed" || s == "failed" || s == "cancelled" || s == "needs_review"
}
