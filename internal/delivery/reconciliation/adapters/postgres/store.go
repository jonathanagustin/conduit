// Package postgres persists reconciliation inputs, leased work, decisions,
// audit evidence, proposal outbox entries, and planner receipts in PostgreSQL.
// It owns its schema independently of the transitional control-plane snapshot.
package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/ports"
)

const schema = `
CREATE TABLE IF NOT EXISTS conduit_reconciliation_desired (
    environment text NOT NULL,
    installation text NOT NULL,
    generation text NOT NULL,
    target_digest text NOT NULL,
    payload_digest text NOT NULL,
    payload bytea NOT NULL,
    recorded_at timestamptz NOT NULL,
    PRIMARY KEY (environment, installation)
);
CREATE TABLE IF NOT EXISTS conduit_reconciliation_observations (
    environment text NOT NULL,
    installation text NOT NULL,
    generation text NOT NULL,
    target_digest text NOT NULL,
    complete boolean NOT NULL,
    observed_at timestamptz NOT NULL,
    PRIMARY KEY (environment, installation)
);
CREATE TABLE IF NOT EXISTS conduit_reconciliation_work (
    environment text NOT NULL,
    installation text NOT NULL,
    available_at timestamptz NOT NULL,
    lease_token text,
    lease_expires_at timestamptz,
    revision bigint NOT NULL CHECK (revision > 0),
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    exhausted boolean NOT NULL DEFAULT false,
    last_error text NOT NULL DEFAULT '',
    PRIMARY KEY (environment, installation)
);
CREATE INDEX IF NOT EXISTS conduit_reconciliation_work_available
    ON conduit_reconciliation_work (available_at, environment, installation)
    WHERE exhausted = false;
CREATE TABLE IF NOT EXISTS conduit_reconciliation_decisions (
    id text PRIMARY KEY,
    environment text NOT NULL,
    installation text NOT NULL,
    input_digest text NOT NULL,
    kind text NOT NULL,
    decided_at timestamptz NOT NULL,
    document bytea NOT NULL
);
CREATE INDEX IF NOT EXISTS conduit_reconciliation_decisions_latest
    ON conduit_reconciliation_decisions (environment, installation, decided_at DESC, id DESC);
CREATE TABLE IF NOT EXISTS conduit_reconciliation_audit (
    decision_id text PRIMARY KEY,
    environment text NOT NULL,
    installation text NOT NULL,
    occurred_at timestamptz NOT NULL,
    document bytea NOT NULL
);
CREATE TABLE IF NOT EXISTS conduit_reconciliation_proposal_outbox (
    proposal_id text PRIMARY KEY,
    idempotency_key text NOT NULL UNIQUE,
    environment text NOT NULL,
    installation text NOT NULL,
    available_at timestamptz NOT NULL,
    lease_token text,
    lease_expires_at timestamptz,
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    exhausted boolean NOT NULL DEFAULT false,
    last_error text NOT NULL DEFAULT '',
    document bytea NOT NULL
);
CREATE INDEX IF NOT EXISTS conduit_reconciliation_outbox_available
    ON conduit_reconciliation_proposal_outbox (available_at, proposal_id)
    WHERE exhausted = false;
CREATE TABLE IF NOT EXISTS conduit_reconciliation_plan_receipts (
    proposal_id text PRIMARY KEY,
    plan_id text NOT NULL,
    created_at timestamptz NOT NULL,
    document bytea NOT NULL
);`

// Store is a multi-replica reconciliation adapter. Leases use row locks with
// SKIP LOCKED; all decision, audit, and outbox writes share one transaction.
type Store struct {
	database *sql.DB
}

var _ ports.Store = (*Store)(nil)

func New(ctx context.Context, dataSourceName string) (*Store, error) {
	if strings.TrimSpace(dataSourceName) == "" {
		return nil, errors.New("PostgreSQL data source name is required")
	}
	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open reconciliation store: %w", err)
	}
	database.SetMaxOpenConns(20)
	database.SetMaxIdleConns(5)
	database.SetConnMaxLifetime(30 * time.Minute)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("connect reconciliation store: %w", err)
	}
	if _, err := database.ExecContext(ctx, schema); err != nil {
		database.Close()
		return nil, fmt.Errorf("initialize reconciliation store: %w", err)
	}
	return &Store{database: database}, nil
}

