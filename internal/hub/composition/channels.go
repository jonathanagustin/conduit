package composition

import (
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	channelapp "github.com/jonathanagustin/conduit/internal/delivery/channels/application"
	channelhttp "github.com/jonathanagustin/conduit/internal/delivery/channels/httpapi"
)

func mountChannels(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	service := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	channelhttp.Register(mux, channelhttp.Config{
		Board: channelapp.Service{Plans: repository},
		Select: func(w http.ResponseWriter, request *http.Request) (func(string) bool, bool) {
			principal, ok := packagingPrincipal(request, service, options)
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return nil, false
			}
			return controlplane.ResolveSpaceEnvironmentFilter(w, request, principal, options)
		},
		Authorize: func(request *http.Request) error {
			_, err := operatorClaims(request, options)
			return err
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
	})
	mux.Handle("/", legacy)
	return mux
}
