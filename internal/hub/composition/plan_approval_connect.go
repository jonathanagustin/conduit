package composition

import (
	"context"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	approvalhttp "github.com/jonathanagustin/conduit/internal/controlplane/httpapi"
)

type planApprovalPrincipalKey struct{}

func mountPlanApprovalConnect(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	governance := newGovernanceService(repository, options)
	lifecycle := controlplane.NewLifecycle(repository)
	mux := http.NewServeMux()
	approvalhttp.RegisterPlanApprovalConnect(mux, approvalhttp.PlanApprovalConfig{
		Authenticate: func(request *http.Request) (context.Context, error) {
			claims, err := operatorClaims(request, options)
			if err != nil {
				return nil, err
			}
			principal, err := governance.ResolvePrincipal(request.Context(), claims)
			if err != nil {
				return nil, err
			}
			return context.WithValue(request.Context(), planApprovalPrincipalKey{}, principal), nil
		},
		Approve: func(ctx context.Context, planID, rationale string) (controlplane.PlanRecord, error) {
			principal, ok := ctx.Value(planApprovalPrincipalKey{}).(controlplane.Principal)
			if !ok {
				return controlplane.PlanRecord{}, controlplane.ErrUnauthorized
			}
			return controlplane.ApprovePlanAsOperator(ctx, repository, lifecycle, governance, principal, options, planID, rationale)
		},
	})
	mux.Handle("/", legacy)
	return mux
}
