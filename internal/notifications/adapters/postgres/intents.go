package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// IntentRecorder belongs to a caller-owned source transaction. It performs no
// independent commit, authority lookup, inbox admission or transport call.
// Consent candidates are captured with the source write, using the SAME SQL
// transaction; later preference edits cannot widen that original snapshot.
type IntentRecorder struct{ transaction *sql.Tx }

var _ ports.Emitter = (*IntentRecorder)(nil)
var _ ports.IntentQueue = (*Store)(nil)

func NewIntentRecorder(transaction *sql.Tx) (*IntentRecorder, error) {
	if transaction == nil {
		return nil, errors.New("a source SQL transaction is required for notification intent")
	}
	return &IntentRecorder{transaction: transaction}, nil
}

func (recorder *IntentRecorder) Emit(ctx context.Context, event domain.Event) error {
	if err := event.Validate(); err != nil {
		return fmt.Errorf("refuse notification intent: %w", err)
	}
	rows, err := recorder.transaction.QueryContext(ctx, `
SELECT document FROM conduit_notification_preferences
WHERE resource_rid = $1
ORDER BY owner_kind, owner_id, channel, id`, event.ResourceRID)
	if err != nil {
		return fmt.Errorf("read notification consent in source transaction: %w", err)
	}
	defer rows.Close()
	preferences := []domain.Preference{}
	for rows.Next() {
		var document []byte
		var preference domain.Preference
		if err := rows.Scan(&document); err != nil {
			return fmt.Errorf("read notification consent: %w", err)
		}
		if err := json.Unmarshal(document, &preference); err != nil {
			return fmt.Errorf("decode notification consent: %w", err)
		}
		preferences = append(preferences, preference)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read notification consent: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close notification consent read: %w", err)
	}
	intent := domain.Intent{Event: event, Candidates: []domain.ConsentCandidate{}}
	for _, entry := range domain.Fan(event, preferences) {
		intent.Candidates = append(intent.Candidates, domain.ConsentCandidate{Recipient: entry.Recipient, PreferenceID: entry.PreferenceID})
	}
	if err := intent.Validate(); err != nil {
		return err
	}
	document, err := json.Marshal(intent)
	if err != nil {
		return fmt.Errorf("encode notification intent: %w", err)
	}
	// A duplicate keeps the FIRST event and consent set, even if that set is
	// empty. ON CONFLICT must not update the document or reset completed work.
	_, err = recorder.transaction.ExecContext(ctx, `
INSERT INTO conduit_notification_intents (event_id, document, available_at)
VALUES ($1, $2, $3) ON CONFLICT (event_id) DO NOTHING`, event.ID, document, event.OccurredAt.UTC())
	if err != nil {
		return fmt.Errorf("persist notification intent with source: %w", err)
	}
	return nil
}

func (store *Store) LeaseIntent(ctx context.Context, now time.Time, duration time.Duration) (ports.IntentLease, bool, error) {
	if store == nil || store.database == nil {
		return ports.IntentLease{}, false, errors.New("notification store is closed")
	}
	if duration <= 0 {
		return ports.IntentLease{}, false, errors.New("notification intent lease duration must be positive")
	}
	token, err := randomToken("nil_")
	if err != nil {
		return ports.IntentLease{}, false, err
	}
	expiresAt := now.UTC().Add(duration)
	var document []byte
	var attempt int64
	err = store.database.QueryRowContext(ctx, `
WITH candidate AS (
    SELECT event_id FROM conduit_notification_intents
    WHERE completed_at IS NULL AND exhausted = false AND available_at <= $1
      AND (lease_token IS NULL OR lease_expires_at <= $1)
    ORDER BY available_at, event_id
    FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE conduit_notification_intents AS intent
SET lease_token = $2, lease_expires_at = $3, attempt = intent.attempt + 1
FROM candidate WHERE intent.event_id = candidate.event_id
RETURNING intent.document, intent.attempt`, now.UTC(), token, expiresAt).Scan(&document, &attempt)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.IntentLease{}, false, nil
	}
	if err != nil {
		return ports.IntentLease{}, false, fmt.Errorf("lease notification intent: %w", err)
	}
	var intent domain.Intent
	if err := json.Unmarshal(document, &intent); err != nil {
		return ports.IntentLease{}, false, fmt.Errorf("decode stored notification intent: %w", err)
	}
	if attempt < 1 || attempt > int64(^uint32(0)) {
		return ports.IntentLease{}, false, errors.New("notification intent attempt overflow")
	}
	return ports.IntentLease{Intent: intent, Token: token, Attempt: uint32(attempt), ExpiresAt: expiresAt}, true, nil
}

func (store *Store) CompleteIntent(ctx context.Context, lease ports.IntentLease, completedAt time.Time) error {
	if store == nil || store.database == nil {
		return errors.New("notification store is closed")
	}
	result, err := store.database.ExecContext(ctx, `
UPDATE conduit_notification_intents
SET completed_at = $3, lease_token = NULL, lease_expires_at = NULL, last_error = ''
WHERE event_id = $1 AND lease_token = $2 AND completed_at IS NULL AND exhausted = false`,
		lease.Intent.Event.ID, lease.Token, completedAt.UTC())
	return intentWriteResult(result, err)
}

func (store *Store) FailIntent(ctx context.Context, lease ports.IntentLease, reason string, retryAt time.Time, exhausted bool) error {
	if store == nil || store.database == nil {
		return errors.New("notification store is closed")
	}
	result, err := store.database.ExecContext(ctx, `
UPDATE conduit_notification_intents
SET available_at = $3, lease_token = NULL, lease_expires_at = NULL, exhausted = $4, last_error = $5
WHERE event_id = $1 AND lease_token = $2 AND completed_at IS NULL AND exhausted = false`,
		lease.Intent.Event.ID, lease.Token, retryAt.UTC(), exhausted, boundedReason(reason))
	return intentWriteResult(result, err)
}

func intentWriteResult(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("update notification intent: %w", err)
	}
	updated, err := affected(result)
	if err != nil {
		return err
	}
	if !updated {
		return errors.New("stale notification intent lease")
	}
	return nil
}

// IntentStats retains the difference between work still owed, work admitted,
// and work whose bounded retries exhausted. Exhausted records are not deleted.
type IntentStats struct {
	Pending   int64
	Completed int64
	Exhausted int64
}

func (store *Store) Intents(ctx context.Context) (IntentStats, error) {
	if store == nil || store.database == nil {
		return IntentStats{}, errors.New("notification store is closed")
	}
	var stats IntentStats
	err := store.database.QueryRowContext(ctx, `
SELECT count(*) FILTER (WHERE completed_at IS NULL AND exhausted = false),
       count(*) FILTER (WHERE completed_at IS NOT NULL),
       count(*) FILTER (WHERE exhausted = true)
FROM conduit_notification_intents`).Scan(&stats.Pending, &stats.Completed, &stats.Exhausted)
	if err != nil {
		return IntentStats{}, fmt.Errorf("read notification intent statistics: %w", err)
	}
	return stats, nil
}
