package composition

import (
	"context"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	planhttp "github.com/jonathanagustin/conduit/internal/controlplane/httpapi"
)

type planScopedSummaryPrincipalKey struct{}

func mountPlanScopedSummaryConnect(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	planhttp.RegisterPlanScopedSummaryConnect(mux, planhttp.PlanScopedSummaryConfig{
		Authenticate: func(request *http.Request) (context.Context, error) {
			claims, err := operatorClaims(request, options)
			if err != nil {
				return nil, err
			}
			principal, err := governance.ResolvePrincipal(request.Context(), claims)
			if err != nil {
				return nil, err
			}
			return context.WithValue(request.Context(), planScopedSummaryPrincipalKey{}, principal), nil
		},
		Read: func(ctx context.Context, spaceID string, query controlplane.PlanQuery, cursor string, limit *int32) (controlplane.PlanSummaryPage, error) {
			principal, ok := ctx.Value(planScopedSummaryPrincipalKey{}).(controlplane.Principal)
			if !ok {
				return controlplane.PlanSummaryPage{}, controlplane.ErrUnauthorized
			}
			return controlplane.ReadAuthorizedPlanSummaryPageInSpace(ctx, repository, governance, principal, options, spaceID, query, cursor, limit)
		},
	})
	mux.Handle("/", legacy)
	return mux
}
