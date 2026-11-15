// Package ports declares storage, current authorization and contract validation.
package ports

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/liveevents/domain"
)

// Journal commits the complete callback result durably before success. A callback
// failure leaves state unchanged. Implementations serialize concurrent updates.
type Journal interface {
	Update(context.Context, func(*domain.State) error) error
	Snapshot(context.Context) (domain.State, error)
}

// Authorizer must re-evaluate native identity, grants and delegation on each call.
// Snapshot refuses callers without stream permission before a subscriber exists.
type Authorizer interface {
	Snapshot(context.Context) (domain.Identity, error)
	Visible(context.Context, domain.Identity, domain.Subject) (bool, error)
}

// Validator applies the committed JSON contract, including payload allowlists.
type Validator interface{ Envelope(domain.Envelope) error }

// Readiness refuses a public stream while required committed-source adapters
// are absent, unavailable or in a failed publication state.
type Readiness interface{ Check(context.Context) error }

// CommittedSource reads private native outboxes after their owning state commits.
// Positions are the delivery journal's durable producer checkpoints. A retained
// history gap must return ErrSourceRetentionGap only after verifying a complete
// native retained window. That signal atomically retires all public streams and
// source checkpoints, forcing authorized catch-up before a new baseline.
// Generic ErrReset/corruption/unavailability never discard checkpoints.
// Successful delivery must persist checkpoints before the next page. This port
// conveys no subscriber authority.
type CommittedSource interface {
	ReadCommittedEvents(context.Context, map[string]uint64, int) ([]domain.Record, error)
}
