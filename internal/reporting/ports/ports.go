// Package ports declares what Reporting needs in order to build the operator's
// overview, and nothing more.
//
// Every method answers for one panel, and answers only with what this reader is
// allowed to see. That is deliberate: authorization is Governance's rule, and
// filtering after counting would let a panel report a number the reader cannot
// account for. An implementation filters first, and the domain counts what it
// is given.
//
// A method that returns an error leaves its panel UNAVAILABLE rather than
// empty. Refusing to answer and answering "nothing" are different facts and the
// overview shows them differently.
package ports

import (
	"context"
	"time"

	"github.com/jonathanagustin/conduit/internal/reporting/domain"
)

// OverviewSource is one consistent, already-authorized snapshot of the fleet.
//
// The methods are separate so that one unavailable store costs one panel, but
// they are one interface because they must describe the same moment. An
// implementation reads each underlying store once: two reads could disagree,
// and an overview that contradicts itself is worse than one with a gap.
type OverviewSource interface {
	// VisiblePlans returns the plans this reader may see, already classified
	// into Reporting's phases.
	VisiblePlans(ctx context.Context) ([]domain.PlanSummary, error)

	// VisibleInstallations returns the fleet as of now, with reports older than
	// staleAfter marked stale. It fails if either the plans or the reports
	// behind the fleet are unavailable, because a fleet built from one of them
	// is not a fleet.
	VisibleInstallations(
		ctx context.Context, staleAfter time.Duration, now time.Time,
	) ([]domain.InstallationSummary, error)

	// VisibleChangeRequests returns the change requests this reader may see.
	VisibleChangeRequests(ctx context.Context) ([]domain.ChangeRequestSummary, error)

	// VisiblePromotions returns the promotions this reader may see. A reader
	// without the platform-wide authority promotions are listed under gets an
	// error, so the panel reads UNAVAILABLE rather than falsely empty.
	VisiblePromotions(ctx context.Context) ([]domain.PromotionSummary, error)
}
