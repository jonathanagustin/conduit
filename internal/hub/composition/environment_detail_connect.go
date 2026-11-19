package composition

import (
	"context"
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	environmenthttp "github.com/jonathanagustin/conduit/internal/controlplane/httpapi"
)

type environmentDetailPrincipalKey struct{}

func mountEnvironmentDetailConnect(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	environmenthttp.RegisterEnvironmentDetailConnect(mux, environmenthttp.EnvironmentDetailConfig{
		Authenticate: func(request *http.Request) (context.Context, error) {
			claims, err := operatorClaims(request, options)
			if err != nil {
				return nil, err
			}
			principal, err := governance.ResolvePrincipal(request.Context(), claims)
			if err != nil {
				return nil, err
			}
			return context.WithValue(request.Context(), environmentDetailPrincipalKey{}, principal), nil
		},
		Read: func(ctx context.Context, environment string, spaceID *string) (controlplane.EnvironmentCatalogRecord, error) {
			principal, ok := ctx.Value(environmentDetailPrincipalKey{}).(controlplane.Principal)
			if !ok {
				return controlplane.EnvironmentCatalogRecord{}, controlplane.ErrUnauthorized
			}
			return controlplane.ReadAuthorizedEnvironmentDetail(ctx, repository, governance, principal, options, environment, func() (*string, error) { return spaceID, nil }, time.Now)
		},
	})
	mux.Handle("/", legacy)
	return mux
}
