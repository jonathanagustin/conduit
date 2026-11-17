// Package sessionpostgres keeps operator sessions in PostgreSQL, so a person
// who signed in through one Hub replica is signed in at all of them, and a
// sign-out at one is a sign-out everywhere.
//
// Nothing stored here is a credential on its own. A session row carries the
// SHA-256 of the cookie's value, not the value; a sign-in attempt carries the
// hash of the state and of the browser binding. The attempt does hold its PKCE
// verifier and nonce for the ten minutes it lives, and neither is useful
// without the authorization code that only the binding browser receives.
package sessionpostgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

// Component is this store's name in the migration ledger.
const Component = "operator-sessions"

// The hash columns are fixed at 64 hexadecimal characters: a row that is not
// keyed by a SHA-256 is one this adapter did not write. The 24-hour CHECK
// repeats domain.MaximumSessionLifetime; a merged migration does not change, so
// raising that bound is a new migration.
var migrations = []pgmigrate.Migration{{Name: "0001-operator-sessions", Statements: `
CREATE TABLE conduit_operator_sessions (
	id_hash text PRIMARY KEY CHECK (id_hash ~ '^[0-9a-f]{64}$'),
	claims bytea NOT NULL CHECK (octet_length(claims) BETWEEN 2 AND 65536),
	created_at timestamptz NOT NULL,
	expires_at timestamptz NOT NULL,
	CHECK (expires_at > created_at),
	CHECK (expires_at <= created_at + interval '24 hours')
);
CREATE INDEX conduit_operator_sessions_expiry ON conduit_operator_sessions (expires_at);
CREATE TABLE conduit_operator_login_attempts (
	state_hash text PRIMARY KEY CHECK (state_hash ~ '^[0-9a-f]{64}$'),
	binding_hash text NOT NULL CHECK (binding_hash ~ '^[0-9a-f]{64}$'),
	nonce text NOT NULL CHECK (octet_length(nonce) BETWEEN 32 AND 128),
	code_verifier text NOT NULL CHECK (octet_length(code_verifier) BETWEEN 43 AND 128),
	return_path text NOT NULL CHECK (octet_length(return_path) BETWEEN 1 AND 2048),
	expires_at timestamptz NOT NULL
);
CREATE INDEX conduit_operator_login_attempts_expiry ON conduit_operator_login_attempts (expires_at);`}}

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

func (store *Store) SaveLoginAttempt(ctx context.Context, attempt domain.LoginAttempt, now time.Time) error {
	// Expired attempts are swept on the way in, so the table is bounded by the
	// anonymous rate limit times the attempt lifetime even if Prune never runs.
	if _, err := store.db.ExecContext(ctx,
		`DELETE FROM conduit_operator_login_attempts WHERE expires_at <= $1`, now.UTC()); err != nil {
		return err
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO conduit_operator_login_attempts
		(state_hash, binding_hash, nonce, code_verifier, return_path, expires_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		attempt.StateHash, attempt.BindingHash, attempt.Nonce, attempt.CodeVerifier, attempt.ReturnPath,
		attempt.ExpiresAt.UTC())
	return err
}

func (store *Store) TakeLoginAttempt(ctx context.Context, stateHash string, now time.Time) (domain.LoginAttempt, error) {
	// DELETE ... RETURNING is the single step: of two replicas asked to redeem
	// the same state, exactly one gets the row.
	attempt := domain.LoginAttempt{StateHash: stateHash}
	err := store.db.QueryRowContext(ctx, `DELETE FROM conduit_operator_login_attempts WHERE state_hash = $1
		RETURNING binding_hash, nonce, code_verifier, return_path, expires_at`, stateHash).
		Scan(&attempt.BindingHash, &attempt.Nonce, &attempt.CodeVerifier, &attempt.ReturnPath, &attempt.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.LoginAttempt{}, domain.ErrSessionNotFound
	}
	if err != nil {
		return domain.LoginAttempt{}, err
	}
	attempt.ExpiresAt = attempt.ExpiresAt.UTC()
	if !attempt.ExpiresAt.After(now) {
		return domain.LoginAttempt{}, domain.ErrSessionNotFound
	}
	return attempt, nil
}

func (store *Store) SaveSession(ctx context.Context, session domain.Session) error {
	if session.Claims.Username == "" {
		return errors.New("a session requires a username")
	}
	claims, err := json.Marshal(session.Claims)
	if err != nil {
		return err
	}
	_, err = store.db.ExecContext(ctx, `INSERT INTO conduit_operator_sessions (id_hash, claims, created_at, expires_at)
		VALUES ($1, $2, $3, $4)`, session.IDHash, claims, session.CreatedAt.UTC(), session.ExpiresAt.UTC())
	return err
}

func (store *Store) Session(ctx context.Context, idHash string, now time.Time) (domain.Session, error) {
	session := domain.Session{IDHash: idHash}
	var claims []byte
	err := store.db.QueryRowContext(ctx, `SELECT claims, created_at, expires_at FROM conduit_operator_sessions
		WHERE id_hash = $1 AND expires_at > $2`, idHash, now.UTC()).
		Scan(&claims, &session.CreatedAt, &session.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Session{}, domain.ErrSessionNotFound
	}
	if err != nil {
		return domain.Session{}, err
	}
	if err := json.Unmarshal(claims, &session.Claims); err != nil {
		return domain.Session{}, err
	}
	session.CreatedAt, session.ExpiresAt = session.CreatedAt.UTC(), session.ExpiresAt.UTC()
	return session, nil
}

func (store *Store) DeleteSession(ctx context.Context, idHash string) error {
	_, err := store.db.ExecContext(ctx, `DELETE FROM conduit_operator_sessions WHERE id_hash = $1`, idHash)
	return err
}

func (store *Store) Prune(ctx context.Context, now time.Time) error {
	if _, err := store.db.ExecContext(ctx,
		`DELETE FROM conduit_operator_sessions WHERE expires_at <= $1`, now.UTC()); err != nil {
		return err
	}
	_, err := store.db.ExecContext(ctx,
		`DELETE FROM conduit_operator_login_attempts WHERE expires_at <= $1`, now.UTC())
	return err
}
