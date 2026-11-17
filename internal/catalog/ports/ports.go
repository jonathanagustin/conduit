// Package ports declares what Catalog needs from the rest of the system, and
// nothing more.
//
// There are two, and both are outbound. PublicationRepository is persistence: a
// Publication is a coordinate the catalogue mints and must never mint twice, so
// the store is the thing that has to enforce immutability and the domain
// cannot. RecallOpenings is announcement: a recall withdraws software that is
// running somewhere, and the surface that accepts one must not have to know
// what a notification is in order to say so.
package ports

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/catalog/domain"
)

// PublicationRepository records where a Release was published.
//
// Implementations enforce coordinate immutability: a Publication for a
// product/version/registry triple that already exists is not overwritten. That
// is a store-level guarantee because it is a uniqueness guarantee, and no
// amount of checking before the write can make one out of two operations.
type PublicationRepository interface {
	CreateProductPublication(ctx context.Context, publication domain.Publication) (domain.Publication, error)
	ListProductPublications(ctx context.Context, product string) ([]domain.Publication, error)
}

// RecallOpenings is told which recalls are in force for an Environment, so that
// something outside this context can tell a person one opened.
//
// A port rather than a direct call into notifications, for the reason
// reconciliation's Escalations states about itself: the caller must not learn
// what a notification is. What a caller knows is that these recalls are in
// force for this Environment as of now. Whether that becomes an inbox entry, an
// email or nothing at all is the adapter's problem, and every deployment
// without a notification store passes no implementation and raises nothing.
//
// The method takes the whole set rather than one opening, and that is the
// load-bearing part of the shape. Nothing in Conduit stores which recalls it
// has already announced -- recalls are a field of a DesiredState, submitted
// whole every time -- so no caller can tell an opening from a restatement. The
// implementation is what makes the difference, by deriving an identity the
// notification store deduplicates on. That has to be durable rather than a
// replica's memory, because every Hub replica sees the same submissions and a
// per-process memory would announce the same recall once per replica.
type RecallOpenings interface {
	// RecallsInForce announces every recall in the set that has not been
	// announced before. Reverted recalls withdraw nothing and are skipped.
	RecallsInForce(ctx context.Context, environment string, recalls []domain.Recall) error
}

// PromotionFailures is told about every stored promotion evaluation, so that
// something outside this context can tell a person when one failed.
//
// The caller hands over each evaluation rather than deciding which ones
// matter, for the reason RecallOpenings takes the whole set: what counts as a
// failure worth a person's attention is the implementation's rule, and a
// caller that filtered first would hold a second copy of it. Called after the
// evaluation is stored; the caller drops the error, because the record exists
// by then.
type PromotionFailures interface {
	PromotionEvaluated(ctx context.Context, record domain.PromotionRecord) error
}
