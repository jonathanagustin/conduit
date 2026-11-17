package ports

import (
	"context"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

// EventRecorder admits an event and its already resolved audience. An intent
// consumer has no preference-read port: retry cannot discover new subscribers.
type EventRecorder interface {
	RecordEvent(context.Context, domain.Event, []domain.InboxEntry) error
}

type IntentLease struct {
	Intent    domain.Intent
	Token     string
	Attempt   uint32
	ExpiresAt time.Time
}

// IntentQueue retains source-committed work until admission succeeds or its
// bounded retry budget is visibly exhausted. No pending intent is truncated.
// Leases fence completion/failure by token, following DeliveryQueue's contract.
type IntentQueue interface {
	LeaseIntent(context.Context, time.Time, time.Duration) (IntentLease, bool, error)
	CompleteIntent(context.Context, IntentLease, time.Time) error
	FailIntent(context.Context, IntentLease, string, time.Time, bool) error
}
