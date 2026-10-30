package research

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
)

// Every database test gets its own disposable schema, never the app's tables.
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	value := os.Getenv("TEST_DATABASE_URL")
	if value == "" {
		values, _ := godotenv.Read("../../../.env")
		value = values["TEST_DATABASE_URL"]
	}
	if value == "" {
		t.Skip("PostgreSQL tests need TEST_DATABASE_URL (use a separate test database or Neon branch)")
	}
	if err := validateDatabaseURL(value); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", value)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	schema := newID("test")
	if _, err = db.Exec("CREATE SCHEMA " + schema); err != nil {
		db.Close()
		t.Fatal("cannot create test schema; check TEST_DATABASE_URL and test database schema permissions")
	}
	config, err := pgx.ParseConfig(value)
	if err != nil {
		db.Close()
		t.Fatal("invalid test database configuration")
	}
	// Neon does not reliably apply a search_path startup parameter. Select the
	// schema after every physical connection opens, before it enters the pool.
	config.AfterConnect = func(ctx context.Context, conn *pgconn.PgConn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize()).ReadAll()
		return err
	}
	registered := stdlib.RegisterConnConfig(config)
	t.Cleanup(func() {
		if _, err := db.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
		db.Close()
		stdlib.UnregisterConnConfig(registered)
	})
	return registered
}

func TestPostgresMutationsSerialize(t *testing.T) {
	store, err := OpenStore(testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	if err := store.Create(Experiment{ID: "concurrent", Status: "draft", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Mutate("concurrent", "increment", "incremented", func(e *Experiment) error { e.Attempt++; return nil })
			if err != nil {
				failures <- err
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	e, err := store.Get("concurrent")
	if err != nil || e.Attempt != 20 {
		t.Fatalf("concurrent updates lost: attempt=%d, err=%v", e.Attempt, err)
	}
	events, err := store.Events(e.ID, 0, 100)
	if err != nil || len(events) != 21 {
		t.Fatalf("expected 21 committed events, got %d: %v", len(events), err)
	}
	if err = store.Delete(e.ID); err != nil {
		t.Fatal(err)
	}
	events, err = store.Events(e.ID, 0, 100)
	if err != nil || len(events) != 0 {
		t.Fatalf("event cascade failed: %v", err)
	}
}