func (store *Store) Close() error {
	if store == nil || store.database == nil {
		return nil
	}
	return store.database.Close()
}

// UpsertDesired durably records the latest reviewed input and its work
// notification in one transaction. recordedAt is the ordering authority;
// generation breaks exact-time ties without assuming a numeric format.
func (store *Store) UpsertDesired(ctx context.Context, snapshot domain.DesiredSnapshot) error {
	if _, err := (domain.Input{Desired: &snapshot}).Digest(); err != nil {
		return fmt.Errorf("validate desired snapshot: %w", err)
	}
	transaction, err := store.begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(ctx, `
INSERT INTO conduit_reconciliation_desired
    (environment, installation, generation, target_digest, payload_digest, payload, recorded_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (environment, installation) DO UPDATE SET
    generation = EXCLUDED.generation,
    target_digest = EXCLUDED.target_digest,
    payload_digest = EXCLUDED.payload_digest,
    payload = EXCLUDED.payload,
    recorded_at = EXCLUDED.recorded_at
WHERE EXCLUDED.recorded_at > conduit_reconciliation_desired.recorded_at
   OR (EXCLUDED.recorded_at = conduit_reconciliation_desired.recorded_at
       AND EXCLUDED.generation > conduit_reconciliation_desired.generation)`,
		snapshot.Key.Environment, snapshot.Key.Installation, snapshot.Generation, snapshot.TargetDigest,
		snapshot.PayloadDigest, []byte(snapshot.Payload), snapshot.RecordedAt.UTC())
	if err != nil {
		return fmt.Errorf("upsert desired snapshot: %w", err)
	}
	changed, err := affected(result)
	if err != nil {
		return err
	}
	if changed {
		if err := enqueue(ctx, transaction, snapshot.Key, snapshot.RecordedAt.UTC(), true); err != nil {
			return fmt.Errorf("enqueue desired snapshot: %w", err)
		}
	}
	if !changed {
		// A duplicate event still repairs a notification lost after its input was
		// committed, but does not reset an exhausted retry budget.
		if err := enqueue(ctx, transaction, snapshot.Key, snapshot.RecordedAt.UTC(), false); err != nil {
			return fmt.Errorf("coalesce desired notification: %w", err)
		}
	}
	return commit(transaction, "desired snapshot")
}

// UpsertObservation orders reports by observedAt and atomically notifies the
// same stable installation key.
func (store *Store) UpsertObservation(ctx context.Context, observation domain.Observation) error {
	if _, err := (domain.Input{Observation: &observation}).Digest(); err != nil {
		return fmt.Errorf("validate observation: %w", err)
	}
	transaction, err := store.begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(ctx, `
INSERT INTO conduit_reconciliation_observations
    (environment, installation, generation, target_digest, complete, observed_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (environment, installation) DO UPDATE SET
    generation = EXCLUDED.generation,
    target_digest = EXCLUDED.target_digest,
    complete = EXCLUDED.complete,
    observed_at = EXCLUDED.observed_at
WHERE EXCLUDED.observed_at > conduit_reconciliation_observations.observed_at
   OR (EXCLUDED.observed_at = conduit_reconciliation_observations.observed_at
       AND (EXCLUDED.generation, EXCLUDED.target_digest, EXCLUDED.complete::text) >
           (conduit_reconciliation_observations.generation,
            conduit_reconciliation_observations.target_digest,
            conduit_reconciliation_observations.complete::text))`,
		observation.Key.Environment, observation.Key.Installation, observation.Generation,
		observation.TargetDigest, observation.Complete, observation.ObservedAt.UTC())
	if err != nil {
		return fmt.Errorf("upsert observation: %w", err)
	}
	changed, err := affected(result)
	if err != nil {
		return err
	}
	if err := enqueue(ctx, transaction, observation.Key, observation.ObservedAt.UTC(), changed); err != nil {
		return fmt.Errorf("enqueue observation: %w", err)
	}
	return commit(transaction, "observation")
}

