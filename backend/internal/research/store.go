package research

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct{ db *sql.DB }

// OpenStore initializes a database using one direct connection URL.
func OpenStore(databaseURL string) (*Store, error) {
	return OpenStoreWithMigrationURL(databaseURL, databaseURL)
}

// OpenStoreWithMigrationURL initializes the schema over a direct connection,
// then uses the pooled connection for the application's normal queries.
func OpenStoreWithMigrationURL(databaseURL, migrationURL string) (*Store, error) {
	if migrationURL == "" {
		migrationURL = databaseURL
	}
	migrationDB, err := connectPostgres(migrationURL)
	if err != nil {
		return nil, err
	}
	if err = initializeSchema(migrationDB); err != nil {
		migrationDB.Close()
		return nil, err
	}
	if migrationURL == databaseURL {
		return &Store{migrationDB}, nil
	}
	migrationDB.Close()
	db, err := connectPostgres(databaseURL)
	if err != nil {
		return nil, err
	}
	return &Store{db}, nil
}

func connectPostgres(databaseURL string) (*sql.DB, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid PostgreSQL connection URL")
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(time.Minute)
	db.SetConnMaxLifetime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("cannot connect to PostgreSQL; check DATABASE_URL, DATABASE_URL_UNPOOLED, SSL settings, and database access")
	}
	return db, nil
}

func initializeSchema(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Serialize schema initialization when connections open simultaneously.
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(728104923)"); err == nil {
		for _, statement := range strings.Split(storeSchema, ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err = tx.ExecContext(ctx, statement); err != nil {
				break
			}
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return fmt.Errorf("initialize PostgreSQL schema: %w", err)
	}
	return nil
}

