package research

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type Language struct {
	ID        string   `json:"id"`
	Available bool     `json:"available"`
	Version   string   `json:"version,omitempty"`
	Modules   []string `json:"modules"`
	Libraries []string `json:"libraries"`
	Extension string   `json:"extension"`
}
type WorkerCapabilities struct {
	Modules   []string   `json:"modules"`
	Languages []Language `json:"languages"`
}

var languageName = regexp.MustCompile(`^[a-z][a-z0-9+#-]{0,39}$`)

func normalizeLanguage(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "automatic":
		return "auto"
	case "py":
		return "python"
	case "js", "node", "nodejs":
		return "javascript"
	case "c++", "cxx":
		return "cpp"
	case "golang":
		return "go"
	case "rb":
		return "ruby"
	case "rs":
		return "rust"
	case "jl":
		return "julia"
	}
	return s
}
func removedLanguage(s string) bool {
	switch normalizeLanguage(s) {
	case "rust", "ruby", "julia":
		return true
	}
	return false
}
func validLanguage(s string) bool { return languageName.MatchString(s) && !removedLanguage(s) }
func planLanguage(p Plan) string {
	if p.Adapter != "language.experiment" {
		return "python"
	}
	return normalizeLanguage(p.Language)
}
func validateLanguageChoice(preference string, p Plan) error {
	selected := normalizeLanguage(preference)
	if selected != "auto" && selected != planLanguage(p) {
		return fmt.Errorf("plan language must match the selected language %q", selected)
	}
	return nil
}
func sourceFilename(p Plan) string {
	if planLanguage(p) == "python" {
		return "experiment.py"
	}
	var catalog map[string]struct {
		Extension string `json:"extension"`
	}
	_ = json.Unmarshal([]byte(runtimeCatalog), &catalog)
	ext := catalog[planLanguage(p)].Extension
	if ext == "" {
		ext = "txt"
	}
	return "experiment." + ext
}
func WorkerProbeCode() string { return workerCode }
func (r *Registry) SetLanguages(languages []Language) {
	r.languages = nil
	for _, language := range languages {
		if !removedLanguage(language.ID) {
			r.languages = append(r.languages, language)
		}
	}
}
func (r *Registry) Languages() []Language {
	if len(r.languages) > 0 {
		return r.languages
	}
	t, _ := r.Lookup("python.experiment")
	return []Language{{ID: "python", Available: t.Available, Modules: t.AvailableModules, Extension: "py"}}
}
