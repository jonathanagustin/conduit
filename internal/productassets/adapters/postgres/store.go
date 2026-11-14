// Package postgres keeps download metadata outside the legacy catalogue
// snapshot, so older writers cannot erase declarations they do not model.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
	"github.com/jonathanagustin/conduit/internal/productassets/domain"
	"github.com/jonathanagustin/conduit/internal/productassets/ports"
)

const Component = "product-assets"

var migrations = []pgmigrate.Migration{{Name: "0001-download-asset-manifests", Statements: `CREATE TABLE conduit_product_asset_manifests (coordinate text PRIMARY KEY, document bytea NOT NULL CHECK(octet_length(document) <= 4194304));`}}

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
func (store *Store) Prepare(ctx context.Context, record domain.Prepared) (domain.Prepared, error) {
	if err := record.Validate(); err != nil {
		return domain.Prepared{}, err
	}
	document, err := json.Marshal(record)
	if err != nil {
		return domain.Prepared{}, err
	}
	if len(document) > domain.MaxDocumentBytes {
		return domain.Prepared{}, domain.ErrInvalid
	}
	var saved []byte
	err = store.db.QueryRowContext(ctx, `INSERT INTO conduit_product_asset_manifests (coordinate,document) VALUES ($1,$2) ON CONFLICT (coordinate) DO NOTHING RETURNING document`, record.Manifest.Coordinate(), document).Scan(&saved)
	if errors.Is(err, sql.ErrNoRows) {
		existing, err := store.Get(ctx, record.Manifest.Coordinate())
		if err != nil {
			return domain.Prepared{}, err
		}
		if existing.Digest != record.Digest {
			return domain.Prepared{}, domain.ErrConflict
		}
		return existing, nil
	}
	if err != nil {
		return domain.Prepared{}, err
	}
	return domain.DecodePrepared(record.Manifest.Coordinate(), saved)
}
func (store *Store) Get(ctx context.Context, coordinate string) (domain.Prepared, error) {
	if !domain.ValidCoordinate(coordinate) {
		return domain.Prepared{}, domain.ErrNotFound
	}
	var document []byte
	err := store.db.QueryRowContext(ctx, `SELECT document FROM conduit_product_asset_manifests WHERE coordinate=$1`, coordinate).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Prepared{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Prepared{}, err
	}
	return domain.DecodePrepared(coordinate, document)
}
