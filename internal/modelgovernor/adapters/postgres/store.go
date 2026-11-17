// Package postgres keeps model usage and transcripts in PostgreSQL, so a
// budget is checked against what every replica spent and a transcript
// survives a restart.
package postgres

import (
	"context"
	"database/sql"
	"slices"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

// Component is this store's name in the migration ledger.
const Component = "model-governance"

// The transcript CHECK bounds are ceilings above the configurable caps
// (modelgovernor.DefaultTranscriptRecordBytes, 2 MiB per record): the
// governor enforces the caps, the schema refuses anything absurd whatever the
// governor does. A merged migration does not change; a new bound is a new
// migration.
var migrations = []pgmigrate.Migration{{Name: "0001-model-usage-daily", Statements: `
CREATE TABLE conduit_model_usage_daily (
	day date NOT NULL,
	owner text NOT NULL CHECK (octet_length(owner) BETWEEN 1 AND 256),
	capability text NOT NULL CHECK (octet_length(capability) BETWEEN 1 AND 64),
	transport text NOT NULL CHECK (octet_length(transport) BETWEEN 1 AND 16),
	configured_model text NOT NULL CHECK (octet_length(configured_model) <= 256),
	provider_model text NOT NULL CHECK (octet_length(provider_model) <= 256),
	outcome text NOT NULL CHECK (octet_length(outcome) BETWEEN 1 AND 32),
	calls bigint NOT NULL DEFAULT 0 CHECK (calls >= 0),
	attempts bigint NOT NULL DEFAULT 0 CHECK (attempts >= 0),
	prompt_bytes bigint NOT NULL DEFAULT 0 CHECK (prompt_bytes >= 0),
	response_bytes bigint NOT NULL DEFAULT 0 CHECK (response_bytes >= 0),
	prompt_tokens bigint CHECK (prompt_tokens >= 0),
	completion_tokens bigint CHECK (completion_tokens >= 0),
	calls_with_tokens bigint NOT NULL DEFAULT 0 CHECK (calls_with_tokens >= 0),
	latency_millis bigint NOT NULL DEFAULT 0 CHECK (latency_millis >= 0),
	PRIMARY KEY (day, owner, capability, transport, configured_model, provider_model, outcome)
);
CREATE INDEX conduit_model_usage_daily_owner ON conduit_model_usage_daily (owner, day);`},
	{Name: "0002-ai-transcripts", Statements: `
CREATE TABLE conduit_ai_transcripts (
	run_id text NOT NULL CHECK (octet_length(run_id) BETWEEN 1 AND 128),
	round integer NOT NULL CHECK (round BETWEEN 1 AND 100000),
	session_id text NOT NULL CHECK (octet_length(session_id) <= 512),
	owner text NOT NULL CHECK (octet_length(owner) BETWEEN 1 AND 256),
	capability text NOT NULL CHECK (octet_length(capability) BETWEEN 1 AND 64),
	sent_prompt_sha256 text NOT NULL CHECK (octet_length(sent_prompt_sha256) = 71),
	received_response_sha256 text NOT NULL CHECK (octet_length(received_response_sha256) = 71),
	stored_prompt_sha256 text NOT NULL CHECK (octet_length(stored_prompt_sha256) = 71),
	stored_response_sha256 text NOT NULL CHECK (octet_length(stored_response_sha256) = 71),
	prompt bytea NOT NULL,
	response bytea NOT NULL,
	created_at timestamptz NOT NULL,
	expires_at timestamptz NOT NULL,
	PRIMARY KEY (run_id, round),
	CHECK (octet_length(prompt) + octet_length(response) <= 16777216)
);
CREATE INDEX conduit_ai_transcripts_expiry ON conduit_ai_transcripts (expires_at);
CREATE INDEX conduit_ai_transcripts_session ON conduit_ai_transcripts (session_id, created_at);`}}

// Migrations is what a start would apply. See pgmigrate.Pending.
func Migrations() []pgmigrate.Migration { return slices.Clone(migrations) }

// Store is both ports. It is safe for concurrent use and for several
// replicas on one database.
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

// Record adds one call to its row in a single statement, so two replicas
// recording at once both land.
func (store *Store) Record(ctx context.Context, record modelgovernor.UsageRecord) error {
	withTokens := 0
	if record.PromptTokens != nil || record.CompletionTokens != nil {
		withTokens = 1
	}
	_, err := store.db.ExecContext(ctx, `
INSERT INTO conduit_model_usage_daily AS usage (
	day, owner, capability, transport, configured_model, provider_model, outcome,
	calls, attempts, prompt_bytes, response_bytes, prompt_tokens, completion_tokens, calls_with_tokens, latency_millis)
VALUES ($1::date, $2, $3, $4, $5, $6, $7, 1, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (day, owner, capability, transport, configured_model, provider_model, outcome) DO UPDATE SET
	calls = usage.calls + 1,
	attempts = usage.attempts + EXCLUDED.attempts,
	prompt_bytes = usage.prompt_bytes + EXCLUDED.prompt_bytes,
	response_bytes = usage.response_bytes + EXCLUDED.response_bytes,
	prompt_tokens = CASE WHEN EXCLUDED.prompt_tokens IS NULL THEN usage.prompt_tokens
		ELSE COALESCE(usage.prompt_tokens, 0) + EXCLUDED.prompt_tokens END,
	completion_tokens = CASE WHEN EXCLUDED.completion_tokens IS NULL THEN usage.completion_tokens
		ELSE COALESCE(usage.completion_tokens, 0) + EXCLUDED.completion_tokens END,
	calls_with_tokens = usage.calls_with_tokens + EXCLUDED.calls_with_tokens,
	latency_millis = usage.latency_millis + EXCLUDED.latency_millis`,
		record.Day, record.Owner, record.Capability, record.Transport, record.ConfiguredModel, record.ProviderModel, record.Outcome,
		record.Attempts, record.PromptBytes, record.ResponseBytes, nullable(record.PromptTokens), nullable(record.CompletionTokens),
		withTokens, max(record.Latency.Milliseconds(), 0))
	return err
}

func nullable(count *int64) sql.NullInt64 {
	if count == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *count, Valid: true}
}

