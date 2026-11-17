package composition

import (
	"context"
	"errors"
	"log/slog"

	catalognotifications "github.com/jonathanagustin/conduit/internal/catalog/adapters/notifications"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	catalogports "github.com/jonathanagustin/conduit/internal/catalog/ports"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/application"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// NewRecallOpenings assembles the port that tells somebody a recall opened, or
// reports that this deployment has nobody to tell.
//
// Nil store, nil port, and that is the same answer NewNotificationTransport
// gives an unconfigured deployment: REPOSITORY_KIND=file has no PostgreSQL to
// hold a notification store, and a control plane with nowhere to record an
// event should raise none rather than fail every recall that arrives. The
// caller passes the nil straight into HandlerOptions, where it means the same
// thing.
//
// Assembled here rather than in internal/controlplane because that package is
// on a ratcheted line ceiling and this is wiring. What crosses into it is one
// interface value; the emitter, the fan-out and the event vocabulary stay on
// this side of it.
func NewRecallOpenings(
	store notificationports.Store, eligibility notificationports.RecipientEligibility, logger *slog.Logger,
) (catalogports.RecallOpenings, error) {
	if store == nil {
		return nil, nil
	}
	if logger == nil {
		return nil, errors.New("a logger is required: the surface that announces a recall drops the error")
	}
	emitter, err := notifications.NewEmitter(store, eligibility)
	if err != nil {
		return nil, err
	}
	openings, err := catalognotifications.NewRecallOpenings(emitter, notifications.SystemClock{}.Now)
	if err != nil {
		return nil, err
	}
	return loggedRecallOpenings{inner: openings, logger: logger}, nil
}

// loggedRecallOpenings is what makes the dropped error at the emit site honest.
//
// internal/controlplane discards the result on purpose -- the DesiredState is
// committed by the time it announces, so a notification failure must not be
// reported as a refusal of work that happened -- which would leave a broken
// notification store silent in the one place silence is the defect. Logging
// here means the announcement failed loudly and the Plan still succeeded.
type loggedRecallOpenings struct {
	inner  catalogports.RecallOpenings
	logger *slog.Logger
}

func (openings loggedRecallOpenings) RecallsInForce(
	ctx context.Context, environment string, recalls []catalog.Recall,
) error {
	err := openings.inner.RecallsInForce(ctx, environment, recalls)
	if err != nil {
		openings.logger.ErrorContext(ctx, "announce recalls in force", "environment", environment, "error", err)
	}
	return err
}
