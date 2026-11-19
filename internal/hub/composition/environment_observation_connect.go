package composition

import (
	"context"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	environmenthttp "github.com/jonathanagustin/conduit/internal/controlplane/httpapi"
)

type environmentObservationPrincipalKey struct{}

func mountEnvironmentObservationConnect(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	environmenthttp.RegisterEnvironmentObservationConnect(mux, environmenthttp.EnvironmentObservationConfig{
		Authenticate: func(request *http.Request) (context.Context, error) {
			claims, err := operatorClaims(request, options)
			if err != nil {
				return nil, err
			}
			principal, err := governance.ResolvePrincipal(request.Context(), claims)
			if err != nil {
				return nil, err
			}
			return context.WithValue(request.Context(), environmentObservationPrincipalKey{}, principal), nil
		},
		Read: func(ctx context.Context, environment string) (controlplane.EnvironmentReport, error) {
			principal, ok := ctx.Value(environmentObservationPrincipalKey{}).(controlplane.Principal)
			if !ok {
				return controlplane.EnvironmentReport{}, controlplane.ErrUnauthorized
			}
			return controlplane.ReadAuthorizedEnvironmentReport(ctx, repository, governance, principal, options, environment)
		},
	})
	mux.Handle("/", legacy)
	return mux
}
