package composition

import (
	"context"
	"errors"
	"time"

	planports "github.com/jonathanagustin/conduit/internal/delivery/plan/ports"
	governanceports "github.com/jonathanagustin/conduit/internal/governance/ports"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

// failingNotificationStore reads normally and cannot record, which is a
// notification database that is up for reads and refusing writes.
type failingNotificationStore struct{ *notificationStore }

func (failingNotificationStore) RecordEvent(context.Context, domain.Event, []domain.InboxEntry) error {
	return errors.New("the notification store refused the write")
}

func planFailure() planports.PlanFailure {
	return planports.PlanFailure{
		PlanID: "pln_abc", Environment: "dev-use2", Reason: "chart rollout failed",
		At: time.Date(2026, time.November, 16, 9, 30, 0, 0, time.UTC),
	}
}

func catalogueOpening() governanceports.ChangeRequestOpening {
	return governanceports.ChangeRequestOpening{ID: "crq_catalogue", TargetKind: "LABEL_DEFINITION", TargetID: "tier", Revision: 1}
}
