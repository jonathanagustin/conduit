// Package revocationpostgres keeps the Agent deny-list in PostgreSQL, so that
// a revocation written through one Hub replica is the next thing every other
// replica reads. There is no cache in front of it: an Agent request reads its
// Environment's rows by primary-key prefix, and staleness is zero by
// construction rather than bounded by a number somebody has to defend.
package revocationpostgres

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

// Component is this store's name in the migration ledger.
const Component = "agent-credentials"

// The CHECK bounds repeat domain.MaximumRevocationReasonBytes (512) and the
// 63-byte DNS label: a merged migration does not change, so a later change to
// a bound is a new migration. The scope CHECK is the three scopes this code
// decides on; a fourth is a migration and a code change together.
var migrations = []pgmigrate.Migration{{Name: "0001-agent-credentials", Statements: `
CREATE TABLE conduit_agent_revocations (
	environment text NOT NULL CHECK (octet_length(environment) BETWEEN 1 AND 63),
	revocation_id text NOT NULL CHECK (octet_length(revocation_id) BETWEEN 1 AND 64),
	scope text NOT NULL CHECK (scope IN ('SERIAL', 'AGENT', 'ENVIRONMENT')),
	agent_id text NOT NULL DEFAULT '' CHECK (octet_length(agent_id) <= 63),
	serial text NOT NULL DEFAULT '' CHECK (octet_length(serial) <= 64),
	not_before timestamptz,
	reason text NOT NULL CHECK (octet_length(reason) BETWEEN 1 AND 512),
	created_by text NOT NULL CHECK (octet_length(created_by) BETWEEN 1 AND 256),
	created_at timestamptz NOT NULL,
	PRIMARY KEY (environment, revocation_id)
);
CREATE TABLE conduit_agent_credential_sightings (
	environment text NOT NULL CHECK (octet_length(environment) BETWEEN 1 AND 63),
	agent_id text NOT NULL CHECK (octet_length(agent_id) BETWEEN 1 AND 63),
	serial text NOT NULL CHECK (octet_length(serial) <= 64),
	not_before timestamptz NOT NULL,
	not_after timestamptz NOT NULL,
	seen_at timestamptz NOT NULL,
	PRIMARY KEY (environment, agent_id)
);
CREATE INDEX conduit_agent_credential_sightings_seen ON conduit_agent_credential_sightings (seen_at);`}}

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

const revocationColumns = `revocation_id, scope, environment, agent_id, serial, not_before, reason, created_by, created_at`

type scanner interface{ Scan(...any) error }

func scanRevocation(row scanner) (domain.AgentRevocation, error) {
	var revocation domain.AgentRevocation
	var notBefore sql.NullTime
	err := row.Scan(&revocation.ID, &revocation.Scope, &revocation.Environment, &revocation.AgentID,
		&revocation.Serial, &notBefore, &revocation.Reason, &revocation.CreatedBy, &revocation.CreatedAt)
	if notBefore.Valid {
		revocation.NotBefore = notBefore.Time.UTC()
	}
	revocation.CreatedAt = revocation.CreatedAt.UTC()
	return revocation, err
}

