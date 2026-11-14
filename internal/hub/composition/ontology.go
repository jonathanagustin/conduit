package composition

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	ontologyrepo "github.com/jonathanagustin/conduit/internal/ontology/adapters/repository"
	ontologydomain "github.com/jonathanagustin/conduit/internal/ontology/domain"
	ontologyhttp "github.com/jonathanagustin/conduit/internal/ontology/httpapi"
	"github.com/jonathanagustin/conduit/internal/ontology/ports"
)

// mountOntology serves the catalog in front of the rest of the Hub. The routes
// stay mounted when the repository is empty so an anonymous caller is refused
// rather than the path not existing. Apply proposes a change request and does
// not approve it.
func mountOntology(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, hub Options) http.Handler {
	service := controlplane.NewGovernanceService(repository)
	mux := http.NewServeMux()
	ontologyhttp.Register(mux, ontologyhttp.Config{
		Source: ontologyrepo.New(repository, service),
		Authorize: func(request *http.Request) error {
			if _, err := operatorClaims(request, options); err != nil {
				return ontologyhttp.StatusError{Status: statusForIdentityError(err), Message: "unauthorized"}
			}
			return nil
		},
		Allow: func(request *http.Request, environmentID string) bool {
			principal, ok := packagingPrincipal(request, service, options)
			if !ok || environmentID == "" {
				return false
			}
			return service.AuthorizeResourceOperation(
				request.Context(), principal, options, "environment/"+environmentID, controlplane.OperationReadResource,
			)
		},
		Caller: func(request *http.Request) (ports.Caller, error) {
			principal, ok := packagingPrincipal(request, service, options)
			if !ok {
				return ports.Caller{}, ontologyhttp.StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
			}
			return ports.Caller{Username: principal.Username, Teams: principal.Teams, Groups: principal.Groups}, nil
		},
		Audit: func(ctx context.Context, subject, action, resource, decision, reason string) error {
			event := governance.NewAuthorizationCheckEvent(time.Now().UTC(), subject, action, resource, decision, reason, nil)
			_, err := repository.AppendAuthorizationAudit(ctx, event)
			return err
		},
		Facts: func(ctx context.Context, action, resource string) (ontologydomain.ActionFacts, error) {
			return actionFacts(ctx, hub, action, resource)
		},
		Permit: func(request *http.Request, environmentID string) error {
			principal, ok := packagingPrincipal(request, service, options)
			if !ok {
				return ontologyhttp.StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
			}
			if environmentID == "" || !service.AuthorizeResourceOperation(
				request.Context(), principal, options, "environment/"+environmentID, controlplane.OperationProposeChange,
			) {
				return ontologyhttp.StatusError{Status: http.StatusForbidden, Message: "forbidden"}
			}
			return nil
		},
	})
	mux.Handle("/", legacy)
	return mux
}

func actionFacts(ctx context.Context, hub Options, action, resource string) (ontologydomain.ActionFacts, error) {
	facts := ontologydomain.ActionFacts{}
	runID := strings.TrimPrefix(resource, "packaging-run/")
	switch action {
	case "cancel_packaging_run", "cancel_run", "rerun_packaging_run", "rerun_from_step":
		if hub.PackagingStatus == nil || runID == "" {
			return facts, nil
		}
		status, err := hub.PackagingStatus.Read(ctx, runID)
		if err != nil {
			return facts, nil
		}
		facts.RunKnown = true
		facts.RunPhase = status.Phase
	}
	return facts, nil
}
