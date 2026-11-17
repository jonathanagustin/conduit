package notificationsadapter

import (
	"context"
	"errors"

	"github.com/jonathanagustin/conduit/internal/governance/application"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/domain"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// RecipientEligibility translates notification ownership into Governance's
// current authority checks. It carries no authentication claims or credentials.
type RecipientEligibility struct{ access *application.RecipientAccess }

var _ notificationports.RecipientEligibility = (*RecipientEligibility)(nil)

func NewRecipientEligibility(access *application.RecipientAccess) (*RecipientEligibility, error) {
	if access == nil {
		return nil, errors.New("current governance access is required for notification recipients")
	}
	return &RecipientEligibility{access: access}, nil
}

func (eligibility *RecipientEligibility) Check(ctx context.Context, owner notifications.Owner, resourceRID string) (bool, error) {
	switch owner.Kind {
	case notifications.PrincipalUser:
		return eligibility.access.UserCanRead(ctx, owner.ID, resourceRID)
	case notifications.PrincipalTeam:
		return eligibility.access.TeamCanRead(ctx, owner.ID, resourceRID)
	default:
		return false, nil
	}
}