func (store *Store) LeaseWork(
	ctx context.Context, now time.Time, duration time.Duration,
) (ports.WorkLease, bool, error) {
	if duration <= 0 {
		return ports.WorkLease{}, false, errors.New("work lease duration must be positive")
	}
	token, err := randomToken("rcw_")
	if err != nil {
		return ports.WorkLease{}, false, err
	}
	expiresAt := now.UTC().Add(duration)
	row := store.database.QueryRowContext(ctx, `
WITH candidate AS (
    SELECT environment, installation
    FROM conduit_reconciliation_work
    WHERE exhausted = false AND available_at <= $1
      AND (lease_token IS NULL OR lease_expires_at <= $1)
    ORDER BY available_at, environment, installation
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE conduit_reconciliation_work AS work
SET lease_token = $2, lease_expires_at = $3, attempt = work.attempt + 1
FROM candidate
WHERE work.environment = candidate.environment AND work.installation = candidate.installation
RETURNING work.environment, work.installation, work.revision, work.attempt`, now.UTC(), token, expiresAt)
	lease := ports.WorkLease{Token: token, ExpiresAt: expiresAt}
	var attempt int64
	if err := row.Scan(&lease.Key.Environment, &lease.Key.Installation, &lease.Revision, &attempt); errors.Is(err, sql.ErrNoRows) {
		return ports.WorkLease{}, false, nil
	} else if err != nil {
		return ports.WorkLease{}, false, fmt.Errorf("lease reconciliation work: %w", err)
	}
	if attempt < 1 || attempt > int64(^uint32(0)) {
		return ports.WorkLease{}, false, errors.New("reconciliation work attempt overflow")
	}
	lease.Attempt = uint32(attempt)
	return lease, true, nil
}

func (store *Store) LoadInput(ctx context.Context, key domain.Key) (domain.Input, error) {
	if err := key.Validate(); err != nil {
		return domain.Input{}, err
	}
	input := domain.Input{}
	desired := domain.DesiredSnapshot{Key: key}
	err := store.database.QueryRowContext(ctx, `
SELECT generation, target_digest, payload_digest, payload, recorded_at
FROM conduit_reconciliation_desired WHERE environment = $1 AND installation = $2`,
		key.Environment, key.Installation).Scan(&desired.Generation, &desired.TargetDigest, &desired.PayloadDigest,
		&desired.Payload, &desired.RecordedAt)
	if err == nil {
		input.Desired = &desired
	} else if !errors.Is(err, sql.ErrNoRows) {
		return domain.Input{}, fmt.Errorf("load desired snapshot: %w", err)
	}
	observation := domain.Observation{Key: key}
	err = store.database.QueryRowContext(ctx, `
SELECT generation, target_digest, complete, observed_at
FROM conduit_reconciliation_observations WHERE environment = $1 AND installation = $2`,
		key.Environment, key.Installation).Scan(&observation.Generation, &observation.TargetDigest,
		&observation.Complete, &observation.ObservedAt)
	if err == nil {
		input.Observation = &observation
	} else if !errors.Is(err, sql.ErrNoRows) {
		return domain.Input{}, fmt.Errorf("load observation: %w", err)
	}
	if _, err := input.Digest(); err != nil {
		return domain.Input{}, fmt.Errorf("validate stored reconciliation input: %w", err)
	}
	return input, nil
}

