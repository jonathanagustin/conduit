package ports

import (
	"context"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

// AgentCredentialStore holds the Hub's deny-list for Agent credentials and the
// last certificate each Agent identity presented.
//
// The store answers; it does not decide. Whether a credential is refused is
// domain.AgentRevocation.Refuses, so the rule is one function a test can call
// rather than a WHERE clause per adapter.
//
// An error from Revocations must be treated by the caller as "the deny-list
// is unknown", never as "the deny-list is empty".
type AgentCredentialStore interface {
	// Revocations returns every revocation recorded for one Environment.
	Revocations(ctx context.Context, environment string) ([]domain.AgentRevocation, error)
	// PutRevocation writes a revocation under its identifier, replacing one
	// already there, and returns what it replaced.
	PutRevocation(ctx context.Context, revocation domain.AgentRevocation) (*domain.AgentRevocation, error)
	// DeleteRevocation removes one revocation and returns it, or
	// domain.ErrRevocationNotFound.
	DeleteRevocation(ctx context.Context, environment, id string) (domain.AgentRevocation, error)

	// RecordSighting keeps the latest certificate one Agent identity presented.
	RecordSighting(ctx context.Context, sighting domain.AgentCredentialSighting) error
	// Sightings returns the latest certificate of every Agent identity seen in
	// one Environment.
	Sightings(ctx context.Context, environment string) ([]domain.AgentCredentialSighting, error)
	// EarliestExpiry is the soonest NotAfter among Agent identities seen at or
	// after seenSince, and how many such identities there are. Zero agents
	// means there is nothing to report, not that something expires now.
	EarliestExpiry(ctx context.Context, seenSince time.Time) (notAfter time.Time, agents int, err error)
}
