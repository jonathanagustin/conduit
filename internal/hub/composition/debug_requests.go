package composition

import (
	"context"
	"errors"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	requesthttp "github.com/jonathanagustin/conduit/internal/debugsessions/adapters/httpapi"
	requestapp "github.com/jonathanagustin/conduit/internal/debugsessions/application"
	requests "github.com/jonathanagustin/conduit/internal/debugsessions/domain"
	requestports "github.com/jonathanagustin/conduit/internal/debugsessions/ports"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	notificationhttp "github.com/jonathanagustin/conduit/internal/notifications/adapters/httpapi"
)

func mountDebugRequests(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, journal requestports.Journal) (http.Handler, error) {
	if journal == nil {
		return legacy, nil
	}
	service := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	err := requesthttp.Register(mux, func(r *http.Request) (*requestapp.Service, error) {
		if _, err := operatorFrom(r.Context()); err != nil {
			var failure notificationhttp.StatusError
			status := 401
			if errors.As(err, &failure) {
				status = failure.Status
			}
			return nil, requesthttp.StatusError{Status: status, Message: "operator identity could not be established"}
		}
		return requestapp.New(journal, debugRequestAuthority{repository, service, options}, nil)
	})
	if err != nil {
		return nil, err
	}
	mux.Handle("/", legacy)
	return withOperatorCarrier(service, options, mux), nil
}

type debugRequestAuthority struct {
	repository controlplane.Repository
	governance *controlplane.GovernanceService
	options    controlplane.HandlerOptions
}

func (a debugRequestAuthority) Require(ctx context.Context, action string, scope requests.Scope) (requestports.Decision, error) {
	carrier, err := operatorFrom(ctx)
	if err != nil {
		return requestports.Decision{}, requests.ErrForbidden
	}
	if scope.Validate() != nil {
		return requestports.Decision{}, requests.ErrInvalid
	}
	environment := "environment/" + scope.Environment
	namespace := environment + "/namespace/" + scope.Namespace
	allow := func(operation string) bool {
		return a.governance.AuthorizeResourceOperation(ctx, carrier, a.options, environment, operation) && a.governance.AuthorizeResourceOperation(ctx, carrier, a.options, namespace, operation)
	}
	manage := allow(governance.OperationManageDebugRequest)
	switch action {
	case requestports.ActionCreate:
		if !allow(governance.OperationCreateDebugRequest) {
			return requestports.Decision{}, requests.ErrForbidden
		}
		// The namespace must be explicitly declared by this Environment. A request
		// profile is still intent metadata, not image or broker preflight proof.
		records, err := controlplane.ListEnvironmentCatalog(ctx, a.repository)
		if err != nil {
			return requestports.Decision{}, err
		}
		found := false
		for _, record := range records {
			if record.Environment.ID != scope.Environment {
				continue
			}
			for _, target := range record.Environment.Namespaces {
				if target.Name == scope.Namespace {
					found = true
				}
			}
		}
		if !found {
			return requestports.Decision{}, requests.ErrNotFound
		}
	case requestports.ActionRead, requestports.ActionMutate:
		if !manage && !allow(governance.OperationReadDebugRequest) {
			return requestports.Decision{}, requests.ErrForbidden
		}
	default:
		return requestports.Decision{}, requests.ErrForbidden
	}
	return requestports.Decision{Identity: requests.Identity{Actor: carrier.Username, Owner: governance.EffectivePrincipal(carrier).Username}, ManageOthers: manage}, nil
}
