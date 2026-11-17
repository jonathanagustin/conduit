// Package threadpostgres keeps assistant conversations in PostgreSQL, so a
// thread survives a restart and is the same on every replica and device.
package threadpostgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/jonathanagustin/conduit/internal/assistant/domain"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

// Component is this store's name in the migration ledger.
const Component = "assistant-threads"

// The CHECK bounds repeat domain.MaxMessagesPerThread (200),
// domain.MaxMessageBytes (65536) and domain.MaxThreadTitleBytes (120): a merged
// migration does not change, so a later change to a bound is a new migration.
var migrations = []pgmigrate.Migration{{Name: "0001-assistant-threads", Statements: `
CREATE TABLE conduit_assistant_threads (
	owner text NOT NULL CHECK (octet_length(owner) BETWEEN 1 AND 256),
	thread_id text NOT NULL,
	title text NOT NULL CHECK (octet_length(title) <= 120),
	created_at timestamptz NOT NULL,
	updated_at timestamptz NOT NULL,
	message_count integer NOT NULL DEFAULT 0 CHECK (message_count BETWEEN 0 AND 200),
	PRIMARY KEY (owner, thread_id)
);
CREATE INDEX conduit_assistant_threads_recent ON conduit_assistant_threads (owner, updated_at DESC, thread_id);
CREATE TABLE conduit_assistant_thread_messages (
	owner text NOT NULL,
	thread_id text NOT NULL,
	position integer NOT NULL CHECK (position BETWEEN 0 AND 199),
	document bytea NOT NULL CHECK (octet_length(document) <= 65536),
	PRIMARY KEY (owner, thread_id, position),
	FOREIGN KEY (owner, thread_id) REFERENCES conduit_assistant_threads (owner, thread_id) ON DELETE CASCADE
);`}}

// Migrations is what a start would apply. See pgmigrate.Pending.
func Migrations() []pgmigrate.Migration { return slices.Clone(migrations) }

// Store is safe for concurrent use and for several replicas on one database.
type Store struct{ db *sql.DB }

// New opens the database and applies this component's migrations.
func New(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := pgmigrate.Apply(ctx, db, Component, migrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (store *Store) Close() error { return store.db.Close() }

func (store *Store) Create(ctx context.Context, thread domain.Thread) error {
	if !domain.ValidOwner(thread.Owner) || !domain.ValidThreadID(thread.ID) || len(thread.Title) > domain.MaxThreadTitleBytes {
		return domain.ErrThreadInvalid
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// One owner's creates are serialized, so two replicas cannot both read a
	// count one under the limit and both insert.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('conduit-assistant-threads:' || $1, 0))`, thread.Owner); err != nil {
		return err
	}
	var owned int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM conduit_assistant_threads WHERE owner = $1`, thread.Owner).Scan(&owned); err != nil {
		return err
	}
	if owned >= domain.MaxThreadsPerOwner {
		return domain.ErrThreadLimit
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO conduit_assistant_threads (owner, thread_id, title, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5) ON CONFLICT (owner, thread_id) DO NOTHING`,
		thread.Owner, thread.ID, thread.Title, thread.CreatedAt.UTC(), thread.UpdatedAt.UTC())
	if err != nil {
		return err
	}
	if inserted, err := result.RowsAffected(); err != nil {
		return err
	} else if inserted != 1 {
		return domain.ErrThreadInvalid
	}
	return tx.Commit()
}

func (store *Store) List(ctx context.Context, owner string) ([]domain.Thread, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT thread_id, title, created_at, updated_at, message_count
		FROM conduit_assistant_threads WHERE owner = $1 ORDER BY updated_at DESC, thread_id ASC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Thread{}
	for rows.Next() {
		thread := domain.Thread{Owner: owner}
		if err := rows.Scan(&thread.ID, &thread.Title, &thread.CreatedAt, &thread.UpdatedAt, &thread.MessageCount); err != nil {
			return nil, err
		}
		thread.CreatedAt, thread.UpdatedAt = thread.CreatedAt.UTC(), thread.UpdatedAt.UTC()
		out = append(out, thread)
	}
	return out, rows.Err()
}

func (store *Store) Get(ctx context.Context, owner, id string) (domain.Thread, error) {
	// One snapshot for the header and the messages, so a concurrent append
	// cannot make the count disagree with the list.
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return domain.Thread{}, err
	}
	defer func() { _ = tx.Rollback() }()
	thread := domain.Thread{ID: id, Owner: owner}
	err = tx.QueryRowContext(ctx, `SELECT title, created_at, updated_at, message_count
		FROM conduit_assistant_threads WHERE owner = $1 AND thread_id = $2`, owner, id).
		Scan(&thread.Title, &thread.CreatedAt, &thread.UpdatedAt, &thread.MessageCount)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Thread{}, domain.ErrThreadNotFound
	}
	if err != nil {
		return domain.Thread{}, err
	}
	thread.CreatedAt, thread.UpdatedAt = thread.CreatedAt.UTC(), thread.UpdatedAt.UTC()
	rows, err := tx.QueryContext(ctx, `SELECT document FROM conduit_assistant_thread_messages
		WHERE owner = $1 AND thread_id = $2 ORDER BY position ASC`, owner, id)
	if err != nil {
		return domain.Thread{}, err
	}
	defer rows.Close()
	thread.Messages = []domain.Message{}
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return domain.Thread{}, err
		}
		var message domain.Message
		if err := json.Unmarshal(document, &message); err != nil {
			return domain.Thread{}, err
		}
		thread.Messages = append(thread.Messages, message)
	}
	if err := rows.Err(); err != nil {
		return domain.Thread{}, err
	}
	return thread, nil
}

func (store *Store) Append(ctx context.Context, owner, id string, messages []domain.Message, title string) error {
	documents := make([][]byte, 0, len(messages))
	for _, message := range messages {
		if err := message.Validate(); err != nil {
			return err
		}
		document, err := json.Marshal(message)
		if err != nil {
			return err
		}
		documents = append(documents, document)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// The row lock orders concurrent appends to one thread, so positions are
	// dense and the count is checked against what is really stored.
	var count int
	err = tx.QueryRowContext(ctx, `SELECT message_count FROM conduit_assistant_threads
		WHERE owner = $1 AND thread_id = $2 FOR UPDATE`, owner, id).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrThreadNotFound
	}
	if err != nil {
		return err
	}
	if len(messages) == 0 {
		return nil
	}
	if count+len(messages) > domain.MaxMessagesPerThread {
		return domain.ErrThreadFull
	}
	for index, document := range documents {
		if _, err := tx.ExecContext(ctx, `INSERT INTO conduit_assistant_thread_messages (owner, thread_id, position, document)
			VALUES ($1, $2, $3, $4)`, owner, id, count+index, document); err != nil {
			return err
		}
	}
	updated := messages[len(messages)-1].CreatedAt.UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE conduit_assistant_threads
		SET message_count = $3, updated_at = $4, title = CASE WHEN title = '' THEN $5 ELSE title END
		WHERE owner = $1 AND thread_id = $2`, owner, id, count+len(messages), updated, domain.ThreadTitle(title)); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *Store) Delete(ctx context.Context, owner, id string) error {
	result, err := store.db.ExecContext(ctx, `DELETE FROM conduit_assistant_threads WHERE owner = $1 AND thread_id = $2`, owner, id)
	if err != nil {
		return err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if deleted != 1 {
		return domain.ErrThreadNotFound
	}
	return nil
}
