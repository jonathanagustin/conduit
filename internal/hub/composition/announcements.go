package composition

import (
	"context"
	"errors"
	"log/slog"

	catalognotifications "github.com/jonathanagustin/conduit/internal/catalog/adapters/notifications"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	catalogports "github.com/jonathanagustin/conduit/internal/catalog/ports"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	plannotifications "github.com/jonathanagustin/conduit/internal/delivery/plan/adapters/notifications"
	planports "github.com/jonathanagustin/conduit/internal/delivery/plan/ports"
	governancenotifications "github.com/jonathanagustin/conduit/internal/governance/adapters/notifications"
	governanceports "github.com/jonathanagustin/conduit/internal/governance/ports"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/application"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// NewAnnouncements assembles the ports that tell somebody a change request is
// waiting, a grant changed, a Plan failed or a promotion was aborted -- or
// reports, with a zero value, that this deployment has nobody to tell.
//
// The same shape as NewRecallOpenings and for the same reasons: a nil store is
// REPOSITORY_KIND=file, which records no event and so raises none; and each
// port is wrapped in a logger, because internal/controlplane drops the error at
// every one of these sites on purpose and a broken notification store must not
// be silent in the place silence is the defect.
//
// NewHub shares these ports with the facade and the governance services used
// by its other surfaces. Transaction-local services stay silent; the owner of
// the transaction announces only after its writes have committed.
func NewAnnouncements(
	store notificationports.Store, logger *slog.Logger,
) (controlplane.Announcements, error) {
	if store == nil {
		return controlplane.Announcements{}, nil
	}
	if logger == nil {
		return controlplane.Announcements{}, errors.New("a logger is required: the surfaces that announce drop the error")
	}
	emitter, err := notifications.NewEmitter(store)
	if err != nil {
		return controlplane.Announcements{}, err
	}
	governance, err := governancenotifications.NewAnnouncements(emitter, notifications.SystemClock{}.Now)
	if err != nil {
		return controlplane.Announcements{}, err
	}
	plans, err := plannotifications.NewFailures(emitter)
	if err != nil {
		return controlplane.Announcements{}, err
	}
	promotions, err := catalognotifications.NewPromotionFailures(emitter)
	if err != nil {
		return controlplane.Announcements{}, err
	}
	logged := loggedAnnouncements{
		openings: governance, grants: governance, plans: plans, promotions: promotions, logger: logger,
	}
	return controlplane.Announcements{
		ChangeRequestOpenings: logged, GrantChanges: logged, PlanFailures: logged, PromotionFailures: logged,
	}, nil
}

// withAnnouncements fills in what the caller did not supply. A caller that set
// a port keeps it, so a test can observe one site without assembling a store.
func withAnnouncements(
	options controlplane.HandlerOptions, store notificationports.Store, logger *slog.Logger,
) (controlplane.HandlerOptions, error) {
	assembled, err := NewAnnouncements(store, logger)
	if err != nil {
		return options, err
	}
	if options.Announcements.ChangeRequestOpenings == nil {
		options.Announcements.ChangeRequestOpenings = assembled.ChangeRequestOpenings
	}
	if options.Announcements.GrantChanges == nil {
		options.Announcements.GrantChanges = assembled.GrantChanges
	}
	if options.Announcements.PlanFailures == nil {
		options.Announcements.PlanFailures = assembled.PlanFailures
	}
	if options.Announcements.PromotionFailures == nil {
		options.Announcements.PromotionFailures = assembled.PromotionFailures
	}
	return options, nil
}

// newGovernanceService keeps the same announcement ports on every Hub surface,
// including services called outside the legacy facade. A caller-supplied port
// is preserved, and absent ports remain nil when no store is configured.
func newGovernanceService(repository controlplane.Repository, options ...controlplane.HandlerOptions) *controlplane.GovernanceService {
	service := controlplane.NewGovernanceService(repository)
	if len(options) > 0 {
		service.SetAnnouncements(options[0].Announcements)
	}
	return service
}

// loggedAnnouncements is what makes the dropped errors honest, as
// loggedRecallOpenings is for recalls.
type loggedAnnouncements struct {
	openings   governanceports.ChangeRequestOpenings
	grants     governanceports.GrantChanges
	plans      planports.Failures
	promotions catalogports.PromotionFailures
	logger     *slog.Logger
}

func (logged loggedAnnouncements) AwaitingApproval(
	ctx context.Context, opening governanceports.ChangeRequestOpening,
) error {
	err := logged.openings.AwaitingApproval(ctx, opening)
	switch {
	case errors.Is(err, governancenotifications.ErrNoEnvironment):
		// Routine: a catalogue-wide request has no Environment to subscribe
		// to. Recorded so that "nobody was told" has an answer, at a level
		// that does not page anybody.
		logged.logger.InfoContext(ctx, "change request not announced", "changeRequest", opening.ID, "reason", err)
	case err != nil:
		logged.logger.ErrorContext(ctx, "announce change request awaiting approval",
			"changeRequest", opening.ID, "environment", opening.Environment, "error", err)
	}
	return err
}

func (logged loggedAnnouncements) GrantChanged(ctx context.Context, change governanceports.GrantChange) error {
	err := logged.grants.GrantChanged(ctx, change)
	if err != nil {
		logged.logger.ErrorContext(ctx, "announce resource grant change",
			"grant", change.After.ID, "resource", change.After.ResourceRID, "error", err)
	}
	return err
}

func (logged loggedAnnouncements) PlanFailed(ctx context.Context, failure planports.PlanFailure) error {
	err := logged.plans.PlanFailed(ctx, failure)
	if err != nil {
		logged.logger.ErrorContext(ctx, "announce failed plan",
			"plan", failure.PlanID, "environment", failure.Environment, "error", err)
	}
	return err
}

func (logged loggedAnnouncements) PromotionEvaluated(ctx context.Context, record catalog.PromotionRecord) error {
	err := logged.promotions.PromotionEvaluated(ctx, record)
	if err != nil {
		logged.logger.ErrorContext(ctx, "announce aborted promotion",
			"promotion", record.ID, "product", record.Product, "error", err)
	}
	return err
}
