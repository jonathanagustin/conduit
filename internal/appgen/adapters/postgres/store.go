// Package postgres keeps generation results in PostgreSQL, so they survive a
// Hub restart and every replica reads the same ones.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/appgen/application"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

// Component names this adapter's rows in the migration ledger.
const Component = "appgen"

// MaxDocumentBytes bounds one stored result. A result holds at most a few
// small source files, four rendered templates and a short attempt list.
const MaxDocumentBytes = 1 << 20

// The owner is a column and not only a field of the document, so a list is
// selected by owner in the query and an unowned row matches no caller.
// sequence gives the order results were first saved in.
var migrations = []pgmigrate.Migration{{Name: "0001-app-generations", Statements: `
CREATE TABLE conduit_app_generations (
	id text PRIMARY KEY,
	owner text NOT NULL,
	sequence bigserial NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	document bytea NOT NULL CHECK (octet_length(document) <= 1048576)
);
CREATE INDEX conduit_app_generations_owner ON conduit_app_generations (owner, sequence);`}}

// Migrations returns a copy of the ordered migration list.
func Migrations() []pgmigrate.Migration { return slices.Clone(migrations) }

// Store is the PostgreSQL generation store.
type Store struct{ db *sql.DB }

var _ application.Store = (*Store)(nil)

// New opens the database and applies the migrations.
func New(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	// Generation is rare and each call is one short statement.
	db.SetMaxOpenConns(4)
	if err := pgmigrate.Apply(ctx, db, Component, migrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (store *Store) Close() error { return store.db.Close() }

// Save inserts the result, or replaces it when the same owner already holds
// the id. The owner condition is in the statement, so two writers cannot
// both pass a check and then overwrite each other.
func (store *Store) Save(ctx context.Context, result application.Result) error {
	if strings.TrimSpace(result.ID) == "" {
		return errors.New("generation id is required")
	}
	document, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if len(document) > MaxDocumentBytes {
		return fmt.Errorf("generation %s is %d bytes, over the %d byte limit", result.ID, len(document), MaxDocumentBytes)
	}
	var saved string
	err = store.db.QueryRowContext(ctx, `
INSERT INTO conduit_app_generations (id, owner, document) VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE SET document = EXCLUDED.document, updated_at = now()
WHERE conduit_app_generations.owner = EXCLUDED.owner
RETURNING id`, result.ID, result.Owner, document).Scan(&saved)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrOwnerConflict
	}
	return err
}

func (store *Store) Get(ctx context.Context, id string) (application.Result, bool, error) {
	var owner string
	var document []byte
	err := store.db.QueryRowContext(ctx, `SELECT owner, document FROM conduit_app_generations WHERE id = $1`, id).Scan(&owner, &document)
	if errors.Is(err, sql.ErrNoRows) {
		return application.Result{}, false, nil
	}
	if err != nil {
		return application.Result{}, false, err
	}
	result, err := decode(id, owner, document)
	if err != nil {
		return application.Result{}, false, err
	}
	return result, true, nil
}

// List returns one owner's results, oldest first. A blank owner gets none:
// the query is not run, so an unowned row cannot match it.
func (store *Store) List(ctx context.Context, owner string) ([]application.Result, error) {
	out := []application.Result{}
	if strings.TrimSpace(owner) == "" {
		return out, nil
	}
	rows, err := store.db.QueryContext(ctx, `SELECT id, owner, document FROM conduit_app_generations WHERE owner = $1 ORDER BY sequence`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, rowOwner string
		var document []byte
		if err := rows.Scan(&id, &rowOwner, &document); err != nil {
			return nil, err
		}
		result, err := decode(id, rowOwner, document)
		if err != nil {
			return nil, err
		}
		out = append(out, result)
	}
	return out, rows.Err()
}

// decode reads one row. The id and owner columns are the authority. A
// document that names another id or owner is a corrupt row and is not served.
func decode(id, owner string, document []byte) (application.Result, error) {
	var result application.Result
	if err := json.Unmarshal(document, &result); err != nil {
		return application.Result{}, fmt.Errorf("decode generation record: %w", err)
	}
	if result.ID != id || result.Owner != owner {
		return application.Result{}, errors.New("generation record does not match its storage key")
	}
	return result, nil
}
