package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

var _ ports.DeliveryQueue = (*Store)(nil)

// LeaseDelivery claims the oldest due, undelivered, non-terminal inbox entry.
//
// The query is the reconciliation proposal outbox's, transposed onto the inbox:
// a CTE selects one candidate FOR UPDATE SKIP LOCKED, the UPDATE stamps a lease
// token and increments the attempt, and the whole thing is one statement so two
// replicas polling together take two different entries rather than one entry
// twice. SKIP LOCKED is what makes that true under concurrency; the partial
// index makes it cheap once most of the inbox is delivered.
//
// The event document is joined in the same statement rather than read
// afterwards. A second round trip would be a second point at which the process
// can die holding a lease over an entry it never rendered.
func (store *Store) LeaseDelivery(
	ctx context.Context, now time.Time, duration time.Duration,
) (ports.DeliveryLease, bool, error) {
	if store == nil || store.database == nil {
		return ports.DeliveryLease{}, false, errors.New("notification store is closed")
	}
	if duration <= 0 {
		return ports.DeliveryLease{}, false, errors.New("notification delivery lease duration must be positive")
	}
	token, err := randomToken("ndl_")
	if err != nil {
		return ports.DeliveryLease{}, false, err
	}
	expiresAt := now.UTC().Add(duration)
	var (
		ownerKind    string
		ownerID      string
		channel      string
		preferenceID string
		readAt       sql.NullTime
		document     []byte
		attempt      int64
	)
	err = store.database.QueryRowContext(ctx, `
WITH candidate AS (
    SELECT event_id, owner_kind, owner_id, channel
    FROM conduit_notification_inbox
    WHERE delivery_available_at IS NOT NULL
      AND delivered_at IS NULL
      AND delivery_exhausted = false
      AND delivery_available_at <= $1
      AND (delivery_lease_token IS NULL OR delivery_lease_expires_at <= $1)
    ORDER BY delivery_available_at, event_id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE conduit_notification_inbox AS inbox
SET delivery_lease_token = $2,
    delivery_lease_expires_at = $3,
    delivery_attempt = inbox.delivery_attempt + 1
FROM candidate, conduit_notification_events AS event
WHERE inbox.event_id = candidate.event_id
  AND inbox.owner_kind = candidate.owner_kind
  AND inbox.owner_id = candidate.owner_id
  AND inbox.channel = candidate.channel
  AND event.id = candidate.event_id
RETURNING inbox.owner_kind, inbox.owner_id, inbox.channel, inbox.preference_id,
          inbox.read_at, event.document, inbox.delivery_attempt`,
		now.UTC(), token, expiresAt,
	).Scan(&ownerKind, &ownerID, &channel, &preferenceID, &readAt, &document, &attempt)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.DeliveryLease{}, false, nil
	}
	if err != nil {
		return ports.DeliveryLease{}, false, fmt.Errorf("lease notification delivery: %w", err)
	}
	var event domain.Event
	if err := json.Unmarshal(document, &event); err != nil {
		return ports.DeliveryLease{}, false, fmt.Errorf("decode stored notification event: %w", err)
	}
	if attempt < 1 || attempt > int64(^uint32(0)) {
		return ports.DeliveryLease{}, false, errors.New("notification delivery attempt overflow")
	}
	entry := domain.InboxEntry{
		Recipient: domain.Recipient{
			Owner:   domain.Owner{Kind: domain.PrincipalKind(ownerKind), ID: ownerID},
			Channel: domain.Channel(channel),
		},
		Event:        event,
		PreferenceID: preferenceID,
	}
	if readAt.Valid {
		entry.ReadAt = readAt.Time.UTC()
	}
	return ports.DeliveryLease{
		Entry: entry, Token: token, Attempt: uint32(attempt), ExpiresAt: expiresAt,
	}, true, nil
}

