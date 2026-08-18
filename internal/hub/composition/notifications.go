package composition

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	identity "github.com/jonathanagustin/conduit/internal/identity/domain"
	notificationhttp "github.com/jonathanagustin/conduit/internal/notifications/adapters/httpapi"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/application"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// Options carries the Hub surfaces that are not the legacy facade's.
//
// A zero value mounts nothing extra, which is what every caller without a
// notification store gets: a Hub with no durable notification component serves
// no notification routes, rather than serving routes that answer 500.
type Options struct {
	// NotificationStore is the durable notification component. Nil means this
	// deployment has none -- REPOSITORY_KIND=file has no PostgreSQL to hold one.
	NotificationStore notificationports.Store
}

// mountNotifications puts the notification routes in front of the legacy
// facade, or returns the facade unchanged when there is no store to serve them
// from.
//
// In front rather than inside: internal/controlplane is on a ratcheted line
// ceiling and new Hub wiring belongs here, which is what internal/AGENTS.md
// says. The fall-through pattern is what keeps that true -- "/" reaches the
// facade, so every route it already serves is unchanged, and the four patterns
// registered here are more specific and win.
func mountNotifications(
	legacy http.Handler, repository controlplane.Repository,
	options controlplane.HandlerOptions, hub Options,
) (http.Handler, error) {
	if hub.NotificationStore == nil {
		return legacy, nil
	}
	reader, err := notifications.NewReader(hub.NotificationStore)
	if err != nil {
		return nil, err
	}
	preferences, err := notifications.NewPreferences(hub.NotificationStore, nil)
	if err != nil {
		return nil, err
	}
	service := controlplane.NewGovernanceService(repository)
	mux := http.NewServeMux()
	if err := notificationhttp.Register(mux, notificationhttp.Config{
		Reader:      reader,
		Preferences: preferences,
		Resolve:     operatorPrincipal(service, options),
		Authorize:   resourceOperation(service, options),
	}); err != nil {
		return nil, err
	}
	mux.Handle("/", legacy)
	return mux, nil
}

// resourceOperation is the resource half of the authorization, and it is the
// half that matches internal/controlplane's requireResourceOperation exactly: a
// Governance administrator, or a grant to one of the principal's teams.
//
// The other half is not here and must not be. Whether this caller owns the
// inbox or preference they named is the notification adapter's own check, and
// it admits no administrator: a grant conveys authority over a resource, never
// over a person's mail. Folding the two together in one predicate is how the
// second one would quietly acquire the first one's escape hatch.
func resourceOperation(
	service *controlplane.GovernanceService, options controlplane.HandlerOptions,
) notificationhttp.Authorize {
	return func(
		ctx context.Context, principal notificationhttp.Principal, resourceRID, operation string,
	) bool {
		asking := governance.Principal{Username: principal.Username, Teams: principal.Teams}
		return governanceAdministrator(ctx, service, asking, options) ||
			service.Authorize(ctx, asking, resourceRID, operation)
	}
}

func governanceAdministrator(
	ctx context.Context, service *controlplane.GovernanceService,
	principal governance.Principal, options controlplane.HandlerOptions,
) bool {
	for _, administrator := range options.BootstrapAdministrators {
		if strings.TrimSpace(administrator) == principal.Username {
			return true
		}
	}
	if options.AllowInsecureOperatorIdentityHeaders && len(options.BootstrapAdministrators) == 0 &&
		principal.Username == "local-developer" {
		return true
	}
	return service.Authorize(ctx, principal, "platform/root", governance.OperationManagePermissions)
}

// operatorPrincipal resolves an operator the way every other Hub route does:
// the configured OIDC resolver if there is one, otherwise a verified client
// certificate, otherwise the development headers behind their explicit opt-in,
// and Governance turns the claims into a principal with its teams resolved.
//
// This restates internal/controlplane's requireOperatorPrincipal rather than
// calling it, because that function is unexported and the package it lives in
// is at a line ceiling no new export may cross. The restatement is held to the
// original by TestNotificationRoutesAuthenticateLikeEveryOtherHubRoute, which
// drives the same three credentials through both surfaces and asserts they
// agree -- a comment promising the two match would not have noticed when they
// stopped.
//
// The principal handed on is the effective one. A service account acting for a
// person reads that person's inbox and nobody else's, which is the same
// doctrine every authorization decision in this Hub already uses; resolving it
// here means the notification adapter never sees a delegation and cannot get it
// wrong.
func operatorPrincipal(
	service *controlplane.GovernanceService, options controlplane.HandlerOptions,
) notificationhttp.ResolvePrincipal {
	return func(request *http.Request) (notificationhttp.Principal, error) {
		claims, err := operatorClaims(request, options)
		if err != nil {
			return notificationhttp.Principal{}, notificationhttp.StatusError{
				Status: http.StatusUnauthorized, Message: err.Error(),
			}
		}
		principal, err := service.ResolvePrincipal(request.Context(), claims)
		if err != nil {
			return notificationhttp.Principal{}, notificationhttp.StatusError{
				Status: statusForIdentityError(err), Message: err.Error(),
			}
		}
		effective := governance.EffectivePrincipal(principal)
		return notificationhttp.Principal{Username: effective.Username, Teams: effective.Teams}, nil
	}
}

func operatorClaims(
	request *http.Request, options controlplane.HandlerOptions,
) (controlplane.IdentityClaims, error) {
	switch {
	case options.OperatorIdentityResolver != nil:
		return options.OperatorIdentityResolver(request)
	case options.AllowInsecureOperatorIdentityHeaders:
		claims := controlplane.IdentityClaims{
			Username:       request.Header.Get("X-Platform-Subject"),
			Groups:         strings.Split(request.Header.Get("X-Platform-Groups"), ","),
			ServiceAccount: strings.EqualFold(request.Header.Get("X-Platform-Service-Account"), "true"),
			OnBehalfOf:     request.Header.Get("X-Platform-On-Behalf-Of"),
		}
		if strings.TrimSpace(claims.Username) == "" {
			claims.Username = "local-developer"
		}
		return claims, nil
	case request.TLS != nil && len(request.TLS.VerifiedChains) > 0 && len(request.TLS.VerifiedChains[0]) > 0:
		return identity.OperatorIdentityFromCertificate(request.TLS.VerifiedChains[0][0])
	default:
		return controlplane.IdentityClaims{}, controlplane.ErrUnauthorized
	}
}

// statusForIdentityError keeps the two identity failures apart. A refused
// delegation is a forbidden request by an authenticated caller, and reporting
// it as 401 would tell a service account to go and authenticate again.
func statusForIdentityError(err error) int {
	if errors.Is(err, controlplane.ErrForbidden) {
		return http.StatusForbidden
	}
	return http.StatusUnauthorized
}
