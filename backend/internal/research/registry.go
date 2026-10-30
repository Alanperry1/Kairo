package research

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"strings"
)

type Tool struct {
	Category          string   `json:"category"`
	AvailableModules  []string `json:"available_modules,omitempty"`
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Domain            string   `json:"domain"`
	Description       string   `json:"description"`
	Available         bool     `json:"available"`
	UnavailableReason string   `json:"unavailable_reason,omitempty"`
	Dependencies      []string `json:"dependencies"`
	Outputs           []string `json:"outputs"`
	ExampleParameters any      `json:"example_parameters"`
}
type Registry struct{ tools []Tool }
type GeneralParams struct {
	Inputs          map[string]any `json:"inputs"`
	Seed            int64          `json:"seed"`
	RequiredModules []string       `json:"required_modules"`
}

var moduleName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
var standardModules = []string{"math", "statistics", "random", "csv", "json", "itertools", "collections", "decimal", "fractions", "datetime", "array", "cmath", "functools", "heapq", "bisect", "re", "time"}

type PhysicsParams struct {
	Velocity float64   `json:"velocity_m_s"`
	Angle    float64   `json:"angle_degrees"`
	Gravity  float64   `json:"gravity_m_s2"`
	Mass     float64   `json:"mass_kg"`
	Area     float64   `json:"area_m2"`
	Density  float64   `json:"air_density_kg_m3"`
	Drag     []float64 `json:"drag_coefficients"`
	TimeStep float64   `json:"time_step_s"`
	MaxTime  float64   `json:"max_time_s"`
}
type BenchmarkParams struct {
	Sizes  []int `json:"sizes"`
	Trials int   `json:"trials"`
	Seed   int64 `json:"seed"`
}
type StatisticsParams struct {
	GroupColumn string  `json:"group_column"`
	ValueColumn string  `json:"value_column"`
	GroupA      string  `json:"group_a"`
	GroupB      string  `json:"group_b"`
	Alpha       float64 `json:"alpha"`
}

