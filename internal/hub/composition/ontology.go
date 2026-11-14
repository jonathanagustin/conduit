package composition

import (
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	ontologyrepo "github.com/jonathanagustin/conduit/internal/ontology/adapters/repository"
	ontologyhttp "github.com/jonathanagustin/conduit/internal/ontology/httpapi"
	"github.com/jonathanagustin/conduit/internal/ontology/ports"
)

// mountOntology serves the catalog in front of the rest of the Hub. The routes
// stay mounted when the repository is empty so an anonymous caller is refused
// rather than the path not existing. Apply proposes a change request and does
// not approve it.
func mountOntology(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
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