func (store *Store) CommitDecision(
	ctx context.Context, lease ports.WorkLease, decision domain.Decision, proposal *domain.Proposal,
) error {
	if err := validateDecision(lease, decision, proposal); err != nil {
		return err
	}
	decisionDocument, err := json.Marshal(decision)
	if err != nil {
		return fmt.Errorf("encode decision: %w", err)
	}
	var proposalDocument []byte
	if proposal != nil {
		proposalDocument, err = json.Marshal(proposal)
		if err != nil {
			return fmt.Errorf("encode proposal: %w", err)
		}
	}
	transaction, err := store.begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var currentRevision uint64
	err = transaction.QueryRowContext(ctx, `
SELECT revision FROM conduit_reconciliation_work
WHERE environment = $1 AND installation = $2 AND lease_token = $3
FOR UPDATE`, lease.Key.Environment, lease.Key.Installation, lease.Token).Scan(&currentRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("stale reconciliation work lease")
	}
	if err != nil {
		return fmt.Errorf("fence reconciliation work lease: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO conduit_reconciliation_decisions
    (id, environment, installation, input_digest, kind, decided_at, document)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO NOTHING`, decision.ID, decision.Key.Environment, decision.Key.Installation,
		decision.InputDigest, string(decision.Kind), decision.DecidedAt.UTC(), decisionDocument); err != nil {
		return fmt.Errorf("persist reconciliation decision: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO conduit_reconciliation_audit
    (decision_id, environment, installation, occurred_at, document)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (decision_id) DO NOTHING`, decision.ID, decision.Key.Environment, decision.Key.Installation,
		decision.DecidedAt.UTC(), decisionDocument); err != nil {
		return fmt.Errorf("append reconciliation audit: %w", err)
	}
	if proposal != nil {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO conduit_reconciliation_proposal_outbox
    (proposal_id, idempotency_key, environment, installation, available_at, document)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (proposal_id) DO NOTHING`, proposal.ID, proposal.IdempotencyKey,
			proposal.Key.Environment, proposal.Key.Installation, decision.DecidedAt.UTC(), proposalDocument); err != nil {
			return fmt.Errorf("persist reconciliation proposal outbox: %w", err)
		}
	}
	if currentRevision == lease.Revision {
		result, err := transaction.ExecContext(ctx, `
DELETE FROM conduit_reconciliation_work
WHERE environment = $1 AND installation = $2 AND lease_token = $3`,
			lease.Key.Environment, lease.Key.Installation, lease.Token)
		if err != nil {
			return fmt.Errorf("acknowledge reconciliation work: %w", err)
		}
		deleted, err := affected(result)
		if err != nil || !deleted {
			return errors.New("reconciliation work lease changed before acknowledgement")
		}
	} else {
		if _, err := transaction.ExecContext(ctx, `
UPDATE conduit_reconciliation_work
SET available_at = $4, lease_token = NULL, lease_expires_at = NULL
WHERE environment = $1 AND installation = $2 AND lease_token = $3`,
			lease.Key.Environment, lease.Key.Installation, lease.Token, decision.DecidedAt.UTC()); err != nil {
			return fmt.Errorf("release superseded reconciliation work: %w", err)
		}
	}
	return commit(transaction, "reconciliation decision")
}

func (store *Store) FailWork(
	ctx context.Context, lease ports.WorkLease, reason string, retryAt time.Time, exhausted bool,
) error {
	transaction, err := store.begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var revision uint64
	if err := transaction.QueryRowContext(ctx, `
SELECT revision FROM conduit_reconciliation_work
WHERE environment = $1 AND installation = $2 AND lease_token = $3
FOR UPDATE`, lease.Key.Environment, lease.Key.Installation, lease.Token).Scan(&revision); errors.Is(err, sql.ErrNoRows) {
		return errors.New("stale reconciliation work lease")
	} else if err != nil {
		return fmt.Errorf("fence failed reconciliation work: %w", err)
	}
	// A newer input owns a fresh retry budget and must remain runnable even if
	// the superseded attempt exhausted its own budget.
	if revision != lease.Revision {
		exhausted = false
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE conduit_reconciliation_work
SET available_at = $4, lease_token = NULL, lease_expires_at = NULL,
    exhausted = $5, last_error = $6
WHERE environment = $1 AND installation = $2 AND lease_token = $3`,
		lease.Key.Environment, lease.Key.Installation, lease.Token, retryAt.UTC(), exhausted, boundedReason(reason)); err != nil {
		return fmt.Errorf("record reconciliation work failure: %w", err)
	}
	return commit(transaction, "reconciliation work failure")
}

func (store *Store) RepairDroppedWork(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 {
		return 0, errors.New("repair batch size must be positive")
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT desired.environment, desired.installation
FROM conduit_reconciliation_desired AS desired
LEFT JOIN conduit_reconciliation_work AS work
  ON work.environment = desired.environment AND work.installation = desired.installation
WHERE work.environment IS NULL
ORDER BY desired.environment, desired.installation
LIMIT $1`, limit)
	if err != nil {
		return 0, fmt.Errorf("list reconciliation repair candidates: %w", err)
	}
	keys := []domain.Key{}
	for rows.Next() {
		var key domain.Key
		if err := rows.Scan(&key.Environment, &key.Installation); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan reconciliation repair candidate: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close reconciliation repair candidates: %w", err)
	}
	repaired := 0
	for _, key := range keys {
		input, err := store.LoadInput(ctx, key)
		if err != nil {
			return repaired, err
		}
		digest, err := input.Digest()
		if err != nil {
			return repaired, err
		}
		var latest string
		err = store.database.QueryRowContext(ctx, `
SELECT input_digest FROM conduit_reconciliation_decisions
WHERE environment = $1 AND installation = $2
ORDER BY decided_at DESC, id DESC LIMIT 1`, key.Environment, key.Installation).Scan(&latest)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return repaired, fmt.Errorf("read latest reconciliation decision: %w", err)
		}
		if latest == digest {
			continue
		}
		result, err := store.database.ExecContext(ctx, `
INSERT INTO conduit_reconciliation_work
    (environment, installation, available_at, revision)
VALUES ($1, $2, $3, 1)
ON CONFLICT (environment, installation) DO NOTHING`, key.Environment, key.Installation, now.UTC())
		if err != nil {
			return repaired, fmt.Errorf("repair reconciliation work: %w", err)
		}
		inserted, err := affected(result)
		if err != nil {
			return repaired, err
		}
		if inserted {
			repaired++
		}
	}
	return repaired, nil
}

func (store *Store) LeaseProposal(
	ctx context.Context, now time.Time, duration time.Duration,
) (ports.ProposalLease, bool, error) {
	if duration <= 0 {
		return ports.ProposalLease{}, false, errors.New("proposal lease duration must be positive")
	}
	token, err := randomToken("rco_")
	if err != nil {
		return ports.ProposalLease{}, false, err
	}
	expiresAt := now.UTC().Add(duration)
	var document []byte
	var attempt int64
	err = store.database.QueryRowContext(ctx, `
WITH candidate AS (
    SELECT proposal_id FROM conduit_reconciliation_proposal_outbox
    WHERE exhausted = false AND available_at <= $1
      AND (lease_token IS NULL OR lease_expires_at <= $1)
    ORDER BY available_at, proposal_id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE conduit_reconciliation_proposal_outbox AS outbox
SET lease_token = $2, lease_expires_at = $3, attempt = outbox.attempt + 1
FROM candidate
WHERE outbox.proposal_id = candidate.proposal_id
RETURNING outbox.document, outbox.attempt`, now.UTC(), token, expiresAt).Scan(&document, &attempt)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.ProposalLease{}, false, nil
	}
	if err != nil {
		return ports.ProposalLease{}, false, fmt.Errorf("lease reconciliation proposal: %w", err)
	}
	var proposal domain.Proposal
	if err := json.Unmarshal(document, &proposal); err != nil {
		return ports.ProposalLease{}, false, fmt.Errorf("decode reconciliation proposal: %w", err)
	}
	if attempt < 1 || attempt > int64(^uint32(0)) {
		return ports.ProposalLease{}, false, errors.New("reconciliation proposal attempt overflow")
	}
	return ports.ProposalLease{Proposal: proposal, Token: token, Attempt: uint32(attempt), ExpiresAt: expiresAt}, true, nil
}

func (store *Store) CompleteProposal(
	ctx context.Context, lease ports.ProposalLease, receipt domain.PlanReceipt,
) error {
	if receipt.ProposalID != lease.Proposal.ID || strings.TrimSpace(receipt.PlanID) == "" || receipt.CreatedAt.IsZero() {
		return errors.New("proposal receipt does not match its lease")
	}
	document, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("encode proposal receipt: %w", err)
	}
	transaction, err := store.begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO conduit_reconciliation_plan_receipts
    (proposal_id, plan_id, created_at, document)
VALUES ($1, $2, $3, $4)
ON CONFLICT (proposal_id) DO UPDATE SET
    plan_id = EXCLUDED.plan_id, created_at = EXCLUDED.created_at, document = EXCLUDED.document
WHERE conduit_reconciliation_plan_receipts.plan_id = EXCLUDED.plan_id`,
		receipt.ProposalID, receipt.PlanID, receipt.CreatedAt.UTC(), document); err != nil {
		return fmt.Errorf("persist reconciliation plan receipt: %w", err)
	}
	var persistedPlanID string
	if err := transaction.QueryRowContext(ctx, `
SELECT plan_id FROM conduit_reconciliation_plan_receipts WHERE proposal_id = $1`,
		receipt.ProposalID).Scan(&persistedPlanID); err != nil || persistedPlanID != receipt.PlanID {
		return errors.New("proposal already has a different plan receipt")
	}
	result, err := transaction.ExecContext(ctx, `
DELETE FROM conduit_reconciliation_proposal_outbox
WHERE proposal_id = $1 AND lease_token = $2`, lease.Proposal.ID, lease.Token)
	if err != nil {
		return fmt.Errorf("acknowledge reconciliation proposal: %w", err)
	}
	deleted, err := affected(result)
	if err != nil || !deleted {
		return errors.New("stale reconciliation proposal lease")
	}
	return commit(transaction, "proposal receipt")
}

func (store *Store) FailProposal(
	ctx context.Context, lease ports.ProposalLease, reason string, retryAt time.Time, exhausted bool,
) error {
	result, err := store.database.ExecContext(ctx, `
UPDATE conduit_reconciliation_proposal_outbox
SET available_at = $3, lease_token = NULL, lease_expires_at = NULL,
    exhausted = $4, last_error = $5
WHERE proposal_id = $1 AND lease_token = $2`, lease.Proposal.ID, lease.Token,
		retryAt.UTC(), exhausted, boundedReason(reason))
	if err != nil {
		return fmt.Errorf("record reconciliation proposal failure: %w", err)
	}
	updated, err := affected(result)
	if err != nil || !updated {
		return errors.New("stale reconciliation proposal lease")
	}
	return nil
}

func (store *Store) begin(ctx context.Context) (*sql.Tx, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("reconciliation store is closed")
	}
	transaction, err := store.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin reconciliation transaction: %w", err)
	}
	return transaction, nil
}