func NewRegistry(python, mode string, scipyAvailable bool) *Registry {
	_, err := exec.LookPath(python)
	available := err == nil
	reason := ""
	if !available {
		reason = "Python executable not found"
	}
	if mode == "docker" {
		_, err = exec.LookPath("docker")
		available = err == nil
		reason = "Docker executable not found"
		if available {
			reason = ""
		}
	}
	tools := []Tool{
		{ID: "physics.projectile", Name: "Projectile simulation", Domain: "physics", Description: "RK4 integration with quadratic air drag and a no-drag control. Numerical, not a physical experiment.", Available: available, UnavailableReason: reason, Dependencies: []string{"Python 3.10+ standard library"}, Outputs: []string{"results.json", "data.csv", "chart.svg", "report.md"}, ExampleParameters: PhysicsParams{30, 45, 9.81, 1, .01, 1.225, []float64{0, .2, .5}, .01, 30}},
		{ID: "cs.sorting", Name: "Sorting benchmark", Domain: "computer_science", Description: "Paired randomized workloads comparing insertion sort, merge sort, and Python built-in sort.", Available: available, UnavailableReason: reason, Dependencies: []string{"Python 3.10+ standard library"}, Outputs: []string{"results.json", "data.csv", "chart.svg", "report.md"}, ExampleParameters: BenchmarkParams{[]int{100, 500, 1000}, 5, 42}},
		{ID: "statistics.welch", Name: "Two-group comparison", Domain: "statistics", Description: "Pre-specified two-sided Welch t-test, raw mean difference and 95% confidence interval. Requires independent observations and explicit group/value columns.", Available: available && scipyAvailable, Dependencies: []string{"Python 3.10+", "SciPy"}, Outputs: []string{"results.json", "data.csv", "chart.svg", "report.md"}, ExampleParameters: StatisticsParams{"group", "value", "A", "B", .05}},
	}
	if !tools[2].Available {
		tools[2].UnavailableReason = "SciPy is not installed in the selected worker; this adapter is unavailable"
	}
	for i := range tools {
		tools[i].Category = "example"
	}
	general := Tool{ID: "python.experiment", Name: "General computational experiments", Domain: "any", Category: "capability", Description: "Custom Python simulations, analyses and benchmarks across scientific domains. Plans define their own method, code, inputs and result schema. Execution requires an isolated Docker worker and installed dependencies.", Available: available && mode == "docker", Dependencies: []string{"Python 3.10+", "Isolated Docker worker"}, Outputs: []string{"experiment.py", "results.json", "data.csv", "chart.svg", "report.md"}, ExampleParameters: GeneralParams{Inputs: map[string]any{}, Seed: 42, RequiredModules: []string{}}, AvailableModules: append([]string{}, standardModules...)}
	if scipyAvailable {
		general.AvailableModules = append(general.AvailableModules, "numpy", "scipy")
	}
	if !general.Available {
		general.UnavailableReason = "Custom code execution requires WORKER_MODE=docker and a locally built worker image; plans can still be created and reviewed"
	}
	tools = append([]Tool{general}, tools...)
	return &Registry{tools}
}
func (r *Registry) Tools() []Tool { return r.tools }
func (r *Registry) Lookup(id string) (Tool, bool) {
	for _, t := range r.tools {
		if t.ID == id {
			return t, true
		}
	}
	return Tool{}, false
}
func strictJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected a single JSON value")
	}
	return nil
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func (r *Registry) Validate(p Plan, requireAvailable bool) error {
	t, ok := r.Lookup(p.Adapter)
	if !ok {
		return fmt.Errorf("unsupported adapter %q", p.Adapter)
	}
	if requireAvailable && !t.Available {
		return fmt.Errorf("adapter unavailable: %s", t.UnavailableReason)
	}
	if p.Adapter != "python.experiment" && p.Domain != t.Domain {
		return fmt.Errorf("domain must be %q for this adapter", t.Domain)
	}
	if strings.TrimSpace(p.ResearchQuestion) == "" || strings.TrimSpace(p.Hypothesis) == "" || strings.TrimSpace(p.Methodology) == "" {
		return fmt.Errorf("research_question, hypothesis, and methodology are required")
	}
	if len(p.ResearchQuestion) > 4000 || len(p.Hypothesis) > 4000 || len(p.Methodology) > 8000 || len(p.Assumptions) > 30 || len(p.Limitations) > 30 {
		return fmt.Errorf("plan text exceeds limits")
	}
	if len(p.Assumptions) == 0 || len(p.Limitations) == 0 || len(p.IndependentVariables) == 0 || len(p.DependentVariables) == 0 {
		return fmt.Errorf("explicit assumptions, limitations, independent_variables and dependent_variables are required")
	}
	if p.TimeoutSeconds < 1 || p.TimeoutSeconds > 300 {
		return fmt.Errorf("timeout_seconds must be between 1 and 300")
	}
	if strings.TrimSpace(p.Domain) == "" || len(p.Domain) > 80 {
		return fmt.Errorf("domain is required (max 80 characters)")
	}
	if p.Adapter != "python.experiment" && p.Code != "" {
		return fmt.Errorf("custom code requires the python.experiment adapter")
	}
	switch p.Adapter {
	case "python.experiment":
		var c GeneralParams
		if err := strictJSON(p.Parameters, &c); err != nil {
			return fmt.Errorf("general experiment parameters: %w", err)
		}
		if c.Inputs == nil || c.Seed < 0 || c.Seed > 9007199254740991 || len(c.RequiredModules) > 32 {
			return fmt.Errorf("general parameters require inputs (object), a nonnegative safe-integer seed, and up to 32 required_modules")
		}
		if strings.TrimSpace(p.Code) == "" || len(p.Code) > 64000 || strings.ContainsRune(p.Code, 0) {
			return fmt.Errorf("general experiments require Python code (1–64000 bytes) defining run_experiment(inputs, dataset_rows)")
		}
		for _, name := range c.RequiredModules {
			if !moduleName.MatchString(name) {
				return fmt.Errorf("invalid module name %q", name)
			}
			found := false
			for _, installed := range t.AvailableModules {
				if name == installed {
					found = true
					break
				}
			}
			if requireAvailable && !found {
				return fmt.Errorf("required module %q is not available in the worker; install it in the worker image and restart the server", name)
			}
		}

	case "physics.projectile":
		var c PhysicsParams
		if err := strictJSON(p.Parameters, &c); err != nil {
			return fmt.Errorf("physics parameters: %w", err)
		}
		if c.Velocity <= 0 || c.Velocity > 300 || c.Angle <= 0 || c.Angle >= 90 || c.Gravity <= 0 || c.Gravity > 30 || c.Mass < .01 || c.Mass > 1000 || c.Area <= 0 || c.Area > 10 || c.Density < 0 || c.Density > 5 || c.TimeStep < .001 || c.TimeStep > .1 || c.MaxTime <= 0 || c.MaxTime > 120 {
			return fmt.Errorf("physics parameters out of bounds; see example and API documentation")
		}
		if len(c.Drag) < 2 || len(c.Drag) > 8 {
			return fmt.Errorf("provide 2–8 drag coefficients including a zero-drag control")
		}
		seen := map[float64]bool{}
		for _, v := range c.Drag {
			if v < 0 || v > 2 || !finite(v) || seen[v] {
				return fmt.Errorf("drag coefficients must be distinct numbers in [0,2]")
			}
			seen[v] = true
		}
		if !seen[0] {
			return fmt.Errorf("include a zero-drag control")
		}
		flight := 2 * c.Velocity * math.Sin(c.Angle*math.Pi/180) / c.Gravity
		if c.TimeStep > flight/20 {
			return fmt.Errorf("time_step_s must resolve at least 20 steps of the no-drag flight")
		}
		for _, cd := range c.Drag {
			if c.TimeStep*.5*c.Density*cd*c.Area/c.Mass*c.Velocity > .25 {
				return fmt.Errorf("time_step_s is too large for the specified drag stiffness")
			}
		}
		if c.MaxTime/c.TimeStep*float64(len(c.Drag)) > 500000 {
			return fmt.Errorf("simulation exceeds 500000 integration steps")
		}
	case "cs.sorting":
		var c BenchmarkParams
		if err := strictJSON(p.Parameters, &c); err != nil {
			return err
		}
		if len(c.Sizes) < 2 || len(c.Sizes) > 8 || c.Trials < 3 || c.Trials > 20 || c.Seed < 0 || c.Seed > 9007199254740991 {
			return fmt.Errorf("benchmark requires 2–8 sizes, 3–20 trials and a nonnegative safe-integer seed")
		}
		work := 0
		seen := map[int]bool{}
		for _, n := range c.Sizes {
			if n < 10 || n > 5000 || seen[n] {
				return fmt.Errorf("sizes must be distinct integers from 10 to 5000")
			}
			seen[n] = true
			work += n * n * c.Trials
		}
		if work > 200000000 {
			return fmt.Errorf("benchmark exceeds quadratic-work budget")
		}
	case "statistics.welch":
		var c StatisticsParams
		if err := strictJSON(p.Parameters, &c); err != nil {
			return err
		}
		if c.GroupColumn == "" || c.ValueColumn == "" || c.GroupColumn == c.ValueColumn || c.GroupA == "" || c.GroupB == "" || c.GroupA == c.GroupB || c.Alpha != .05 {
			return fmt.Errorf("provide distinct columns, distinct group names, and alpha=0.05 (pre-specified 95%% interval)")
		}
	}
	return nil
}

// SetWorkerAvailability records a startup probe; it is not a promise that a worker
// cannot fail later. Runtime errors are always preserved on the experiment.
func (r *Registry) SetWorkerAvailability(available bool, reason string) {
	if available {
		return
	}
	for i := range r.tools {
		r.tools[i].Available = false
		r.tools[i].UnavailableReason = reason
	}
}

func (r *Registry) SetAvailableModules(modules []string) {
	for i := range r.tools {
		if r.tools[i].ID == "python.experiment" {
			r.tools[i].AvailableModules = append(append([]string{}, standardModules...), modules...)
		}
	}
}