func pointer(count sql.NullInt64) *int64 {
	if !count.Valid {
		return nil
	}
	return &count.Int64
}

func (store *Store) OwnerDay(ctx context.Context, owner, day string) (modelgovernor.Totals, error) {
	var totals modelgovernor.Totals
	err := store.db.QueryRowContext(ctx, `
SELECT COALESCE(sum(calls), 0), COALESCE(sum(COALESCE(prompt_tokens, 0) + COALESCE(completion_tokens, 0)), 0)
FROM conduit_model_usage_daily
WHERE owner = $1 AND day = $2::date AND outcome NOT IN ($3, $4)`,
		owner, day, string(modelgovernor.OutcomeThrottled), string(modelgovernor.OutcomeBudgetExceeded),
	).Scan(&totals.Calls, &totals.Tokens)
	return totals, err
}

func (store *Store) Summary(ctx context.Context, query modelgovernor.UsageQuery) ([]modelgovernor.UsageRow, error) {
	rows, err := store.db.QueryContext(ctx, `
SELECT to_char(day, 'YYYY-MM-DD'), owner, capability, transport, configured_model, provider_model, outcome,
	calls, attempts, prompt_bytes, response_bytes, prompt_tokens, completion_tokens, calls_with_tokens, latency_millis
FROM conduit_model_usage_daily
WHERE day BETWEEN $1::date AND $2::date AND ($3 = '' OR owner = $3)
ORDER BY day DESC, owner, capability, transport, configured_model, provider_model, outcome`,
		query.FromDay, query.ToDay, query.Owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []modelgovernor.UsageRow{}
	for rows.Next() {
		var row modelgovernor.UsageRow
		var promptTokens, completionTokens sql.NullInt64
		if err := rows.Scan(&row.Day, &row.Owner, &row.Capability, &row.Transport, &row.ConfiguredModel, &row.ProviderModel, &row.Outcome,
			&row.Calls, &row.Attempts, &row.PromptBytes, &row.ResponseBytes, &promptTokens, &completionTokens,
			&row.CallsWithTokens, &row.LatencyMillis); err != nil {
			return nil, err
		}
		row.PromptTokens, row.CompletionTokens = pointer(promptTokens), pointer(completionTokens)
		out = append(out, row)
	}
	return out, rows.Err()
}

// Put serializes one run's writes, so two replicas cannot both read a size
// under the cap and both insert past it.
func (store *Store) Put(ctx context.Context, record modelgovernor.TranscriptRecord, runCapBytes int64) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('conduit-ai-transcripts:' || $1, 0))`, record.RunID); err != nil {
		return err
	}
	var stored int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(sum(octet_length(prompt) + octet_length(response)), 0) FROM conduit_ai_transcripts WHERE run_id = $1`,
		record.RunID).Scan(&stored); err != nil {
		return err
	}
	if runCapBytes > 0 && stored+int64(len(record.Prompt)+len(record.Response)) > runCapBytes {
		return modelgovernor.ErrTranscriptRunFull
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO conduit_ai_transcripts (
	run_id, round, session_id, owner, capability, sent_prompt_sha256, received_response_sha256,
	stored_prompt_sha256, stored_response_sha256, prompt, response, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		record.RunID, record.Round, record.SessionID, record.Owner, record.Capability,
		record.SentPromptSHA256, record.ReceivedResponseSHA256, record.StoredPromptSHA256, record.StoredResponseSHA256,
		notNil(record.Prompt), notNil(record.Response), record.CreatedAt.UTC(), record.ExpiresAt.UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

// notNil keeps an empty side as empty bytes; a nil slice would be SQL NULL.
func notNil(data []byte) []byte {
	if data == nil {
		return []byte{}
	}
	return data
}

func (store *Store) Run(ctx context.Context, runID string, now time.Time) ([]modelgovernor.TranscriptRecord, error) {
	rows, err := store.db.QueryContext(ctx, `
SELECT run_id, round, session_id, owner, capability, sent_prompt_sha256, received_response_sha256,
	stored_prompt_sha256, stored_response_sha256, prompt, response, created_at, expires_at
FROM conduit_ai_transcripts WHERE run_id = $1 AND expires_at > $2 ORDER BY round`, runID, now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []modelgovernor.TranscriptRecord{}
	for rows.Next() {
		var record modelgovernor.TranscriptRecord
		if err := rows.Scan(&record.RunID, &record.Round, &record.SessionID, &record.Owner, &record.Capability,
			&record.SentPromptSHA256, &record.ReceivedResponseSHA256, &record.StoredPromptSHA256, &record.StoredResponseSHA256,
			&record.Prompt, &record.Response, &record.CreatedAt, &record.ExpiresAt); err != nil {
			return nil, err
		}
		record.CreatedAt, record.ExpiresAt = record.CreatedAt.UTC(), record.ExpiresAt.UTC()
		out = append(out, record)
	}
	return out, rows.Err()
}

func (store *Store) Runs(ctx context.Context, filter modelgovernor.RunFilter, now time.Time) ([]modelgovernor.RunSummary, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 200
	}
	rows, err := store.db.QueryContext(ctx, `
SELECT run_id, max(session_id), max(owner), max(capability), count(*),
	sum(octet_length(prompt) + octet_length(response)), min(created_at), min(expires_at)
FROM conduit_ai_transcripts
WHERE expires_at > $1 AND ($2 = '' OR owner = $2) AND ($3 = '' OR session_id = $3)
GROUP BY run_id ORDER BY min(created_at) DESC, run_id LIMIT $4`, now.UTC(), filter.Owner, filter.SessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []modelgovernor.RunSummary{}
	for rows.Next() {
		var summary modelgovernor.RunSummary
		if err := rows.Scan(&summary.RunID, &summary.SessionID, &summary.Owner, &summary.Capability, &summary.Rounds,
			&summary.Bytes, &summary.FirstAt, &summary.ExpiresAt); err != nil {
			return nil, err
		}
		summary.FirstAt, summary.ExpiresAt = summary.FirstAt.UTC(), summary.ExpiresAt.UTC()
		out = append(out, summary)
	}
	return out, rows.Err()
}

// DeleteExpired is one statement. Two replicas running it at once each delete
// the rows the other has not; neither waits on a lock the other never
// releases, and a row is deleted once.
func (store *Store) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	result, err := store.db.ExecContext(ctx, `DELETE FROM conduit_ai_transcripts WHERE expires_at <= $1`, now.UTC())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
