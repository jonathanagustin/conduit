// Package casepostgres keeps operator-added eval cases in PostgreSQL, so a
// case survives a Hub restart and is the same on every replica.
package casepostgres

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/jonathanagustin/conduit/internal/intelligence/application"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

// Component is this store's name in the migration ledger.
const Component = "intelligence-eval-cases"

// position orders cases by insertion. The byte bounds are ceilings on
// operator text; the HTTP layer already limits the request body.
var migrations = []pgmigrate.Migration{{Name: "0001-intelligence-eval-cases", Statements: `
CREATE TABLE conduit_intelligence_eval_cases (
	case_id text PRIMARY KEY CHECK (octet_length(case_id) BETWEEN 1 AND 128),
	position bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
	scenario text NOT NULL CHECK (octet_length(scenario) BETWEEN 1 AND 65536),
	bound_object text NOT NULL CHECK (octet_length(bound_object) <= 65536),
	value text NOT NULL CHECK (octet_length(value) <= 65536),
	expected_outcome text NOT NULL CHECK (octet_length(expected_outcome) BETWEEN 1 AND 256),
	source text NOT NULL CHECK (octet_length(source) <= 256)
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

func (store *Store) Add(ctx context.Context, item application.Case, limit int) (application.Case, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return application.Case{}, err
	}
	defer func() { _ = tx.Rollback() }()
	// Adds are serialized, so two replicas cannot both read a count one
	// under the limit and both insert.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('conduit-intelligence-eval-cases', 0))`); err != nil {
		return application.Case{}, err
	}
	existing := application.Case{ID: item.ID}
	err = tx.QueryRowContext(ctx, `SELECT scenario, bound_object, value, expected_outcome, source
		FROM conduit_intelligence_eval_cases WHERE case_id = $1`, item.ID).
		Scan(&existing.Scenario, &existing.BoundObject, &existing.Value, &existing.ExpectedOutcome, &existing.Source)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return application.Case{}, err
	}
	var stored int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM conduit_intelligence_eval_cases`).Scan(&stored); err != nil {
		return application.Case{}, err
	}
	if stored >= limit {
		return application.Case{}, application.ErrCaseLimit
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO conduit_intelligence_eval_cases
		(case_id, scenario, bound_object, value, expected_outcome, source) VALUES ($1, $2, $3, $4, $5, $6)`,
		item.ID, item.Scenario, item.BoundObject, item.Value, item.ExpectedOutcome, item.Source); err != nil {
		return application.Case{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.Case{}, err
	}
	return item, nil
}

func (store *Store) List(ctx context.Context) ([]application.Case, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT case_id, scenario, bound_object, value, expected_outcome, source
		FROM conduit_intelligence_eval_cases ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []application.Case{}
	for rows.Next() {
		var item application.Case
		if err := rows.Scan(&item.ID, &item.Scenario, &item.BoundObject, &item.Value, &item.ExpectedOutcome, &item.Source); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

var _ application.CaseStore = (*Store)(nil)
