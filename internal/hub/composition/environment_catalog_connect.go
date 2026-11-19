package composition

import (
	"context"
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	environmenthttp "github.com/jonathanagustin/conduit/internal/controlplane/httpapi"
)

type environmentCatalogPrincipalKey struct{}

func mountEnvironmentCatalogConnect(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	environmenthttp.RegisterEnvironmentCatalogConnect(mux, environmenthttp.EnvironmentCatalogConfig{
		Authenticate: func(request *http.Request) (context.Context, error) {
			claims, err := operatorClaims(request, options)
			if err != nil {
				return nil, err
			}
			principal, err := governance.ResolvePrincipal(request.Context(), claims)
			if err != nil {
				return nil, err
			}
			return context.WithValue(request.Context(), environmentCatalogPrincipalKey{}, principal), nil
		},
		Read: func(ctx context.Context, search string, cursor string, spaceID *string, limit *int32) (controlplane.EnvironmentCatalogPage, error) {
			principal, ok := ctx.Value(environmentCatalogPrincipalKey{}).(controlplane.Principal)
			if !ok {
				return controlplane.EnvironmentCatalogPage{}, controlplane.ErrUnauthorized
			}
			return controlplane.ReadAuthorizedEnvironmentCatalogPage(ctx, repository, governance, principal, options, search, cursor, spaceID, limit, time.Now)
		},
	})
	mux.Handle("/", legacy)
	return mux
}
