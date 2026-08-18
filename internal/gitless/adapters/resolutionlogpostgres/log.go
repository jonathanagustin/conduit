// Package resolutionlogpostgres records Gitless source resolutions in the
// Hub's shared PostgreSQL database. Every replica appends to the same ordered
// history, so the record-before-deploy contract holds for the fleet rather
// than for one pod's filesystem.
package resolutionlogpostgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
	"github.com/jonathanagustin/conduit/internal/gitless/ports"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

const component = "gitless-resolution-log"

var migrations = []pgmigrate.Migration{
	{
		Name: "0001-resolution-history",
		Statements: `
CREATE TABLE conduit_gitless_resolutions (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    recorded_at timestamptz NOT NULL,
    environment text NOT NULL,
    selector text NOT NULL,
    selector_kind text NOT NULL,
    repository text NOT NULL,
    digest text NOT NULL,
    superseded text NOT NULL,
    moved boolean NOT NULL,
    publisher text NOT NULL,
    failure text NOT NULL
);
CREATE INDEX conduit_gitless_resolutions_environment
    ON conduit_gitless_resolutions (environment, recorded_at DESC, sequence DESC);`,
	},
}

// Migrations and Component expose this adapter's forward-only schema to the
// repository's migration audit and restore drill.
func Migrations() []pgmigrate.Migration { return slices.Clone(migrations) }
func Component() string                 { return component }

// Log appends resolutions to one shared database.
type Log struct {
	database *sql.DB
	now      func() time.Time
}

var _ ports.ResolutionLog = (*Log)(nil)

func New(ctx context.Context, dataSourceName string) (*Log, error) {
	if strings.TrimSpace(dataSourceName) == "" {
		return nil, errors.New("PostgreSQL data source name is required")
	}
	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open Gitless resolution log: %w", err)
	}
	database.SetMaxOpenConns(5)
	database.SetMaxIdleConns(2)
	database.SetConnMaxLifetime(30 * time.Minute)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("connect Gitless resolution log: %w", err)
	}
	if err := pgmigrate.Apply(ctx, database, component, migrations); err != nil {
		database.Close()
		return nil, fmt.Errorf("initialize Gitless resolution log: %w", err)
	}
	return &Log{database: database, now: time.Now}, nil
}

func (log *Log) Close() error {
	if log == nil || log.database == nil {
		return nil
	}
	return log.database.Close()
}

func (log *Log) Record(ctx context.Context, resolution domain.Resolution) error {
	if err := resolution.Validate(); err != nil {
		return fmt.Errorf("refusing to record an invalid resolution: %w", err)
	}
	_, err := log.database.ExecContext(ctx, `
INSERT INTO conduit_gitless_resolutions
    (recorded_at, environment, selector, selector_kind, repository, digest,
     superseded, moved, publisher, failure)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		log.now().UTC(), resolution.Environment, resolution.Selector.String(),
		string(resolution.Selector.Kind), resolution.Pin.Repository, resolution.Pin.Digest,
		resolution.Superseded, resolution.Moved(), resolution.Publisher, resolution.Failure)
	if err != nil {
		return fmt.Errorf("append Gitless resolution: %w", err)
	}
	return nil
}