func enqueue(ctx context.Context, transaction *sql.Tx, key domain.Key, availableAt time.Time, reset bool) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO conduit_reconciliation_work
    (environment, installation, available_at, revision)
VALUES ($1, $2, $3, 1)
ON CONFLICT (environment, installation) DO UPDATE SET
    available_at = LEAST(conduit_reconciliation_work.available_at, EXCLUDED.available_at),
    revision = conduit_reconciliation_work.revision + 1,
    attempt = CASE WHEN $4 THEN 0 ELSE conduit_reconciliation_work.attempt END,
    exhausted = CASE WHEN $4 THEN false ELSE conduit_reconciliation_work.exhausted END,
    last_error = CASE WHEN $4 THEN '' ELSE conduit_reconciliation_work.last_error END`,
		key.Environment, key.Installation, availableAt, reset)
	return err
}

func validateDecision(lease ports.WorkLease, decision domain.Decision, proposal *domain.Proposal) error {
	if err := lease.Key.Validate(); err != nil || strings.TrimSpace(lease.Token) == "" || lease.Revision == 0 || lease.Attempt == 0 {
		return errors.New("decision requires a valid work lease")
	}
	if decision.Key != lease.Key || strings.TrimSpace(decision.ID) == "" || strings.TrimSpace(decision.InputDigest) == "" ||
		decision.DecidedAt.IsZero() || decision.Attempt != lease.Attempt {
		return errors.New("decision does not match its work lease")
	}
	if proposal != nil && (proposal.Key != decision.Key || proposal.InputDigest != decision.InputDigest ||
		strings.TrimSpace(proposal.ID) == "" || strings.TrimSpace(proposal.IdempotencyKey) == "") {
		return errors.New("proposal does not match its decision")
	}
	return nil
}

func randomToken(prefix string) (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("create reconciliation lease token: %w", err)
	}
	return prefix + hex.EncodeToString(buffer), nil
}

func affected(result sql.Result) (bool, error) {
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("confirm reconciliation write: %w", err)
	}
	return count > 0, nil
}

func commit(transaction *sql.Tx, operation string) error {
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", operation, err)
	}
	return nil
}

func boundedReason(reason string) string {
	const maximum = 4096
	if len(reason) <= maximum {
		return reason
	}
	return reason[:maximum]
}