//go:embed schema.sql
var storeSchema string

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Create(e Experiment) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO experiments VALUES($1,$2,$3,$4)", e.ID, e.Status, e.UpdatedAt.Format(time.RFC3339Nano), b); err != nil {
		return err
	}
	if err = insertEvent(tx, e.ID, "created", "Experiment created"); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Get(id string) (Experiment, error) {
	var b []byte
	var e Experiment
	err := s.db.QueryRow("SELECT payload FROM experiments WHERE id=$1", id).Scan(&b)
	if err == sql.ErrNoRows {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	err = json.Unmarshal(b, &e)
	return e, err
}
func (s *Store) List(status, query, project string, limit, offset int) ([]Experiment, int, error) {
	rows, err := s.db.Query("SELECT payload FROM experiments ORDER BY updated_at DESC,id DESC")
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	all := []Experiment{}
	for rows.Next() {
		var b []byte
		var e Experiment
		if err = rows.Scan(&b); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal(b, &e); err != nil {
			return nil, 0, err
		}
		if status != "" && status != e.Status || project != "" && project != e.ProjectID || query != "" && !strings.Contains(strings.ToLower(e.Title+" "+e.Question+" "+e.ID), strings.ToLower(query)) {
			continue
		}
		all = append(all, e)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	total := len(all)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}
func insertEvent(tx *sql.Tx, id, kind, message string) error {
	_, err := tx.Exec("INSERT INTO events(experiment_id,kind,message,created_at) VALUES($1,$2,$3,$4)", id, kind, message, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func (s *Store) Mutate(id, kind, message string, fn func(*Experiment) error) (Experiment, error) {
	var e Experiment
	tx, err := s.db.Begin()
	if err != nil {
		return e, err
	}
	defer tx.Rollback()
	var b []byte
	if err = tx.QueryRow("SELECT payload FROM experiments WHERE id=$1 FOR UPDATE", id).Scan(&b); err == sql.ErrNoRows {
		return e, ErrNotFound
	} else if err != nil {
		return e, err
	}
	if err = json.Unmarshal(b, &e); err != nil {
		return e, err
	}
	oldVersion := e.PlanVersion
	if err = fn(&e); err != nil {
		return e, err
	}
	if e.Plan != nil && e.PlanVersion > oldVersion {
		planBytes, marshalErr := json.Marshal(e.Plan)
		if marshalErr != nil {
			return e, marshalErr
		}
		if _, err = tx.Exec("INSERT INTO plan_versions VALUES($1,$2,$3)", id, e.PlanVersion, planBytes); err != nil {
			return e, err
		}
	}
	e.UpdatedAt = time.Now().UTC()
	b, err = json.Marshal(e)
	if err != nil {
		return e, err
	}
	if _, err = tx.Exec("UPDATE experiments SET status=$1,updated_at=$2,payload=$3 WHERE id=$4", e.Status, e.UpdatedAt.Format(time.RFC3339Nano), b, id); err != nil {
		return e, err
	}
	if kind != "" {
		if err = insertEvent(tx, id, kind, message); err != nil {
			return e, err
		}
	}
	return e, tx.Commit()
}
func (s *Store) Event(id, kind, message string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = insertEvent(tx, id, kind, message); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Events(id string, after int64, limit int) ([]Event, error) {
	rows, err := s.db.Query("SELECT id,experiment_id,kind,message,created_at FROM events WHERE experiment_id=$1 AND id>$2 ORDER BY id LIMIT $3", id, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var t string
		if err = rows.Scan(&e.ID, &e.ExperimentID, &e.Kind, &e.Message, &t); err != nil {
			return nil, err
		}
		e.CreatedAt, err = time.Parse(time.RFC3339Nano, t)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) Delete(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRow("SELECT status FROM experiments WHERE id=$1 FOR UPDATE", id).Scan(&status); err == sql.ErrNoRows {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if status == "queued" || status == "running" || status == "planning" {
		return ErrConflict
	}
	if _, err = tx.Exec("DELETE FROM experiments WHERE id=$1", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Recover() error {
	runs, _, err := s.List("", "", "", 1<<30, 0)
	if err != nil {
		return err
	}
	for _, e := range runs {
		if e.Status == "running" || e.Status == "planning" {
			_, err = s.Mutate(e.ID, "interrupted", "Service restarted during operation", func(e *Experiment) error {
				e.Status = "failed"
				e.Error = "Service restarted; review the plan and retry explicitly"
				return nil
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Store) SaveProject(p Project) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO projects VALUES($1,$2)", p.ID, b)
	return err
}
func (s *Store) Projects() ([]Project, error) {
	rows, err := s.db.Query("SELECT payload FROM projects ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var b []byte
		var p Project
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) ProjectExists(id string) bool {
	var n int
	return s.db.QueryRow("SELECT 1 FROM projects WHERE id=$1", id).Scan(&n) == nil
}
func (s *Store) SaveDataset(d Dataset) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO datasets VALUES($1,$2)", d.ID, b)
	return err
}
func (s *Store) Dataset(id string) (Dataset, error) {
	var d Dataset
	var b []byte
	err := s.db.QueryRow("SELECT payload FROM datasets WHERE id=$1", id).Scan(&b)
	if err == sql.ErrNoRows {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	err = json.Unmarshal(b, &d)
	return d, err
}
func (s *Store) Datasets() ([]Dataset, error) {
	rows, err := s.db.Query("SELECT payload FROM datasets ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Dataset{}
	for rows.Next() {
		var d Dataset
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Store) SaveArtifacts(id string, as []Artifact) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, a := range as {
		b, err := json.Marshal(a)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO artifacts VALUES($1,$2,$3)", id, a.Name, b); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) Artifacts(id string) ([]Artifact, error) {
	rows, err := s.db.Query("SELECT payload FROM artifacts WHERE experiment_id=$1 ORDER BY name", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Artifact{}
	for rows.Next() {
		var a Artifact
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func stateError(want string) error { return fmt.Errorf("%w: %s", ErrConflict, want) }

func (s *Store) PlanHistory(id string) ([]map[string]any, error) {
	rows, err := s.db.Query("SELECT version,payload FROM plan_versions WHERE experiment_id=$1 ORDER BY version", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var version int
		var b []byte
		if err = rows.Scan(&version, &b); err != nil {
			return nil, err
		}
		var p Plan
		if err = json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"version": version, "plan": p})
	}
	return out, rows.Err()
}