func (store *Store) Revocations(ctx context.Context, environment string) ([]domain.AgentRevocation, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT `+revocationColumns+`
		FROM conduit_agent_revocations WHERE environment = $1 ORDER BY revocation_id`, environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AgentRevocation{}
	for rows.Next() {
		revocation, err := scanRevocation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, revocation)
	}
	return out, rows.Err()
}

func (store *Store) PutRevocation(ctx context.Context, revocation domain.AgentRevocation) (*domain.AgentRevocation, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// One Environment's writes are serialized, so two replicas cannot both read
	// a count one under the limit and both insert.
	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('conduit-agent-revocations:' || $1, 0))`, revocation.Environment); err != nil {
		return nil, err
	}
	var previous *domain.AgentRevocation
	existing, err := scanRevocation(tx.QueryRowContext(ctx, `SELECT `+revocationColumns+`
		FROM conduit_agent_revocations WHERE environment = $1 AND revocation_id = $2`, revocation.Environment, revocation.ID))
	switch {
	case err == nil:
		previous = &existing
	case errors.Is(err, sql.ErrNoRows):
		var held int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM conduit_agent_revocations WHERE environment = $1`,
			revocation.Environment).Scan(&held); err != nil {
			return nil, err
		}
		if held >= domain.MaximumRevocationsPerEnvironment {
			return nil, domain.ErrRevocationLimit
		}
	default:
		return nil, err
	}
	var notBefore sql.NullTime
	if !revocation.NotBefore.IsZero() {
		notBefore = sql.NullTime{Time: revocation.NotBefore.UTC(), Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO conduit_agent_revocations (`+revocationColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (environment, revocation_id) DO UPDATE SET scope = EXCLUDED.scope, agent_id = EXCLUDED.agent_id,
			serial = EXCLUDED.serial, not_before = EXCLUDED.not_before, reason = EXCLUDED.reason,
			created_by = EXCLUDED.created_by, created_at = EXCLUDED.created_at`,
		revocation.ID, revocation.Scope, revocation.Environment, revocation.AgentID, revocation.Serial, notBefore,
		revocation.Reason, revocation.CreatedBy, revocation.CreatedAt.UTC()); err != nil {
		return nil, err
	}
	return previous, tx.Commit()
}

func (store *Store) DeleteRevocation(ctx context.Context, environment, id string) (domain.AgentRevocation, error) {
	revocation, err := scanRevocation(store.db.QueryRowContext(ctx, `DELETE FROM conduit_agent_revocations
		WHERE environment = $1 AND revocation_id = $2 RETURNING `+revocationColumns, environment, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AgentRevocation{}, domain.ErrRevocationNotFound
	}
	if err != nil {
		return domain.AgentRevocation{}, err
	}
	return revocation, nil
}

// RecordSighting keeps the newer of what is held and what is offered. Two
// replicas record independently and their writes can land out of order.
func (store *Store) RecordSighting(ctx context.Context, sighting domain.AgentCredentialSighting) error {
	_, err := store.db.ExecContext(ctx, `INSERT INTO conduit_agent_credential_sightings
		(environment, agent_id, serial, not_before, not_after, seen_at) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (environment, agent_id) DO UPDATE SET serial = EXCLUDED.serial, not_before = EXCLUDED.not_before,
			not_after = EXCLUDED.not_after, seen_at = EXCLUDED.seen_at
		WHERE conduit_agent_credential_sightings.seen_at <= EXCLUDED.seen_at`,
		sighting.Environment, sighting.AgentID, sighting.Serial, sighting.NotBefore.UTC(), sighting.NotAfter.UTC(), sighting.SeenAt.UTC())
	return err
}

func (store *Store) Sightings(ctx context.Context, environment string) ([]domain.AgentCredentialSighting, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT environment, agent_id, serial, not_before, not_after, seen_at
		FROM conduit_agent_credential_sightings WHERE environment = $1 ORDER BY agent_id`, environment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AgentCredentialSighting{}
	for rows.Next() {
		var sighting domain.AgentCredentialSighting
		if err := rows.Scan(&sighting.Environment, &sighting.AgentID, &sighting.Serial,
			&sighting.NotBefore, &sighting.NotAfter, &sighting.SeenAt); err != nil {
			return nil, err
		}
		sighting.NotBefore, sighting.NotAfter, sighting.SeenAt = sighting.NotBefore.UTC(), sighting.NotAfter.UTC(), sighting.SeenAt.UTC()
		out = append(out, sighting)
	}
	return out, rows.Err()
}

func (store *Store) EarliestExpiry(ctx context.Context, seenSince time.Time) (time.Time, int, error) {
	var earliest sql.NullTime
	var agents int
	err := store.db.QueryRowContext(ctx, `SELECT min(not_after), count(*)
		FROM conduit_agent_credential_sightings WHERE seen_at >= $1`, seenSince.UTC()).Scan(&earliest, &agents)
	if err != nil || !earliest.Valid {
		return time.Time{}, 0, err
	}
	return earliest.Time.UTC(), agents, nil
}
