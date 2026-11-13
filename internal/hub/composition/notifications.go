package composition

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	identity "github.com/jonathanagustin/conduit/internal/identity/domain"
	notificationhttp "github.com/jonathanagustin/conduit/internal/notifications/adapters/httpapi"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/application"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/httpapi"
)

// Options carries the Hub surfaces that are not the legacy facade's.
//
// A zero value mounts no notification routes: a Hub with no durable
// notification component serves none, rather than routes that answer 500.
// The packaging-run status route is always mounted and, with no reader,
// answers that no worker is configured.
type Options struct {
	// NotificationStore is the durable notification component. Nil means this
	// deployment has none -- REPOSITORY_KIND=file has no PostgreSQL to hold one.
	NotificationStore notificationports.Store
	// NotificationContacts is the address book behind the contact routes.
	//
	// A field of its own rather than a type assertion on the store, because the
	// port is separate for a reason and reading one off the other here would
	// undo that in the one place it is visible. Nil mounts no contact routes,
	// rather than routes that answer 500 to the first person who edits their
	// own address.
	NotificationContacts notificationports.Contacts
	// PackagingStatus reads packaging-run progress. Nil keeps the route, which
	// answers that no worker is configured. The route never accepts an approval.
	PackagingStatus packaginghttp.Reader
	// PackagingControls cancels or reruns a packaging run after governance
	// allows it. Nil keeps the write routes and refuses them. Cancel does not
	// cancel a Plan.
	PackagingControls packaginghttp.Controls
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
	// Nil stays nil rather than becoming a service over nothing: the adapter
	// reads a nil Contacts as "do not mount those routes", and a non-nil
	// pointer to a service with no store would be a route that answers 500.
	var contacts *notifications.Contacts
	if hub.NotificationContacts != nil {
		if contacts, err = notifications.NewContacts(hub.NotificationContacts, nil); err != nil {
			return nil, err
		}
	}
	service := controlplane.NewGovernanceService(repository)
	mux := http.NewServeMux()
	if err := notificationhttp.Register(mux, notificationhttp.Config{
		Reader:      reader,
		Preferences: preferences,
		Contacts:    contacts,
		Resolve:     operatorPrincipal(),
		Authorize:   resourceOperation(service, options),
	}); err != nil {
		return nil, err
	}
	mux.Handle("/", legacy)
	return withOperatorCarrier(service, options, mux), nil
}

// resourceOperation is the resource half of the authorization, and it does not
// answer it: internal/controlplane does, through the one entry point both
// surfaces reach. Restating "a Governance administrator, or a grant to one of
// the principal's teams" here is exactly what #241 was -- the restatement
// judged the bootstrap administrator list against the effective principal
// rather than the carrier, so a service account holding nothing but
// act-on-behalf read, through these routes, a resource the REST routes refused
// it. Asking rather than restating also means the decision is filed: the same
// call writes the audit record every REST authorization writes.
//
// The other half is not here and must not be. Whether this caller owns the
// inbox or preference they named is the notification adapter's own check, and
// it admits no administrator: a grant conveys authority over a resource, never
// over a person's mail. Folding the two together in one predicate is how the
// second one would quietly acquire the first one's escape hatch.
//
// The principal the adapter passes back is the effective one and cannot express
// a delegation, which is why the decision is taken against the carrier resolved
// for this request instead. The two are checked against each other rather than
// assumed equal, so a future adapter that resolved its own caller would be
// refused here rather than authorized as somebody else.
func resourceOperation(
	service *controlplane.GovernanceService, options controlplane.HandlerOptions,
) notificationhttp.Authorize {
	return func(
		ctx context.Context, principal notificationhttp.Principal, resourceRID, operation string,
	) bool {
		carrier, err := operatorFrom(ctx)
		if err != nil || governance.EffectivePrincipal(carrier).Username != principal.Username {
			return false
		}
		return service.AuthorizeResourceOperation(ctx, carrier, options, resourceRID, operation)
	}
}

// operatorCarrierKey addresses the request-scoped identity below.
type operatorCarrierKey struct{}

// withOperatorCarrier resolves the operator behind a request at most once and
// hands the same answer to everything that asks for it.
//
// It exists because the two notification callbacks need different halves of one
// identity. Resolve needs the *effective* principal, because an inbox is read
// as whoever the caller is acting for, and the adapter is deliberately given
// nothing else -- its own comment says handing it the whole Governance
// principal would let a later edit there reinvent delegation. Authorize needs
// the whole principal, carrier included, because that is what the administrator
// test is judged against. Resolving once here and letting each callback take
// the half it needs is what makes both true at the same time; #241 is the
// defect that existed while only the effective half was reachable.
//
// It is lazy, and it wraps the composed mux rather than the notification routes
// alone, so that the facade's own routes are unaffected: nothing resolves an
// identity unless a notification route asks, and mounting stays a matter of
// which patterns the adapter registered rather than a prefix restated here.
func withOperatorCarrier(
	service *controlplane.GovernanceService, options controlplane.HandlerOptions, next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		resolve := sync.OnceValues(func() (governance.Principal, error) {
			claims, err := operatorClaims(request, options)
			if err != nil {
				return governance.Principal{}, notificationhttp.StatusError{
					Status: http.StatusUnauthorized, Message: err.Error(),
				}
			}
			principal, err := service.ResolvePrincipal(request.Context(), claims)
			if err != nil {
				return governance.Principal{}, notificationhttp.StatusError{
					Status: statusForIdentityError(err), Message: err.Error(),
				}
			}
			return principal, nil
		})
		next.ServeHTTP(writer, request.WithContext(
			context.WithValue(request.Context(), operatorCarrierKey{}, resolve)))
	})
}

// operatorFrom reads the resolved operator, or fails closed. A context with no
// carrier means these routes were mounted without withOperatorCarrier, and a
// route that cannot say who is asking must refuse rather than assume.
func operatorFrom(ctx context.Context) (governance.Principal, error) {
	resolve, mounted := ctx.Value(operatorCarrierKey{}).(func() (governance.Principal, error))
	if !mounted {
		return governance.Principal{}, notificationhttp.StatusError{
			Status:  http.StatusUnauthorized,
			Message: "the notification routes were mounted without operator resolution",
		}
	}
	return resolve()
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
//
// The carrier it was resolved from is not discarded, only withheld from the
// adapter: withOperatorCarrier keeps it on the request for the authorization
// decision, which is judged against a different half of the same identity.
func operatorPrincipal() notificationhttp.ResolvePrincipal {
	return func(request *http.Request) (notificationhttp.Principal, error) {
		principal, err := operatorFrom(request.Context())
		if err != nil {
			return notificationhttp.Principal{}, err
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
