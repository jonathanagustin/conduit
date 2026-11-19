package composition

import (
	"context"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	fleethttp "github.com/jonathanagustin/conduit/internal/controlplane/httpapi"
)

type fleetReadPrincipalKey struct{}

func mountFleetReadConnect(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	fleethttp.RegisterFleetReadConnect(mux, fleethttp.FleetReadConfig{
		Authenticate: func(request *http.Request) (context.Context, error) {
			claims, err := operatorClaims(request, options)
			if err != nil {
				return nil, err
			}
			principal, err := governance.ResolvePrincipal(request.Context(), claims)
			if err != nil {
				return nil, err
			}
			return context.WithValue(request.Context(), fleetReadPrincipalKey{}, principal), nil
		},
		Read: func(ctx context.Context, request controlplane.FleetReadRequest) (controlplane.FleetProjection, error) {
			principal, ok := ctx.Value(fleetReadPrincipalKey{}).(controlplane.Principal)
			if !ok {
				return controlplane.FleetProjection{}, controlplane.ErrUnauthorized
			}
			return controlplane.ReadAuthorizedFleetPage(ctx, repository, governance, principal, options, request)
		},
	})
	mux.Handle("/", legacy)
	return mux
}
