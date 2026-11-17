package composition

import (
	"context"
	"errors"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	notificationauthorization "github.com/jonathanagustin/conduit/internal/governance/adapters/notifications"
	governanceapp "github.com/jonathanagustin/conduit/internal/governance/application"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
)

type notificationGrantSource struct {
	repository controlplane.GovernanceRepository
}

func (source notificationGrantSource) ResourceGrantsInScope(ctx context.Context, scope string) ([]governance.ResourceGrant, error) {
	return source.repository.ListResourceGrants(ctx, scope)
}

// NewNotificationRecipientEligibility binds background notification checks to
// current native teams, grants and the same configured bootstrap policy as
// request authorization. It never captures a request's OIDC claims or token.
func NewNotificationRecipientEligibility(repository controlplane.GovernanceRepository, options controlplane.HandlerOptions) (notificationports.RecipientEligibility, error) {
	if repository == nil {
		return nil, errors.New("a governance repository is required for notification recipient authorization")
	}
	access, err := governanceapp.NewRecipientAccess(repository, notificationGrantSource{repository}, governanceapp.AdministratorPolicy{
		BootstrapAdministrators:     options.BootstrapAdministrators,
		AllowLocalDeveloperFallback: options.AllowInsecureOperatorIdentityHeaders,
	})
	if err != nil {
		return nil, err
	}
	return notificationauthorization.NewRecipientEligibility(access)
}
