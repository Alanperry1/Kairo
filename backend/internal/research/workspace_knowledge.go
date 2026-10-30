package research

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

// These references identify registered workspace records, never arbitrary paths
// or URLs. Research file contents stay in the configured object storage.
type WorkspaceSource struct {
	Key          string `json:"key"`
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	Title        string `json:"title"`
	ExperimentID string `json:"experiment_id,omitempty"`
}
type WorkspaceRead struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type WorkspaceCatalog struct {
	Sources []WorkspaceSource `json:"sources"`
	Total   int               `json:"total_matches"`
	Offset  int               `json:"offset"`
	Scope   string            `json:"scope"`
}

var workspaceWords = regexp.MustCompile(`[\pL\pN_]+`)

func workspaceTerms(query string) string {
	words := workspaceWords.FindAllString(strings.ToLower(query), 32)
	return strings.Join(words, " OR ")
}

// Rank in Postgres and return only a small catalog, rather than transferring
// every experiment payload (including large result series) to the model server.
func (s *Store) searchWorkspace(ctx context.Context, query string, offset int) (WorkspaceCatalog, error) {
	const sql = `WITH resources AS (
 SELECT 'experiment' AS kind,id,coalesce(p->>'title',id) AS title,id AS experiment_id,updated_at AS updated,
 id||' '||coalesce(p->>'project_id','')||' '||coalesce(p->>'dataset_id','')||' '||coalesce(p->>'status','')||' '||coalesce(p->>'title','')||' '||coalesce(p->>'question','')||' '||coalesce(p->>'plan','')||' '||coalesce(p->>'result','')||' '||coalesce(p->>'error','') AS body
 FROM (SELECT id,updated_at,convert_from(payload,'UTF8')::jsonb AS p FROM experiments) e
 UNION ALL SELECT 'project',id,coalesce(p->>'name',id),'',coalesce(p->>'created_at',''),id||' '||p::text FROM (SELECT id,convert_from(payload,'UTF8')::jsonb AS p FROM projects) p
 UNION ALL SELECT 'dataset',id,coalesce(p->>'name',id),'',coalesce(p->>'created_at',''),id||' '||p::text FROM (SELECT id,convert_from(payload,'UTF8')::jsonb AS p FROM datasets) d
 UNION ALL SELECT kind,id,title,experiment_id,created_at,id||' '||title||' '||experiment_id||' '||kind FROM research_documents
 UNION ALL SELECT 'activity',id::text,kind||': '||left(message,160),experiment_id,created_at,experiment_id||' '||kind||' '||message FROM events
 UNION ALL SELECT 'plan',experiment_id||'/'||version::text,'Plan v'||version::text,experiment_id,'',experiment_id||' '||convert_from(payload,'UTF8') FROM plan_versions
 UNION ALL SELECT 'artifact',experiment_id||'/'||name,name,experiment_id,'',experiment_id||' '||name FROM artifacts
 ), ranked AS (
 SELECT *,row_number() OVER(PARTITION BY kind ORDER BY ts_rank_cd(to_tsvector('english',body),websearch_to_tsquery('english',$1)) DESC,updated DESC,id) AS n
 FROM resources WHERE $1='' OR strpos(lower(body),lower($2))>0 OR to_tsvector('english',body) @@ websearch_to_tsquery('english',$1)
 ) SELECT kind,id,title,experiment_id,count(*) OVER() FROM ranked ORDER BY n,kind LIMIT 20 OFFSET $3`
	rows, err := s.db.QueryContext(ctx, sql, workspaceTerms(query), query, offset)
	if err != nil {
		return WorkspaceCatalog{}, err
	}
	defer rows.Close()
	out := WorkspaceCatalog{Sources: []WorkspaceSource{}, Offset: offset, Scope: "Search covers all registered record metadata, current experiment methods/code/results, and artifact names. Document bodies are read on demand. Paginate with workspace_offset in increments of 20."}
	for rows.Next() {
		var v WorkspaceSource
		if err = rows.Scan(&v.Kind, &v.ID, &v.Title, &v.ExperimentID, &out.Total); err != nil {
			return out, err
		}
		v.Key = v.Kind + ":" + v.ID
		out.Sources = append(out.Sources, v)
	}
	return out, rows.Err()
}

