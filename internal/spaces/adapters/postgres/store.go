// Package postgres keeps Space identity outside the legacy delivery snapshot.
// Older Hub writers cannot erase Space data by serializing an older snapshot.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
	"github.com/jonathanagustin/conduit/internal/spaces/domain"
	"github.com/jonathanagustin/conduit/internal/spaces/ports"
)

const Component = "spaces"

var migrations = []pgmigrate.Migration{{Name: "0001-spaces", Statements: `CREATE TABLE conduit_spaces (id text PRIMARY KEY, document bytea NOT NULL);`}, {Name: "0002-space-bindings", Statements: `CREATE TABLE conduit_space_bindings (space_id text NOT NULL REFERENCES conduit_spaces(id), resource_kind text NOT NULL, resource_id text NOT NULL, document bytea NOT NULL, PRIMARY KEY (resource_kind,resource_id)); CREATE INDEX conduit_space_bindings_space ON conduit_space_bindings(space_id,resource_kind,resource_id);`}}

func Migrations() []pgmigrate.Migration { return slices.Clone(migrations) }

type Store struct{ db *sql.DB }

var _ ports.Store = (*Store)(nil)

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

func (store *Store) CreateSpace(ctx context.Context, value domain.Space) (domain.Space, error) {
	if err := value.Validate(); err != nil {
		return domain.Space{}, err
	}
	document, err := json.Marshal(value)
	if err != nil {
		return domain.Space{}, err
	}
	var stored []byte
	err = store.db.QueryRowContext(ctx, `INSERT INTO conduit_spaces (id,document) VALUES ($1,$2) ON CONFLICT (id) DO NOTHING RETURNING document`, value.ID, document).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Space{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Space{}, err
	}
	return decode(value.ID, stored)
}
func (store *Store) GetSpace(ctx context.Context, id string) (domain.Space, error) {
	if !domain.ValidID(id) {
		return domain.Space{}, domain.ErrNotFound
	}
	var document []byte
	err := store.db.QueryRowContext(ctx, `SELECT document FROM conduit_spaces WHERE id=$1`, id).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Space{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Space{}, err
	}
	return decode(id, document)
}
func (store *Store) ListSpaces(ctx context.Context) ([]domain.Space, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT id,document FROM conduit_spaces ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]domain.Space, 0)
	for rows.Next() {
		var id string
		var document []byte
		if err := rows.Scan(&id, &document); err != nil {
			return nil, err
		}
		value, err := decode(id, document)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func decode(id string, document []byte) (domain.Space, error) {
	var value domain.Space
	if err := json.Unmarshal(document, &value); err != nil {
		return domain.Space{}, fmt.Errorf("decode Space record: %w", err)
	}
	if value.ID != id {
		return domain.Space{}, errors.New("Space record identity does not match its storage key")
	}
	if err := value.Validate(); err != nil {
		return domain.Space{}, err
	}
	return value, nil
}

func (store *Store) BindResource(ctx context.Context, value domain.Binding) (domain.Binding, error) {
	if err := value.Validate(); err != nil {
		return domain.Binding{}, err
	}
	document, err := json.Marshal(value)
	if err != nil {
		return domain.Binding{}, err
	}
	var stored []byte
	var storedSpace string
	err = store.db.QueryRowContext(ctx, `INSERT INTO conduit_space_bindings (space_id,resource_kind,resource_id,document) SELECT $1,$2,$3,$4 FROM conduit_spaces WHERE id=$1 ON CONFLICT (resource_kind,resource_id) DO UPDATE SET document=conduit_space_bindings.document RETURNING space_id,document`, value.SpaceID, value.Resource.Kind, value.Resource.ID, document).Scan(&storedSpace, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Binding{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Binding{}, err
	}
	existing, err := decodeBinding(value.Resource, stored)
	if err != nil {
		return domain.Binding{}, err
	}
	if existing.SpaceID != storedSpace {
		return domain.Binding{}, errors.New("Space binding does not match its storage index")
	}
	if existing.SpaceID != value.SpaceID {
		return domain.Binding{}, domain.ErrConflict
	}
	return existing, nil
}
func (store *Store) GetBinding(ctx context.Context, resource domain.Resource) (domain.Binding, error) {
	if err := resource.Validate(); err != nil {
		return domain.Binding{}, err
	}
	var document []byte
	var storedSpace string
	err := store.db.QueryRowContext(ctx, `SELECT space_id,document FROM conduit_space_bindings WHERE resource_kind=$1 AND resource_id=$2`, resource.Kind, resource.ID).Scan(&storedSpace, &document)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Binding{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Binding{}, err
	}
	value, err := decodeBinding(resource, document)
	if err != nil {
		return domain.Binding{}, err
	}
	if value.SpaceID != storedSpace {
		return domain.Binding{}, errors.New("Space binding does not match its storage index")
	}
	return value, nil
}
func (store *Store) ListBindings(ctx context.Context, spaceID string) ([]domain.Binding, error) {
	if _, err := store.GetSpace(ctx, spaceID); err != nil {
		return nil, err
	}
	rows, err := store.db.QueryContext(ctx, `SELECT resource_kind,resource_id,document FROM conduit_space_bindings WHERE space_id=$1 ORDER BY resource_kind,resource_id`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]domain.Binding, 0)
	for rows.Next() {
		var resource domain.Resource
		var document []byte
		if err := rows.Scan(&resource.Kind, &resource.ID, &document); err != nil {
			return nil, err
		}
		value, err := decodeBinding(resource, document)
		if err != nil {
			return nil, err
		}
		if value.SpaceID != spaceID {
			return nil, errors.New("Space binding does not match its storage index")
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func decodeBinding(resource domain.Resource, document []byte) (domain.Binding, error) {
	var value domain.Binding
	if err := json.Unmarshal(document, &value); err != nil {
		return domain.Binding{}, fmt.Errorf("decode Space binding: %w", err)
	}
	if value.Resource != resource {
		return domain.Binding{}, errors.New("Space binding does not match its resource key")
	}
	if err := value.Validate(); err != nil {
		return domain.Binding{}, err
	}
	return value, nil
}
