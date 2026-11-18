package threadpostgres

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/jonathanagustin/conduit/internal/assistant/adapters/threadtest"
	"github.com/jonathanagustin/conduit/internal/assistant/application"
	"github.com/jonathanagustin/conduit/internal/assistant/domain"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
	"github.com/jonathanagustin/conduit/internal/pgtest"
)

// freshDSN returns a throwaway database, or skips without a server.
func freshDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(pgtest.DataSourceNameEnvironment)
	if dsn == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is not set")
	}
	shared, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shared.Close() })
	fresh, drop, err := pgtest.Fresh(context.Background(), shared, dsn, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	return fresh
}

func TestPostgresStoreConformsToTheThreadsPort(t *testing.T) {
	if os.Getenv(pgtest.DataSourceNameEnvironment) == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is not set")
	}
	threadtest.Run(t, func(t *testing.T) application.Threads {
		store, err := New(context.Background(), freshDSN(t))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		return store
	})
}

// A thread written through one pool is read through another, which is what a
// second replica or a second device is.
func TestPostgresThreadsAreSharedBetweenPoolsAndSurviveReopen(t *testing.T) {
	dsn := freshDSN(t)
	ctx := context.Background()
	first, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	id := domain.ThreadID("0123456789abcdef0123456789abcdef")
	if err := first.Create(ctx, domain.Thread{ID: id, Owner: "ada"}); err != nil {
		t.Fatal(err)
	}
	if err := first.Append(ctx, "ada", id, []domain.Message{{Role: domain.RoleUser, Text: "kept"}}, "kept"); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	got, err := second.Get(ctx, "ada", id)
	if err != nil || len(got.Messages) != 1 || got.Messages[0].Text != "kept" || got.Title != "kept" {
		t.Fatalf("reopened = %#v %v", got, err)
	}
	pending, err := pgmigrate.Pending(ctx, second.db, Component, Migrations())
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after apply = %v %v", pending, err)
	}
}

// The database refuses what the adapter refuses, so a second writer that
// skipped the adapter's checks still cannot store past a bound.
func TestPostgresSchemaEnforcesTheBounds(t *testing.T) {
	ctx := context.Background()
	store, err := New(ctx, freshDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := domain.ThreadID("0123456789abcdef0123456789abcdef")
	if err := store.Create(ctx, domain.Thread{ID: id, Owner: "ada"}); err != nil {
		t.Fatal(err)
	}
	for name, statement := range map[string]string{
		"message bytes":   `INSERT INTO conduit_assistant_thread_messages (owner, thread_id, position, document) VALUES ('ada', $1, 0, convert_to(repeat('x', 65537), 'UTF8'))`,
		"position":        `INSERT INTO conduit_assistant_thread_messages (owner, thread_id, position, document) VALUES ('ada', $1, 200, '\x7b7d')`,
		"message count":   `UPDATE conduit_assistant_threads SET message_count = 201 WHERE owner = 'ada' AND thread_id = $1`,
		"title":           `UPDATE conduit_assistant_threads SET title = repeat('t', 121) WHERE owner = 'ada' AND thread_id = $1`,
		"orphan message":  `INSERT INTO conduit_assistant_thread_messages (owner, thread_id, position, document) VALUES ('grace', $1, 0, '\x7b7d')`,
		"duplicate place": `INSERT INTO conduit_assistant_thread_messages (owner, thread_id, position, document) VALUES ('ada', $1, 0, '\x7b7d'), ('ada', $1, 0, '\x7b7d')`,
	} {
		if _, err := store.db.ExecContext(ctx, statement, id); err == nil {
			t.Fatalf("%s: the schema accepted it", name)
		}
	}
}

func TestPostgresForkMessageFailureRollsBackWholeCopy(t *testing.T) {
	ctx := t.Context()
	store, err := New(ctx, freshDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := domain.Thread{ID: domain.ThreadID("11111111111111111111111111111111"), Owner: "ada", Title: "original"}
	if err := store.Create(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ctx, "ada", source.ID, []domain.Message{{Role: domain.RoleUser, Text: "question"}, {Role: domain.RoleAssistant, Text: "answer"}}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `CREATE FUNCTION refuse_fork_message() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.position=1 THEN RAISE EXCEPTION 'synthetic fork refusal'; END IF; RETURN NEW; END $$; CREATE TRIGGER refuse_fork_message BEFORE INSERT ON conduit_assistant_thread_messages FOR EACH ROW EXECUTE FUNCTION refuse_fork_message()`); err != nil {
		t.Fatal(err)
	}
	target := domain.Thread{ID: domain.ThreadID("22222222222222222222222222222222"), Owner: "ada"}
	if _, err := store.Fork(ctx, "ada", source.ID, target); err == nil {
		t.Fatal("failed copy acknowledged")
	}
	rows, err := store.List(ctx, "ada")
	if err != nil || len(rows) != 1 || rows[0].ID != source.ID {
		t.Fatal("partial fork header survived", rows, err)
	}
	original, err := store.Get(ctx, "ada", source.ID)
	if err != nil || len(original.Messages) != 2 {
		t.Fatal("failed fork changed source", original, err)
	}
}
