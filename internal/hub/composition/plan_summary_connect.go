package composition

import (
	"context"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	planhttp "github.com/jonathanagustin/conduit/internal/controlplane/httpapi"
)

type planSummaryPrincipalKey struct{}

func mountPlanSummaryConnect(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	planhttp.RegisterPlanSummaryConnect(mux, planhttp.PlanSummaryConfig{
		Authenticate: func(request *http.Request) (context.Context, error) {
			claims, err := operatorClaims(request, options)
			if err != nil {
				return nil, err
			}
			principal, err := governance.ResolvePrincipal(request.Context(), claims)
			if err != nil {
				return nil, err
			}
			return context.WithValue(request.Context(), planSummaryPrincipalKey{}, principal), nil
		},
		Read: func(ctx context.Context, query controlplane.PlanQuery, cursor string, limit int) (controlplane.PlanSummaryPage, error) {
			principal, ok := ctx.Value(planSummaryPrincipalKey{}).(controlplane.Principal)
			if !ok {
				return controlplane.PlanSummaryPage{}, controlplane.ErrUnauthorized
			}
			return controlplane.ReadAuthorizedPlanSummaryPage(ctx, repository, governance, principal, options, query, cursor, limit)
		},
	})
	mux.Handle("/", legacy)
	return mux
}