// Keep snippets valid UTF-8 and explicitly disclose omissions. Nothing is
// silently presented as the full dataset, document, or execution history.
func workspaceSnippet(text, query string, limit int) map[string]any {
	if len(text) <= limit {
		return map[string]any{"text": text, "truncated": false}
	}
	start := 0
	lower := strings.ToLower(text)
	for _, word := range workspaceWords.FindAllString(strings.ToLower(query), 32) {
		if len(word) < 4 {
			continue
		}
		if at := strings.Index(lower, word); at >= 0 {
			start = min(len(text), max(0, at-limit/4))
			break
		}
	}
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	end := min(len(text), start+limit)
	for end < len(text) && end > start && !utf8.RuneStart(text[end]) {
		end--
	}
	return map[string]any{"text": text[start:end], "truncated": true, "total_bytes": len(text), "offset_bytes": start}
}
func workspaceValue(value any, query string, limit int) any {
	raw, _ := json.Marshal(value)
	if len(raw) <= limit {
		return value
	}
	return workspaceSnippet(string(raw), query, limit)
}
func (s *Server) readWorkspace(ctx context.Context, ref WorkspaceRead, query string) (WorkspaceSource, any, error) {
	source := WorkspaceSource{Kind: ref.Kind, ID: ref.ID, Key: ref.Kind + ":" + ref.ID}
	if s.Store == nil {
		return source, nil, errors.New("workspace is unavailable")
	}
	switch ref.Kind {
	case "experiment":
		e, err := s.Store.Get(ref.ID)
		if err != nil {
			return source, nil, err
		}
		source.Title = e.Title
		source.ExperimentID = e.ID
		evidence := map[string]any{"id": e.ID, "title": e.Title, "question": e.Question, "status": e.Status, "error": e.Error, "project_id": e.ProjectID, "dataset_id": e.DatasetID, "attempt": e.Attempt, "plan_version": e.PlanVersion, "updated_at": e.UpdatedAt}
		if e.Plan != nil {
			p := *e.Plan
			p.Code = ""
			evidence["plan"] = workspaceValue(p, query, 6000)
			evidence["code"] = workspaceSnippet(e.Plan.Code, query, 9000)
		}
		if e.Result != nil {
			evidence["metrics"] = workspaceValue(documentMetrics(e.Result.Metrics), query, 5000)
			evidence["findings"] = workspaceValue(e.Result.Findings, query, 4000)
			evidence["limitations"] = workspaceValue(e.Result.Limitations, query, 3000)
			evidence["narrative"] = workspaceSnippet(e.Result.Narrative, query, 4000)
		}
		events, err := s.Store.Events(e.ID, 0, 200)
		if err != nil {
			return source, nil, err
		}
		evidence["activity"] = workspaceValue(events, query, 4000)
		evidence["activity_scope"] = "At most the first 200 saved events; excerpts may be truncated."
		return source, evidence, nil
	case "dataset":
		d, err := s.Store.Dataset(ref.ID)
		source.Title = d.Name
		return source, map[string]any{"metadata": d, "scope": "Metadata and five preview rows only. Full rows require an experiment; do not infer full-dataset statistics from the preview."}, err
	case "project":
		var raw []byte
		err := s.Store.db.QueryRowContext(ctx, "SELECT payload FROM projects WHERE id=$1", ref.ID).Scan(&raw)
		if err != nil {
			return source, nil, err
		}
		var p Project
		if err = json.Unmarshal(raw, &p); err != nil {
			return source, nil, err
		}
		source.Title = p.Name
		return source, p, nil
	case "report", "manuscript":
		d, err := s.loadDocument(ctx, ref.ID)
		if err != nil {
			return source, nil, err
		}
		if d.Kind != ref.Kind {
			return source, nil, ErrNotFound
		}
		source.Title = d.Title
		source.ExperimentID = d.ExperimentID
		return source, map[string]any{"id": d.ID, "kind": d.Kind, "title": d.Title, "experiment_id": d.ExperimentID, "attempt": d.Attempt, "plan_version": d.PlanVersion, "content": workspaceSnippet(d.Content, query, 18000), "evidence_sha256": d.EvidenceSHA256, "scope": "Author-review draft, not independent scientific validation."}, nil
	case "activity":
		var event Event
		var created string
		err := s.Store.db.QueryRowContext(ctx, "SELECT id,experiment_id,kind,message,created_at FROM events WHERE id=$1", ref.ID).Scan(&event.ID, &event.ExperimentID, &event.Kind, &event.Message, &created)
		source.Title = event.Kind + ": " + event.Message
		source.ExperimentID = event.ExperimentID
		return source, map[string]any{"id": event.ID, "kind": event.Kind, "message": event.Message, "experiment_id": event.ExperimentID, "created_at": created}, err
	case "plan":
		experiment, version, ok := strings.Cut(ref.ID, "/")
		if !ok {
			return source, nil, ErrNotFound
		}
		var raw []byte
		err := s.Store.db.QueryRowContext(ctx, "SELECT payload FROM plan_versions WHERE experiment_id=$1 AND version=$2", experiment, version).Scan(&raw)
		if err != nil {
			return source, nil, err
		}
		var p Plan
		if err = json.Unmarshal(raw, &p); err != nil {
			return source, nil, err
		}
		source.Title = "Plan v" + version
		source.ExperimentID = experiment
		code := p.Code
		p.Code = ""
		return source, map[string]any{"plan": workspaceValue(p, query, 6000), "code": workspaceSnippet(code, query, 12000), "version": version, "scope": "Saved historical plan; may differ from the current method."}, nil

	case "artifact":
		experiment, name, ok := strings.Cut(ref.ID, "/")
		if !ok || !validObjectKey(name) {
			return source, nil, ErrNotFound
		}
		artifacts, err := s.Store.Artifacts(experiment)
		if err != nil {
			return source, nil, err
		}
		for _, a := range artifacts {
			if a.Name != name {
				continue
			}
			source.Title = a.Name
			source.ExperimentID = experiment
			ext := strings.ToLower(path.Ext(name))
			if a.Size > 1<<20 || !strings.Contains(" .txt .md .json .py .js .r .go .cpp .tex .csv .jl .rs .rb ", " "+ext+" ") {
				return source, map[string]any{"metadata": a, "content_available": false, "reason": "Only registered text artifacts up to 1 MiB are readable in chat."}, nil
			}
			raw, err := readObject(ctx, s.objectStorage(), "experiments/"+experiment+"/"+name, 1<<20)
			if err != nil {
				return source, nil, err
			}
			hash := sha256.Sum256(raw)
			if hex.EncodeToString(hash[:]) != a.SHA256 {
				return source, nil, errors.New("artifact integrity check failed")
			}
			return source, map[string]any{"metadata": a, "content": workspaceSnippet(string(raw), query, 18000)}, nil
		}
		return source, nil, ErrNotFound
	default:
		return source, nil, fmt.Errorf("unsupported workspace resource kind")
	}
}

func (s *Store) workspaceOverview(ctx context.Context) (json.RawMessage, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT jsonb_build_object(
 'experiments',(SELECT count(*) FROM experiments),
 'projects',(SELECT count(*) FROM projects),
 'datasets',(SELECT count(*) FROM datasets),
 'reports',(SELECT count(*) FROM research_documents WHERE kind='report'),
 'manuscripts',(SELECT count(*) FROM research_documents WHERE kind='manuscript'),
 'experiment_statuses',coalesce((SELECT jsonb_object_agg(status,n) FROM (SELECT status,count(*) AS n FROM experiments GROUP BY status) counts),'{}'::jsonb))`).Scan(&raw)
	return json.RawMessage(raw), err
}
