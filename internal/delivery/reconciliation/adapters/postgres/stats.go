package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Stats is what an operator needs to tell a working Hub from a wedged one.
//
// The distinction the Hub cannot currently make about itself: a queue that has
// drained and a queue that has stopped draining look identical from outside.
// Both replicas answer their probes, both hold their leases, and depth alone
// does not separate them -- a fleet at steady state and a fleet whose worker is
// stuck both sit at some number.
//
// OldestPendingAge is what separates them, which is why it is here rather than
// the depth alone. Work that is due and has not been taken is the signal; its
// age is how long nobody took it. A depth of two hundred with an age of a second
// is a busy Hub. A depth of three with an age of an hour is a broken one.
type Stats struct {
	// WorkPending is reconciliation work due now and not leased.
	WorkPending int
	// WorkLeased is work a replica currently holds. Two replicas both showing
	// zero leased while pending climbs is the wedge.
	WorkLeased int
	// WorkExhausted is work that has run out of retries. It never drains on its
	// own, so any non-zero value is a standing request for a human.
	WorkExhausted int
	// OldestPendingAge is how long the oldest due-and-unleased item has waited.
	// Zero when nothing is pending.
	OldestPendingAge time.Duration

	// The proposal outbox, same three questions. It is a separate queue with a
	// separate lease, so it wedges independently.
	OutboxPending   int
	OutboxLeased    int
	OutboxExhausted int
	// OldestOutboxAge is the equivalent lag signal for proposals.
	OldestOutboxAge time.Duration
}

// Stats reads both queues in one round trip.
//
// One query rather than six because a scrape must not turn into a transaction
// storm, and because counts read at different instants do not describe a single
// state -- pending and leased taken a second apart can both look wrong when
// neither was.
//
// `now` is a parameter rather than `now()` so a test can place the clock, and so
// the age is measured against the caller's notion of time rather than the
// database's. The two differ, and a lag metric computed against a clock nobody
// else uses is the kind of number that starts an argument during an incident.
func (store *Store) Stats(ctx context.Context, now time.Time) (Stats, error) {
	const query = `
SELECT
    (SELECT count(*) FROM conduit_reconciliation_work
       WHERE exhausted = false AND available_at <= $1
         AND (lease_expires_at IS NULL OR lease_expires_at <= $1)),
    (SELECT count(*) FROM conduit_reconciliation_work
       WHERE exhausted = false AND lease_expires_at > $1),
    (SELECT count(*) FROM conduit_reconciliation_work WHERE exhausted = true),
    (SELECT min(available_at) FROM conduit_reconciliation_work
       WHERE exhausted = false AND available_at <= $1
         AND (lease_expires_at IS NULL OR lease_expires_at <= $1)),
    (SELECT count(*) FROM conduit_reconciliation_proposal_outbox
       WHERE exhausted = false AND available_at <= $1
         AND (lease_expires_at IS NULL OR lease_expires_at <= $1)),
    (SELECT count(*) FROM conduit_reconciliation_proposal_outbox
       WHERE exhausted = false AND lease_expires_at > $1),
    (SELECT count(*) FROM conduit_reconciliation_proposal_outbox WHERE exhausted = true),
    (SELECT min(available_at) FROM conduit_reconciliation_proposal_outbox
       WHERE exhausted = false AND available_at <= $1
         AND (lease_expires_at IS NULL OR lease_expires_at <= $1))`

	var stats Stats
	var oldestWork, oldestOutbox sql.NullTime
	if err := store.database.QueryRowContext(ctx, query, now).Scan(
		&stats.WorkPending, &stats.WorkLeased, &stats.WorkExhausted, &oldestWork,
		&stats.OutboxPending, &stats.OutboxLeased, &stats.OutboxExhausted, &oldestOutbox,
	); err != nil {
		return Stats{}, fmt.Errorf("read reconciliation stats: %w", err)
	}
	stats.OldestPendingAge = age(now, oldestWork)
	stats.OldestOutboxAge = age(now, oldestOutbox)
	return stats, nil
}

// age is clamped at zero. A row scheduled for the future is excluded by the
// query, but clock skew between the Hub and the database can still produce a
// negative interval, and a negative lag rendered into a gauge reads as a
// corrupted metric rather than as the sub-second skew it is.
func age(now time.Time, oldest sql.NullTime) time.Duration {
	if !oldest.Valid {
		return 0
	}
	if elapsed := now.Sub(oldest.Time); elapsed > 0 {
		return elapsed
	}
	return 0
}
