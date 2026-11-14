// Package postgres persists immutable lifecycle/audit events with per-session
// transaction-scoped locking and compare-and-append revision validation.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/debugsessions/domain"
	"github.com/jonathanagustin/conduit/internal/debugsessions/ports"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

const Component = "debug-session-journal"

var migrations = []pgmigrate.Migration{{Name: "0001-immutable-session-events", Statements: `CREATE TABLE conduit_debug_session_events (
 session_id text COLLATE "C" NOT NULL CHECK(session_id ~ '^dsr_[a-z2-7]{26}$'),
 revision smallint NOT NULL CHECK(revision BETWEEN 1 AND 3),
 document bytea NOT NULL CHECK(octet_length(document) BETWEEN 1 AND 8192),
 PRIMARY KEY(session_id,revision));`}}

func Migrations() []pgmigrate.Migration { return slices.Clone(migrations) }

type Store struct{ db *sql.DB }

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
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Append(ctx context.Context, expected uint64, next domain.Event) error {
	if !domain.ValidID(next.SessionID) {
		return domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, Component+":"+next.SessionID); err != nil {
		return err
	}
	history, err := readHistory(ctx, tx, next.SessionID)
	if errors.Is(err, domain.ErrNotFound) {
		history = nil
	} else if err != nil {
		return err
	}
	updated, err := domain.AppendHistory(history, expected, next)
	if err != nil {
		return err
	}
	next = updated[len(updated)-1]
	document, err := domain.EncodeEvent(next)
	if err != nil {
		return err
	}
	// Do not inherit an asynchronously acknowledged session setting. The
	// database operator still owns PostgreSQL/WAL/storage durability policy.
	if _, err := tx.ExecContext(ctx, `SET LOCAL synchronous_commit = on`); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO conduit_debug_session_events(session_id,revision,document) VALUES($1,$2,$3) ON CONFLICT(session_id,revision) DO NOTHING`, next.SessionID, next.Revision, document)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return domain.ErrConflict
	}
	return tx.Commit()
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readHistory(ctx context.Context, q queryer, id string) ([]domain.Event, error) {
	if !domain.ValidID(id) {
		return nil, domain.ErrInvalid
	}
	rows, err := q.QueryContext(ctx, `SELECT revision,document FROM conduit_debug_session_events WHERE session_id=$1 ORDER BY revision LIMIT 4`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	history := []domain.Event{}
	for rows.Next() {
		var revision uint64
		var document []byte
		if err := rows.Scan(&revision, &document); err != nil {
			return nil, err
		}
		event, err := domain.DecodeEvent(id, revision, document)
		if err != nil {
			return nil, err
		}
		history = append(history, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, err := domain.ValidateHistory(history); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
		return nil, domain.ErrCorrupt
	}
	return history, nil
}
func (s *Store) History(ctx context.Context, id string) ([]domain.Event, error) {
	return readHistory(ctx, s.db, id)
}
func (s *Store) IDs(ctx context.Context, after string, limit int) ([]string, error) {
	if (after != "" && !domain.ValidID(after)) || limit < 1 || limit > 256 {
		return nil, domain.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT session_id FROM conduit_debug_session_events WHERE session_id>$1 ORDER BY session_id LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if !domain.ValidID(id) {
			return nil, domain.ErrCorrupt
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

var _ ports.Journal = (*Store)(nil)
