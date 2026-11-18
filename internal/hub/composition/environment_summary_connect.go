package composition

import (
	"context"
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	environmenthttp "github.com/jonathanagustin/conduit/internal/controlplane/httpapi"
)

type environmentSummaryPrincipalKey struct{}

func mountEnvironmentSummaryConnect(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	environmenthttp.RegisterEnvironmentSummaryConnect(mux, environmenthttp.EnvironmentSummaryConfig{
		Authenticate: func(request *http.Request) (context.Context, error) {
			claims, err := operatorClaims(request, options)
			if err != nil {
				return nil, err
			}
			principal, err := governance.ResolvePrincipal(request.Context(), claims)
			if err != nil {
				return nil, err
			}
			return context.WithValue(request.Context(), environmentSummaryPrincipalKey{}, principal), nil
		},
		Read: func(ctx context.Context, search string, cursor string, spaceID string, limit *int32) (controlplane.EnvironmentCatalogSummaryPage, error) {
			principal, ok := ctx.Value(environmentSummaryPrincipalKey{}).(controlplane.Principal)
			if !ok {
				return controlplane.EnvironmentCatalogSummaryPage{}, controlplane.ErrUnauthorized
			}
			return controlplane.ReadAuthorizedEnvironmentSummaryPage(ctx, repository, governance, principal, options, search, cursor, spaceID, limit, time.Now().UTC())
		},
	})
	mux.Handle("/", legacy)
	return mux
}
