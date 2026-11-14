// Package postgres persists discussion outside legacy delivery snapshots.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
	"slices"
)

const Component = "change-discussion"

var migrations = []pgmigrate.Migration{{Name: "0001-discussion-comments", Statements: `CREATE TABLE conduit_change_discussion (comment_id text NOT NULL,request_id text NOT NULL,document bytea NOT NULL CHECK(octet_length(document)<=131072),PRIMARY KEY(request_id,comment_id)); CREATE INDEX conduit_change_discussion_request ON conduit_change_discussion(request_id);`}}

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
func (s *Store) Append(ctx context.Context, c domain.Comment) error {
	if err := c.Validate(); err != nil {
		return err
	}
	document, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if len(document) > domain.MaxDocumentBytes {
		return domain.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO conduit_change_discussion(comment_id,request_id,document) VALUES($1,$2,$3) ON CONFLICT(request_id,comment_id) DO NOTHING`, c.ID, c.RequestID, document)
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
	return nil
}
func (s *Store) List(ctx context.Context, requestID string) ([]domain.Comment, error) {
	if !domain.ValidRequestID(requestID) {
		return nil, domain.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT comment_id,document FROM conduit_change_discussion WHERE request_id=$1`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Comment, 0)
	for rows.Next() {
		var id string
		var document []byte
		if err := rows.Scan(&id, &document); err != nil {
			return nil, err
		}
		c, err := domain.DecodeComment(requestID, id, document)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