// CompleteDelivery marks the leased entry delivered and clears the lease.
//
// Fenced on the lease token, so a replica whose lease expired while it was
// sending cannot report success over the replica that has since taken the
// entry. It updates rather than deletes: the inbox entry is what the recipient
// reads, and deleting it on delivery would erase the notification in the act of
// delivering it.
func (store *Store) CompleteDelivery(
	ctx context.Context, lease ports.DeliveryLease, deliveredAt time.Time,
) error {
	if store == nil || store.database == nil {
		return errors.New("notification store is closed")
	}
	recipient := lease.Entry.Recipient
	result, err := store.database.ExecContext(ctx, `
UPDATE conduit_notification_inbox
SET delivered_at = $5,
    delivery_lease_token = NULL,
    delivery_lease_expires_at = NULL,
    delivery_error = ''
WHERE event_id = $1 AND owner_kind = $2 AND owner_id = $3 AND channel = $4
  AND delivery_lease_token = $6`,
		lease.Entry.Event.ID, string(recipient.Owner.Kind), recipient.Owner.ID,
		string(recipient.Channel), deliveredAt.UTC(), lease.Token)
	if err != nil {
		return fmt.Errorf("record notification delivery: %w", err)
	}
	updated, err := affected(result)
	if err != nil {
		return err
	}
	if !updated {
		return errors.New("stale notification delivery lease")
	}
	return nil
}

// FailDelivery records why an attempt failed, when to try again, and whether
// there is any point.
//
// exhausted is the durable terminal state, and it is durable in the same sense
// the outbox's is: the lease query filters on it, so an exhausted entry is
// never claimed again and keeps the reason it stopped. That is what makes
// "nobody was told, and here is why" answerable later, which is the whole
// difference between a failure and a silence.
func (store *Store) FailDelivery(
	ctx context.Context, lease ports.DeliveryLease, reason string, retryAt time.Time, exhausted bool,
) error {
	if store == nil || store.database == nil {
		return errors.New("notification store is closed")
	}
	recipient := lease.Entry.Recipient
	result, err := store.database.ExecContext(ctx, `
UPDATE conduit_notification_inbox
SET delivery_available_at = $5,
    delivery_lease_token = NULL,
    delivery_lease_expires_at = NULL,
    delivery_exhausted = $6,
    delivery_error = $7
WHERE event_id = $1 AND owner_kind = $2 AND owner_id = $3 AND channel = $4
  AND delivery_lease_token = $8`,
		lease.Entry.Event.ID, string(recipient.Owner.Kind), recipient.Owner.ID,
		string(recipient.Channel), retryAt.UTC(), exhausted, boundedReason(reason), lease.Token)
	if err != nil {
		return fmt.Errorf("record notification delivery failure: %w", err)
	}
	updated, err := affected(result)
	if err != nil {
		return err
	}
	if !updated {
		return errors.New("stale notification delivery lease")
	}
	return nil
}

// DeliveryStats is what an operator needs to tell a busy queue from a stuck
// one, in the same three numbers the reconciliation queues report.
type DeliveryStats struct {
	Pending   int64
	Delivered int64
	Exhausted int64
}

// Deliveries reports the queue's depth without leasing anything.
func (store *Store) Deliveries(ctx context.Context) (DeliveryStats, error) {
	if store == nil || store.database == nil {
		return DeliveryStats{}, errors.New("notification store is closed")
	}
	var stats DeliveryStats
	if err := store.database.QueryRowContext(ctx, `
SELECT
    count(*) FILTER (
        WHERE delivery_available_at IS NOT NULL AND delivered_at IS NULL AND delivery_exhausted = false),
    count(*) FILTER (WHERE delivered_at IS NOT NULL),
    count(*) FILTER (WHERE delivery_exhausted = true)
FROM conduit_notification_inbox`).Scan(&stats.Pending, &stats.Delivered, &stats.Exhausted); err != nil {
		return DeliveryStats{}, fmt.Errorf("read notification delivery statistics: %w", err)
	}
	return stats, nil
}

func randomToken(prefix string) (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("create notification lease token: %w", err)
	}
	return prefix + hex.EncodeToString(buffer), nil
}

func affected(result sql.Result) (bool, error) {
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("confirm notification write: %w", err)
	}
	return count > 0, nil
}

// boundedReason keeps one endpoint's verbose error from becoming an unbounded
// column. The cut is at the same 4 KiB the reconciliation store uses.
func boundedReason(reason string) string {
	const maximum = 4096
	if len(reason) <= maximum {
		return reason
	}
	return reason[:maximum]
}
